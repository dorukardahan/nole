# Partial-Keys Graceful Degradation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Nólë behave as a strict enhancement under any partial-key configuration — search keeps working via DDGS, extract is hidden from the MCP tool surface when no extract-capable provider is configured (AI tool falls back to its built-in fetch), and an actionable upgrade-path message appears once per MCP session.

**Architecture:** Three layered behaviors driven by a single source-of-truth helper. (1) A new `core.BYOKProviders` table replaces the cli-local `byokFreeDefaults` map; cli quota wiring and the new setup-hints builder both read from it. (2) `core.BuildSetupSuggestions(configured)` returns structured suggestions with `impact` ranking that the AI tool uses to decide what to surface. (3) The MCP server inspects configured providers at startup, skips registering `extract` if no extract-capable provider has a key, and tracks first-call-of-session in a connection-scoped boolean so the search response carries a `setup_tip` exactly once.

**Tech Stack:** Go, mark3labs/mcp-go for MCP stdio, standard Go testing. TDD throughout — every behavior gets a failing test before any production code.

---

## File Structure

**New files**
- `internal/core/byok_metadata.go` — `BYOKProvider` struct + `BYOKProviders` slice (the single source of truth)
- `internal/core/byok_metadata_test.go` — invariants on the slice (no duplicates, every entry has required fields, capability flags consistent)
- `internal/core/setup_hints.go` — `BuildSetupSuggestions` and `BuildSetupTip` pure functions
- `internal/core/setup_hints_test.go` — exhaustive truth table over key configurations

**Modified files**
- `internal/core/types.go` — add `SetupSuggestion`, `SetupTip`; extend `ProviderStatusResponse` and `SearchResponse`
- `internal/cli/app.go` — `byokFreeDefaults` deleted; quota wiring reads from `core.BYOKProviders`
- `internal/cli/providers.go` — JSON output gains `setup_suggestions`
- `internal/cli/providers_test.go` — assert new field
- `internal/mcpserver/server.go` (or equivalent) — conditional `extract` registration; per-connection tip flag
- `internal/mcpserver/server_test.go` — assert tool surface honors capability availability; assert tip emitted once
- `internal/bench/claims_guard_test.go` — extend to scan new strings
- `docs/PROVIDER-KEYS.md` — short addendum

---

## Task 0: Locate exact MCP server file paths

Before implementation, the engineer needs the exact filenames for the MCP server changes. The current spec references `internal/mcpserver/*` without naming files because the existing code may have split tool registration across files. This task is read-only.

**Files:**
- Read: `internal/mcpserver/`

- [ ] **Step 1: List the MCP server package**

Run: `ls -la internal/mcpserver/`
Expected: at least one `.go` file plus its `_test.go` companion. Note the file where `AddTool` (or the mark3labs/mcp-go equivalent) is called for each of `search`, `extract`, `provider_status`, `budget_status`.

- [ ] **Step 2: Identify the search/extract registration callsites**

Run: `grep -nE "AddTool|RegisterTool|mcp__nole" internal/mcpserver/*.go | head -40`
Expected: lines that name each tool — record the file path and line numbers for the four tools.

- [ ] **Step 3: Note the server constructor**

Find the function returning the MCP server value (commonly `NewServer` or `New`). Record its file/line so later tasks know where to add a `tipEmitted bool` field.

No commit — this is an information-gathering task.

---

## Task 1: Define BYOKProvider metadata in core

**Files:**
- Create: `internal/core/byok_metadata.go`
- Create: `internal/core/byok_metadata_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/core/byok_metadata_test.go`:

```go
package core

import (
	"strings"
	"testing"
)

func TestBYOKProvidersInvariants(t *testing.T) {
	if len(BYOKProviders) == 0 {
		t.Fatal("BYOKProviders is empty")
	}
	names := map[string]bool{}
	envVars := map[string]bool{}
	for _, p := range BYOKProviders {
		if p.Name == "" {
			t.Errorf("entry has empty Name: %#v", p)
		}
		if names[p.Name] {
			t.Errorf("duplicate provider name: %q", p.Name)
		}
		names[p.Name] = true
		if len(p.EnvVars) == 0 {
			t.Errorf("%s has no env vars", p.Name)
		}
		for _, ev := range p.EnvVars {
			if envVars[ev] {
				t.Errorf("env var %q is claimed by multiple providers", ev)
			}
			envVars[ev] = true
			if !strings.HasSuffix(ev, "_API_KEY") && !strings.HasSuffix(ev, "_SEARCH_API_KEY") {
				t.Errorf("%s env var %q does not follow the *_API_KEY convention", p.Name, ev)
			}
		}
		if p.FreeQuota <= 0 {
			t.Errorf("%s has non-positive FreeQuota %d", p.Name, p.FreeQuota)
		}
		if p.RefreshWindow == "" {
			t.Errorf("%s has empty RefreshWindow", p.Name)
		}
		if p.SignupURL == "" {
			t.Errorf("%s has empty SignupURL", p.Name)
		}
		if p.EnvExample == "" {
			t.Errorf("%s has empty EnvExample", p.Name)
		}
		if !p.SupportsSearch && !p.SupportsExtract {
			t.Errorf("%s supports neither search nor extract — what is it for?", p.Name)
		}
	}
	for _, want := range []string{"brave", "tavily", "firecrawl"} {
		if !names[want] {
			t.Errorf("missing required provider %q", want)
		}
	}
	if names["jina"] {
		t.Error("jina entry leaked back in — it was removed in PR #21")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd <repo> && go test ./internal/core/ -run TestBYOKProvidersInvariants`
Expected: FAIL with "undefined: BYOKProviders" (or build error referencing missing type).

- [ ] **Step 3: Create the metadata file**

Create `internal/core/byok_metadata.go`:

```go
package core

// BYOKProvider holds the metadata Nólë needs to (1) seed the local free-tier
// quota for a keyed provider, (2) classify it during cost decisions, and
// (3) tell the user what they'd unlock by configuring it. Adding a new BYOK
// provider means appending one entry here — the cli quota wiring, the setup
// hints builder, and the docs all consume from this slice.
type BYOKProvider struct {
	Name            string
	EnvVars         []string // primary first; the rest are accepted aliases
	FreeQuota       int
	RefreshWindow   RefreshWindow
	SupportsSearch  bool
	SupportsExtract bool
	SignupURL       string
	FreeTierNote    string
	EnvExample      string
	// Unlocks lists the user-facing capabilities this provider enables.
	// Strings are stable identifiers consumed by AI tools deciding whether to
	// surface a suggestion.
	Unlocks []string
}

// BYOKProviders is the single source of truth for keyed-provider metadata.
// Update this list rather than maintaining separate maps in cli, mcpserver
// and the hints builder.
var BYOKProviders = []BYOKProvider{
	{
		Name:            "brave",
		EnvVars:         []string{"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: false,
		SignupURL:       "https://api.search.brave.com",
		FreeTierNote:    "1000 calls/month — credit card required at signup; Nólë caps usage at the local monthly quota.",
		EnvExample:      "export BRAVE_API_KEY=BSA...",
		Unlocks:         []string{"fast_general_search", "news_search_quality"},
	},
	{
		Name:            "tavily",
		EnvVars:         []string{"TAVILY_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://tavily.com",
		FreeTierNote:    "1000 calls/month, no credit card required.",
		EnvExample:      "export TAVILY_API_KEY=tvly-...",
		Unlocks:         []string{"url_extraction", "semantic_search_quality"},
	},
	{
		Name:            "firecrawl",
		EnvVars:         []string{"FIRECRAWL_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://firecrawl.dev",
		FreeTierNote:    "1000 calls/month free tier — verify dashboard balance before high-volume use.",
		EnvExample:      "export FIRECRAWL_API_KEY=fc-...",
		Unlocks:         []string{"url_extraction"},
	},
}

// LookupBYOK returns the entry for a provider name, or false if not BYOK.
// DDGS is keyless and not present in BYOKProviders.
func LookupBYOK(name string) (BYOKProvider, bool) {
	for _, p := range BYOKProviders {
		if p.Name == name {
			return p, true
		}
	}
	return BYOKProvider{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd <repo> && go test ./internal/core/ -run TestBYOKProvidersInvariants -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <repo>
git add internal/core/byok_metadata.go internal/core/byok_metadata_test.go
git commit -m "feat(core): introduce BYOKProviders single source of truth"
```

---

## Task 2: Migrate cli quota wiring to read from core.BYOKProviders

**Files:**
- Modify: `internal/cli/app.go` (delete local `byokFreeDefaults`, read from `core.BYOKProviders`)
- Verify: existing cli + core tests stay green

- [ ] **Step 1: Read the current cli quota code**

Run: `grep -n "byokFreeDefaults\|providerQuotaEntry\|isProviderPaidMode" internal/cli/app.go`
Expected: a handful of lines including the local `byokFreeDefaults` map declaration and the function(s) that consume it.

- [ ] **Step 2: Run the existing cli + core tests as a baseline**

Run: `cd <repo> && go test ./internal/cli/ ./internal/core/`
Expected: all PASS. Record the count — the same number must still pass after the refactor.

- [ ] **Step 3: Delete `byokFreeDefaults` from `internal/cli/app.go`**

Remove the local map declaration. In its place, update the consumer (commonly `providerQuotaEntry`) to read from `core.BYOKProviders`:

```go
// Before:
//   if defaults, ok := byokFreeDefaults[name]; ok { ... }
// After:
//   if defaults, ok := core.LookupBYOK(name); ok { ... }
```

The fields used will be `defaults.FreeQuota` and `defaults.RefreshWindow` — same shape as the old struct, so the call sites only need the name change.

- [ ] **Step 4: Run tests to verify no regression**

Run: `cd <repo> && go test ./...`
Expected: same number of PASS, zero FAIL.

- [ ] **Step 5: Commit**

```bash
cd <repo>
git add internal/cli/app.go
git commit -m "refactor(cli): read BYOK quota defaults from core source of truth"
```

---

## Task 3: Add SetupSuggestion and SetupTip types

**Files:**
- Modify: `internal/core/types.go` (add types only — no behavior)

- [ ] **Step 1: Open the file and append types after the existing `ProviderStatus` definitions**

Add the following block to `internal/core/types.go`:

```go
// SetupSuggestion describes one missing BYOK key and what configuring it
// would unlock. Surfaced inside ProviderStatusResponse.SetupSuggestions and,
// in compact summary form, inside SearchResponse.SetupTip.
type SetupSuggestion struct {
	MissingKey         string   `json:"missing_key"`
	Impact             string   `json:"impact"`             // "high" | "medium" | "low"
	Unlocks            []string `json:"unlocks"`
	CurrentWorkaround  string   `json:"current_workaround"`
	FreeTier           string   `json:"free_tier"`
	SignupURL          string   `json:"signup_url"`
	EnvExample         string   `json:"env_example"`
}

// SetupTip is the once-per-MCP-session message embedded into the first
// SearchResponse when at least one BYOK key is missing. AI tools surface this
// to the user as a one-time upgrade hint; subsequent search calls on the
// same connection omit the field entirely.
type SetupTip struct {
	Summary string `json:"summary"`
	SeeAlso string `json:"see_also"`
}
```

- [ ] **Step 2: Add fields to existing response types**

In the same file, extend `ProviderStatusResponse` and `SearchResponse`:

```go
type ProviderStatusResponse struct {
	// ...existing fields...
	SetupSuggestions []SetupSuggestion `json:"setup_suggestions,omitempty"`
}

type SearchResponse struct {
	// ...existing fields...
	SetupTip *SetupTip `json:"setup_tip,omitempty"`
}
```

Use a pointer for `SetupTip` so the `omitempty` tag actually omits it on absence (struct values are never empty enough for `omitempty` on a non-pointer).

- [ ] **Step 3: Run the full test suite — types only, nothing breaks**

Run: `cd <repo> && go test ./...`
Expected: all PASS. No new behavior yet.

- [ ] **Step 4: Commit**

```bash
cd <repo>
git add internal/core/types.go
git commit -m "feat(core): add SetupSuggestion and SetupTip types"
```

---

## Task 4: Build setup suggestions — exhaustive truth table

**Files:**
- Create: `internal/core/setup_hints.go`
- Create: `internal/core/setup_hints_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/core/setup_hints_test.go`:

```go
package core

import (
	"reflect"
	"sort"
	"testing"
)

// configuredSet is a small helper so each test case reads as "these providers
// have keys configured." The string keys match the provider names in
// BYOKProviders (lowercase).
func configuredSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func TestBuildSetupSuggestionsExhaustive(t *testing.T) {
	cases := []struct {
		label                string
		configured           map[string]bool
		wantMissing          []string // missing keys, sorted
		wantImpactByProvider map[string]string
	}{
		{
			label:                "no keys configured",
			configured:           configuredSet(),
			wantMissing:          []string{"brave", "firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "high", "firecrawl": "high"},
		},
		{
			label:                "only brave — extract still broken, both extract providers HIGH",
			configured:           configuredSet("brave"),
			wantMissing:          []string{"firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"tavily": "high", "firecrawl": "high"},
		},
		{
			label:                "only tavily — extract works via tavily, others MEDIUM",
			configured:           configuredSet("tavily"),
			wantMissing:          []string{"brave", "firecrawl"},
			wantImpactByProvider: map[string]string{"brave": "medium", "firecrawl": "low"},
		},
		{
			label:                "only firecrawl — extract works via firecrawl",
			configured:           configuredSet("firecrawl"),
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium"},
		},
		{
			label:                "brave + tavily — only firecrawl missing, pure redundancy",
			configured:           configuredSet("brave", "tavily"),
			wantMissing:          []string{"firecrawl"},
			wantImpactByProvider: map[string]string{"firecrawl": "low"},
		},
		{
			label:                "brave + firecrawl — tavily missing, semantic quality MEDIUM",
			configured:           configuredSet("brave", "firecrawl"),
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium"},
		},
		{
			label:                "tavily + firecrawl — brave missing, search slower",
			configured:           configuredSet("tavily", "firecrawl"),
			wantMissing:          []string{"brave"},
			wantImpactByProvider: map[string]string{"brave": "medium"},
		},
		{
			label:                "all three configured — no suggestions",
			configured:           configuredSet("brave", "tavily", "firecrawl"),
			wantMissing:          []string{},
			wantImpactByProvider: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := BuildSetupSuggestions(tc.configured)
			gotKeys := []string{}
			gotImpact := map[string]string{}
			for _, s := range got {
				// Map MissingKey (env var) back to provider name for assertion.
				for _, p := range BYOKProviders {
					for _, ev := range p.EnvVars {
						if ev == s.MissingKey {
							gotKeys = append(gotKeys, p.Name)
							gotImpact[p.Name] = s.Impact
						}
					}
				}
			}
			sort.Strings(gotKeys)
			if !reflect.DeepEqual(gotKeys, tc.wantMissing) {
				t.Errorf("missing providers: got %v, want %v", gotKeys, tc.wantMissing)
			}
			if !reflect.DeepEqual(gotImpact, tc.wantImpactByProvider) {
				t.Errorf("impact map: got %v, want %v", gotImpact, tc.wantImpactByProvider)
			}
		})
	}
}

func TestBuildSetupSuggestionsFieldsArePopulated(t *testing.T) {
	// Sanity check: when a suggestion is produced, all string fields are
	// non-empty so the AI tool has enough to articulate.
	got := BuildSetupSuggestions(configuredSet()) // worst case: 3 suggestions
	if len(got) == 0 {
		t.Fatal("expected suggestions for zero-key case")
	}
	for _, s := range got {
		if s.MissingKey == "" || s.Impact == "" || len(s.Unlocks) == 0 ||
			s.CurrentWorkaround == "" || s.FreeTier == "" || s.SignupURL == "" || s.EnvExample == "" {
			t.Errorf("suggestion has empty field: %+v", s)
		}
	}
}

func TestBuildSetupTipPresenceMatchesSuggestions(t *testing.T) {
	// With no suggestions the tip must be nil. With suggestions present
	// the tip Summary must mention at least one of the missing keys.
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily", "firecrawl"))); tip != nil {
		t.Errorf("expected nil tip when nothing missing, got %+v", tip)
	}
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave")))
	if tip == nil {
		t.Fatal("expected non-nil tip when extract is missing")
	}
	if tip.Summary == "" {
		t.Error("tip.Summary is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd <repo> && go test ./internal/core/ -run "TestBuildSetupSuggestions|TestBuildSetupTip" -v`
Expected: FAIL with "undefined: BuildSetupSuggestions" (compile error).

- [ ] **Step 3: Implement the builder**

Create `internal/core/setup_hints.go`:

```go
package core

import (
	"fmt"
	"sort"
	"strings"
)

// BuildSetupSuggestions inspects the set of configured BYOK providers and
// returns one suggestion per missing key. Suggestions are returned sorted by
// (impact desc, provider name asc) so the most actionable items come first.
//
// `configured` is keyed by provider name (e.g. "brave"). DDGS is keyless and
// is never in this map.
func BuildSetupSuggestions(configured map[string]bool) []SetupSuggestion {
	hasExtractCapable := false
	for _, p := range BYOKProviders {
		if p.SupportsExtract && configured[p.Name] {
			hasExtractCapable = true
			break
		}
	}

	out := []SetupSuggestion{}
	for _, p := range BYOKProviders {
		if configured[p.Name] {
			continue
		}
		impact := classifyImpact(p, hasExtractCapable)
		out = append(out, SetupSuggestion{
			MissingKey:        p.EnvVars[0], // primary env var name
			Impact:            impact,
			Unlocks:           append([]string(nil), p.Unlocks...),
			CurrentWorkaround: currentWorkaroundFor(p, hasExtractCapable),
			FreeTier:          p.FreeTierNote,
			SignupURL:         p.SignupURL,
			EnvExample:        p.EnvExample,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := impactRank(out[i].Impact), impactRank(out[j].Impact)
		if ri != rj {
			return ri < rj // lower rank means higher priority
		}
		return out[i].MissingKey < out[j].MissingKey
	})
	return out
}

func classifyImpact(p BYOKProvider, hasExtractCapable bool) string {
	// HIGH: the provider unlocks a capability that no currently-configured
	// provider can deliver. The only such case today is url_extraction when
	// no extract-capable provider has a key.
	if p.SupportsExtract && !hasExtractCapable {
		return "high"
	}
	// MEDIUM: the provider materially improves a feature that already works
	// — Brave gives faster search than DDGS, Tavily adds semantic-quality
	// even when extract is already covered by Firecrawl.
	if p.Name == "brave" {
		return "medium"
	}
	if p.Name == "tavily" && hasExtractCapable {
		// Extract is covered, but Tavily still adds semantic/people quality.
		return "medium"
	}
	// LOW: the provider only adds redundancy. Today this is Firecrawl when
	// Tavily already covers extract.
	return "low"
}

func impactRank(impact string) int {
	switch impact {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}

func currentWorkaroundFor(p BYOKProvider, hasExtractCapable bool) string {
	switch {
	case p.SupportsExtract && !hasExtractCapable:
		return "AI tool's built-in HTTP fetch (works, but no markdown conversion or paywall handling)"
	case p.Name == "brave":
		return "DDGS keyless fallback (slower, weaker on multilingual queries)"
	case p.Name == "tavily" && hasExtractCapable:
		return "Existing extract provider handles URLs; DDGS handles semantic queries (lower quality)"
	default:
		return "Existing providers cover the capability; this entry is redundancy only"
	}
}

// BuildSetupTip turns a slice of suggestions into the once-per-session
// summary embedded in the first SearchResponse of an MCP connection. Returns
// nil when nothing is missing — the SearchResponse omits the field entirely.
func BuildSetupTip(suggestions []SetupSuggestion) *SetupTip {
	if len(suggestions) == 0 {
		return nil
	}
	high := []string{}
	medium := []string{}
	for _, s := range suggestions {
		switch s.Impact {
		case "high":
			high = append(high, s.MissingKey)
		case "medium":
			medium = append(medium, s.MissingKey)
		}
	}
	// Tip only fires when there's at least one high or medium suggestion.
	// "Low" (pure redundancy) is not worth nagging about on every session.
	if len(high) == 0 && len(medium) == 0 {
		return nil
	}
	var b strings.Builder
	if len(high) > 0 {
		fmt.Fprintf(&b, "Some Nólë features are currently disabled. Set %s to enable them. ", joinOr(high))
	}
	if len(medium) > 0 {
		if len(high) == 0 {
			b.WriteString("Nólë already works with keyless fallbacks. ")
			fmt.Fprintf(&b, "Configuring %s can improve search speed or extract fidelity. ", joinOr(medium))
		} else {
			fmt.Fprintf(&b, "Configuring %s can also improve search speed or extract fidelity. ", joinOr(medium))
		}
	}
	if len(high) > 0 {
		b.WriteString("Until then, Nólë will use keyless/provider fallbacks where available.")
	}
	return &SetupTip{
		Summary: strings.TrimSpace(b.String()),
		SeeAlso: "call provider_status for per-key signup links and env examples",
	}
}

func joinOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd <repo> && go test ./internal/core/ -run "TestBuildSetupSuggestions|TestBuildSetupTip" -v`
Expected: all PASS.

- [ ] **Step 5: Run the full suite — confirm nothing else broke**

Run: `cd <repo> && go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd <repo>
git add internal/core/setup_hints.go internal/core/setup_hints_test.go
git commit -m "feat(core): build setup suggestions and first-session tip"
```

---

## Task 5: Wire setup_suggestions into provider_status (CLI + MCP)

**Files:**
- Modify: `internal/cli/providers.go`
- Modify: `internal/cli/providers_test.go`
- Modify: the MCP server file that handles `provider_status` (identified in Task 0)

- [ ] **Step 1: Write the failing CLI test**

In `internal/cli/providers_test.go`, add:

```go
func TestProvidersJSONIncludesSetupSuggestionsWhenKeysMissing(t *testing.T) {
	// Run providers --json with no provider envs set; output must contain
	// setup_suggestions covering brave, tavily, and firecrawl.
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	out := captureProvidersJSON(t) // existing helper in this file; if absent, use runCLI("providers", "--json")
	var parsed struct {
		SetupSuggestions []struct {
			MissingKey string `json:"missing_key"`
			Impact     string `json:"impact"`
		} `json:"setup_suggestions"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(parsed.SetupSuggestions) != 3 {
		t.Fatalf("got %d suggestions, want 3 (brave, tavily, firecrawl)", len(parsed.SetupSuggestions))
	}
	wantKeys := map[string]bool{"BRAVE_API_KEY": true, "TAVILY_API_KEY": true, "FIRECRAWL_API_KEY": true}
	for _, s := range parsed.SetupSuggestions {
		if !wantKeys[s.MissingKey] {
			t.Errorf("unexpected missing key %q", s.MissingKey)
		}
		if s.Impact == "" {
			t.Errorf("suggestion %q has empty impact", s.MissingKey)
		}
	}
}
```

If `captureProvidersJSON` doesn't exist in the file already, write a small inline helper that invokes the providers command and returns stdout. Match the existing test style.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd <repo> && go test ./internal/cli/ -run TestProvidersJSONIncludesSetupSuggestionsWhenKeysMissing -v`
Expected: FAIL — `len(parsed.SetupSuggestions) == 0`.

- [ ] **Step 3: Wire suggestions into the provider_status response**

In `internal/cli/providers.go`, find where the `ProviderStatusResponse` value is assembled. Compute a `configured map[string]bool` by checking each `BYOKProvider`'s env vars; pass it to `core.BuildSetupSuggestions`; assign to `response.SetupSuggestions`:

```go
configured := map[string]bool{}
for _, p := range core.BYOKProviders {
	for _, ev := range p.EnvVars {
		if os.Getenv(ev) != "" {
			configured[p.Name] = true
			break
		}
	}
}
response.SetupSuggestions = core.BuildSetupSuggestions(configured)
```

- [ ] **Step 4: Run the CLI test — it should pass**

Run: `cd <repo> && go test ./internal/cli/ -run TestProvidersJSONIncludesSetupSuggestionsWhenKeysMissing -v`
Expected: PASS.

- [ ] **Step 5: Wire suggestions into the MCP provider_status handler**

Open the MCP server file identified in Task 0 (likely `internal/mcpserver/server.go` or a tools file). Locate the `provider_status` handler. Apply the same `configured` map construction and assign to the response before returning. The helper is in core, so the change is two new lines (build the map, pass to `BuildSetupSuggestions`) plus one assignment.

- [ ] **Step 6: Add an MCP test for the same behavior**

In the MCP server test file, add (adapt the test harness to whatever the existing tests use):

```go
func TestMCPProviderStatusIncludesSetupSuggestionsWhenKeysMissing(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	// Call the in-process handler; existing tests in this file show the pattern.
	resp := callMCPProviderStatus(t)
	if len(resp.SetupSuggestions) != 3 {
		t.Fatalf("got %d suggestions, want 3", len(resp.SetupSuggestions))
	}
}
```

- [ ] **Step 7: Run the full suite**

Run: `cd <repo> && go test ./...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
cd <repo>
git add internal/cli/providers.go internal/cli/providers_test.go internal/mcpserver/
git commit -m "feat(provider-status): include setup suggestions for missing keys"
```

---

## Task 6: First-of-session setup_tip on search responses (MCP-only)

**Files:**
- Modify: the MCP server file that defines the server struct (Task 0)
- Modify: the MCP search handler in the same package
- Modify: the MCP server test file

The CLI `nole search` command does NOT emit `setup_tip` — it's an MCP-only behavior because the CLI has no "session" concept (every invocation is a fresh process). The CLI quietly omits the field.

- [ ] **Step 1: Add `tipEmitted bool` to the MCP server struct**

In the file holding the server constructor (identified in Task 0), add a `tipEmitted bool` field to the struct. Initialize it to `false` in the constructor. No mutex needed yet — stdio MCP handles requests serially per connection.

- [ ] **Step 2: Write the failing test for first-call emission**

In the MCP test file, add:

```go
func TestMCPSearchTipEmittedOncePerSession(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	srv := newTestMCPServer(t) // existing helper or equivalent

	first := callMCPSearch(t, srv, "what is mcp")
	if first.SetupTip == nil {
		t.Fatal("first search response missing setup_tip")
	}
	if first.SetupTip.Summary == "" {
		t.Error("first tip Summary is empty")
	}

	second := callMCPSearch(t, srv, "another query")
	if second.SetupTip != nil {
		t.Errorf("second search response should not include setup_tip, got %+v", second.SetupTip)
	}
}

func TestMCPSearchTipOmittedWhenAllKeysConfigured(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "fake-brave-key")
	t.Setenv("TAVILY_API_KEY", "fake-tavily-key")
	t.Setenv("FIRECRAWL_API_KEY", "fake-firecrawl-key")
	srv := newTestMCPServer(t)
	resp := callMCPSearch(t, srv, "anything")
	if resp.SetupTip != nil {
		t.Errorf("setup_tip should be nil when no keys missing, got %+v", resp.SetupTip)
	}
}
```

If `callMCPSearch` doesn't exist, write a tiny helper using the existing in-process test harness — the existing search MCP tests will show the shape.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd <repo> && go test ./internal/mcpserver/ -run TestMCPSearchTip -v`
Expected: FAIL — `first.SetupTip is nil`.

- [ ] **Step 4: Wire the tip into the search handler**

In the MCP search handler (probably the same file or a sibling), after the search result is assembled and before returning:

```go
if !s.tipEmitted {
	configured := buildConfiguredMap() // helper shared with provider_status path; extract if duplicated
	suggestions := core.BuildSetupSuggestions(configured)
	if tip := core.BuildSetupTip(suggestions); tip != nil {
		response.SetupTip = tip
		s.tipEmitted = true
	}
}
```

`s` is the server receiver. The flag is set only when a tip was actually emitted, so sessions that have no upgrades available never set the flag (harmless, but tidy).

- [ ] **Step 5: Run the tests — they should pass**

Run: `cd <repo> && go test ./internal/mcpserver/ -run TestMCPSearchTip -v`
Expected: both tests PASS.

- [ ] **Step 6: Run the full suite**

Run: `cd <repo> && go test ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd <repo>
git add internal/mcpserver/
git commit -m "feat(mcp): emit setup_tip on first search of each session"
```

---

## Task 7: Conditionally register the extract MCP tool

**Files:**
- Modify: the MCP server file with the tool registrations (Task 0)
- Modify: the MCP server test file

- [ ] **Step 1: Write the failing test**

In the MCP test file:

```go
func TestMCPExtractToolHiddenWhenNoExtractCapableKey(t *testing.T) {
	// Only brave key set; brave has no extract capability — Nólë's MCP
	// surface must not advertise extract, so the AI tool falls back to its
	// own fetcher instead of hitting a no_free_quota error.
	t.Setenv("BRAVE_API_KEY", "fake-brave-key")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServer(t)
	tools := listMCPTools(t, srv) // existing helper or implement using the server's tool list
	for _, name := range tools {
		if name == "extract" || name == "mcp__nole__extract" {
			t.Fatalf("extract tool should NOT be registered when no extract-capable key is set; tools = %v", tools)
		}
	}
}

func TestMCPExtractToolPresentWhenExtractCapableKeyExists(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "fake-tavily-key")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServer(t)
	tools := listMCPTools(t, srv)
	found := false
	for _, name := range tools {
		if name == "extract" || name == "mcp__nole__extract" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("extract tool should be registered when tavily key is set; tools = %v", tools)
	}
}
```

The exact tool name comparison (`"extract"` vs `"mcp__nole__extract"`) depends on how this codebase names tools. The OR in the loop tolerates either convention; tighten to whichever the codebase uses once Task 0 has surfaced it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd <repo> && go test ./internal/mcpserver/ -run TestMCPExtractTool -v`
Expected: the "hidden" test FAILs because extract is always registered today.

- [ ] **Step 3: Gate the extract registration**

In the MCP server constructor / setup function, wrap the `AddTool(extractTool, ...)` (or equivalent) call:

```go
hasExtractCapable := false
for _, p := range core.BYOKProviders {
	if !p.SupportsExtract {
		continue
	}
	for _, ev := range p.EnvVars {
		if os.Getenv(ev) != "" {
			hasExtractCapable = true
			break
		}
	}
	if hasExtractCapable {
		break
	}
}
if hasExtractCapable {
	server.AddTool(extractTool, extractHandler) // existing call, now gated
}
```

If the existing code registers tools in a slice/list pattern, conditionally append instead.

- [ ] **Step 4: Run the tests — they should pass**

Run: `cd <repo> && go test ./internal/mcpserver/ -run TestMCPExtractTool -v`
Expected: both tests PASS.

- [ ] **Step 5: Run the full suite**

Run: `cd <repo> && go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd <repo>
git add internal/mcpserver/
git commit -m "feat(mcp): hide extract tool when no extract-capable key configured"
```

---

## Task 8: Extend benchmark-claims guard to scan new strings

**Files:**
- Modify: `internal/bench/claims_guard_test.go`

The existing claims guard rejects superlative or quantitative provider rankings in docs and Markdown evidence summaries. The new setup-hint strings live in code and in JSON responses; we add a check that the strings in `core.BYOKProviders` and the suggestions output don't violate the same rules.

- [ ] **Step 1: Locate the existing guard**

Run: `grep -n "claims\|ranking\|provider" internal/bench/claims_guard_test.go`
Expected: the existing patterns banning words like "best", "always works", quantitative comparisons.

- [ ] **Step 2: Add a test extending coverage to setup-hint strings**

Append to `internal/bench/claims_guard_test.go`:

```go
func TestSetupHintStringsAreSanitized(t *testing.T) {
	// Every user-facing string in BYOKProviders flows into provider_status
	// and search_tip output. Reuse the existing banned-words pattern so
	// "Tavily is the best for X" or "Brave is 3x faster than Y" can't sneak
	// in via a metadata edit.
	bad := []string{"best", "fastest", "always works", "guaranteed", "unbeatable", "outperforms"}
	check := func(field, value string) {
		lower := strings.ToLower(value)
		for _, b := range bad {
			if strings.Contains(lower, b) {
				t.Errorf("%s contains banned word %q: %q", field, b, value)
			}
		}
	}
	for _, p := range core.BYOKProviders {
		check(p.Name+".FreeTierNote", p.FreeTierNote)
	}
	// Also scan suggestions output for the zero-key worst case.
	for _, s := range core.BuildSetupSuggestions(map[string]bool{}) {
		check(s.MissingKey+".CurrentWorkaround", s.CurrentWorkaround)
		check(s.MissingKey+".FreeTier", s.FreeTier)
	}
}
```

If `strings` is not already imported in the file, add it.

- [ ] **Step 3: Run the test — confirm it passes against current strings**

Run: `cd <repo> && go test ./internal/bench/ -run TestSetupHintStringsAreSanitized -v`
Expected: PASS (the strings in Task 1 were written to avoid the banned words).

- [ ] **Step 4: Commit**

```bash
cd <repo>
git add internal/bench/claims_guard_test.go
git commit -m "test(bench): claims guard now covers setup-hint metadata strings"
```

---

## Task 9: Update PROVIDER-KEYS.md addendum

**Files:**
- Modify: `docs/PROVIDER-KEYS.md`

- [ ] **Step 1: Read the existing doc**

Run: `cat docs/PROVIDER-KEYS.md | head -60`
Expected: the existing key/quota table.

- [ ] **Step 2: Append the addendum**

Add a new section after the existing "Environment variables" section:

```markdown
## Partial-key behavior

Nólë is designed as a strict enhancement of whatever AI tool consumes it. The MCP surface adapts to which keys are configured:

- **No keys at all:** `mcp__nole__search` is registered and routes via DDGS (keyless). `mcp__nole__extract` is **not** registered — the AI tool uses its own built-in HTTP fetch instead. `mcp__nole__provider_status` and `mcp__nole__budget_status` are always available.
- **Only `BRAVE_API_KEY`:** Search routes Brave-first with DDGS fallback. Extract is still not registered (Brave has no extract capability); the AI tool's built-in fetch handles URL content.
- **Only `TAVILY_API_KEY` or only `FIRECRAWL_API_KEY`:** Both `mcp__nole__search` and `mcp__nole__extract` are registered.
- **Any two or all three:** Full feature set with redundancy on the overlapping capability.

If you add a key mid-session, restart your AI tool (or its MCP connection) so the new tool surface is picked up.

`provider_status` returns a `setup_suggestions` array listing every missing key, what configuring it would unlock, where to sign up, and an `impact` rating (`high` / `medium` / `low`) so AI tools can decide what to surface. The first `search` response of an MCP session also carries a compact `setup_tip` summarizing the same information.
```

- [ ] **Step 3: Verify the doc still passes claims-guard**

Run: `cd <repo> && go test ./internal/bench/ -run TestBenchmarkClaimsGuard`
Expected: PASS (no superlative or quantitative rankings introduced).

- [ ] **Step 4: Commit**

```bash
cd <repo>
git add docs/PROVIDER-KEYS.md
git commit -m "docs(provider-keys): document partial-key MCP behavior and setup hints"
```

---

## Task 10: End-to-end smoke + PR

**Files:**
- No edits — verification and PR.

- [ ] **Step 1: Build the binary fresh from this branch**

Run: `cd <repo> && go build -o /tmp/nole-partial-keys . && /tmp/nole-partial-keys doctor | head -20`
Expected: doctor reports successfully; no panics.

- [ ] **Step 2: Smoke-test the MCP server with no keys**

Run: `env -i HOME=$HOME PATH=$PATH /tmp/nole-partial-keys providers --json | python3 -m json.tool | head -60`
Expected: `setup_suggestions` array present, contains three entries (brave, tavily, firecrawl); no provider has a key configured.

- [ ] **Step 3: Smoke-test with only Brave**

Run: `env -i HOME=$HOME PATH=$PATH BRAVE_API_KEY=fake /tmp/nole-partial-keys providers --json | python3 -m json.tool | head -60`
Expected: `setup_suggestions` has two entries (tavily, firecrawl), both with `"impact": "high"`.

- [ ] **Step 4: Full test suite final pass**

Run: `cd <repo> && go test ./... -count=1`
Expected: all PASS, no cached results.

- [ ] **Step 5: Push and open PR**

```bash
cd <repo>
git push -u origin spec/partial-keys-graceful-degradation
gh pr create --base main --head spec/partial-keys-graceful-degradation \
  --title "feat: partial-keys graceful degradation across MCP surface" \
  --body-file <(cat <<'EOF'
## Summary

- Single source of truth (`core.BYOKProviders`) drives both quota wiring and setup-hint rendering.
- `mcp__nole__extract` is no longer registered when no extract-capable provider has a key configured — AI tools fall back to their built-in HTTP fetch instead of hitting an opaque `no_free_quota` error.
- `provider_status` returns a `setup_suggestions` array with per-missing-key impact (`high`/`medium`/`low`), unlocks, signup URL, and env example.
- The first `search` response per MCP session carries a compact `setup_tip`; subsequent calls omit it.
- English-only output; AI tools translate to the user's language.

## Why

Nólë is BYOK. Many users configure one or two of the three keys. Previously, calling extract on a partial install returned `no_free_quota: no free provider available for task "extract"` — opaque enough that AI tools couldn't recover gracefully and the user perceived nole as broken. This change makes nole a strict enhancement under any key configuration.

## Spec & rationale

`docs/plans/2026-05-24-partial-keys-graceful-degradation.md`

## Test plan

- [x] `go test ./...` — full suite green, new tests cover all 8 key-configuration permutations.
- [x] `go test ./internal/core/ -run TestBuildSetupSuggestions` — exhaustive truth table.
- [x] `go test ./internal/mcpserver/ -run "TestMCPExtractTool|TestMCPSearchTip"` — MCP tool surface gating + first-of-session tip.
- [x] Built binary smoke: providers --json with 0 keys → 3 high-impact suggestions; with brave only → 2 high-impact suggestions for tavily/firecrawl.

@codex review
EOF
)
```

Expected: PR opened, URL printed.

- [ ] **Step 6: Trigger Codex review explicitly (top-level comment)**

```bash
gh pr comment <PR_NUMBER> --repo dorukardahan/nole --body "@codex review"
```

Replace `<PR_NUMBER>` with the number printed in Step 5.

---

## Self-Review

After completing all tasks, re-read the spec and confirm:

1. **Hide extract when no extract-capable provider configured** — Task 7 ✓
2. **`setup_suggestions` on provider_status** — Task 5 ✓
3. **First-of-session `setup_tip` on search** — Task 6 ✓
4. **English-only with AI translation** — every string in Tasks 1 and 4 is English; no localization layer added ✓
5. **Single source of truth** — Tasks 1 and 2 establish `core.BYOKProviders`; Task 5 wires it for status; Task 7 wires it for tool registration ✓
6. **`impact` field with `high`/`medium`/`low`** — Task 4's `classifyImpact` ✓
7. **Tier 1 surfaces (Claude Code / Codex / OpenClaw / Hermes)** — behavior is uniform via MCP protocol; no client-specific code ✓
8. **Out-of-scope items respected** — no `nole setup keys` wizard, no per-language localization, no route-matrix changes ✓
9. **Single PR roll-out** — Task 10 opens one PR ✓
10. **Additive fields** — `SetupSuggestions` and `SetupTip` use `omitempty`; existing consumers untouched ✓
