package core

import (
	"reflect"
	"sort"
	"strings"
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
		extractAvailable     bool     // keyless httpfetch backstop (always true in the default service)
		wantMissing          []string // missing provider names, sorted
		wantImpactByProvider map[string]string
	}{
		// extractAvailable=true is the PRODUCTION reality: the keyless httpfetch
		// backstop is always registered, so extract works out of the box and a
		// missing keyed extract provider is a fidelity upgrade (medium), not a
		// disabled-feature unlock (high).
		{
			label:                "no keys, keyless extract baseline — only key-gated providers are suggested",
			configured:           configuredSet(),
			extractAvailable:     true,
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium"},
		},
		{
			label:                "only brave, keyless extract baseline — tavily remains an optional upgrade",
			configured:           configuredSet("brave"),
			extractAvailable:     true,
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium"},
		},
		{
			label:                "only tavily — brave remains an optional search upgrade",
			configured:           configuredSet("tavily"),
			extractAvailable:     true,
			wantMissing:          []string{"brave"},
			wantImpactByProvider: map[string]string{"brave": "medium"},
		},
		{
			label:                "only firecrawl key — brave and tavily remain optional upgrades",
			configured:           configuredSet("firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium"},
		},
		{
			label:                "brave + tavily — keyless firecrawl needs no setup suggestion",
			configured:           configuredSet("brave", "tavily"),
			extractAvailable:     true,
			wantMissing:          []string{},
			wantImpactByProvider: map[string]string{},
		},
		{
			label:                "brave + firecrawl — tavily missing, semantic quality MEDIUM",
			configured:           configuredSet("brave", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium"},
		},
		{
			label:                "tavily + firecrawl — brave missing, search slower",
			configured:           configuredSet("tavily", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"brave"},
			wantImpactByProvider: map[string]string{"brave": "medium"},
		},
		{
			label:                "all three configured — no suggestions",
			configured:           configuredSet("brave", "tavily", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{},
			wantImpactByProvider: map[string]string{},
		},
		// Even in a custom build with no extract provider registered, a Firecrawl
		// account key is not a meaningful setup suggestion: the provider itself is
		// absent. Only actually key-gated providers are suggested.
		{
			label:                "no keys AND no keyless extract — tavily is the key-gated extract unlock",
			configured:           configuredSet(),
			extractAvailable:     false,
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "high"},
		},
		{
			label:                "only brave, no keyless extract — tavily remains the key-gated extract provider",
			configured:           configuredSet("brave"),
			extractAvailable:     false,
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "high"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := BuildSetupSuggestions(tc.configured, tc.extractAvailable)
			gotKeys := []string{}
			gotImpact := map[string]string{}
			for _, s := range got {
				// Map MissingKey (env var) back to provider name for assertion.
				for _, p := range BYOKProviders() {
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
	got := BuildSetupSuggestions(configuredSet(), true) // zero-key case: only genuinely key-gated providers
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
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily", "firecrawl"), true)); tip != nil {
		t.Errorf("expected nil tip when nothing missing, got %+v", tip)
	}
	// Firecrawl works keylessly, so brave + tavily configured leaves no suggestions.
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily"), true)); tip != nil {
		t.Errorf("expected nil tip for LOW-only suggestions, got %+v", tip)
	}
	// Only brave: Tavily is still a MEDIUM optional upgrade, so the tip is
	// present (a quality nudge, not "disabled").
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave"), true))
	if tip == nil {
		t.Fatal("expected non-nil tip when MEDIUM fidelity upgrades are available")
	}
	if tip.Summary == "" {
		t.Error("tip.Summary is empty")
	}
}

func TestBuildSetupTipFramesZeroKeyBaselineAsWorkingUpgradePath(t *testing.T) {
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet(), true))
	if tip == nil {
		t.Fatal("expected setup_tip for missing medium-impact provider upgrades")
	}
	summary := tip.Summary
	lower := strings.ToLower(summary)

	for _, noisy := range []string{"disabled", "currently disabled", "built-in fallback", "built-in fallbacks"} {
		if strings.Contains(lower, noisy) {
			t.Fatalf("zero-key setup_tip should not use alarmist fallback copy %q: %s", noisy, summary)
		}
	}
	for _, want := range []string{"keyless", "already works", "improve"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("zero-key setup_tip should frame keys as optional upgrades and mention %q: %s", want, summary)
		}
	}
	if !strings.Contains(summary, "can improve search speed or extract fidelity") {
		t.Fatalf("zero-key setup_tip should use speed/fidelity upgrade copy: %s", summary)
	}
	for _, key := range []string{"BRAVE_API_KEY", "TAVILY_API_KEY"} {
		if !strings.Contains(summary, key) {
			t.Fatalf("setup_tip should still name missing key %s: %s", key, summary)
		}
	}
	if strings.Contains(summary, "FIRECRAWL_API_KEY") {
		t.Fatalf("setup_tip must not ask for a key when Firecrawl keyless mode is available: %s", summary)
	}
	if tip.SeeAlso != "call provider_status for per-key signup links and env examples" {
		t.Fatalf("unexpected see_also: %q", tip.SeeAlso)
	}
}

func TestBuildSetupTipKeepsDisabledCopyForTrueHighImpactMissingFeature(t *testing.T) {
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet(), false))
	if tip == nil {
		t.Fatal("expected setup_tip when extract is truly unavailable")
	}
	summary := tip.Summary
	lower := strings.ToLower(summary)
	if !strings.Contains(lower, "disabled") {
		t.Fatalf("true high-impact missing feature should still use disabled wording: %s", summary)
	}
	if !strings.Contains(summary, "TAVILY_API_KEY") || strings.Contains(summary, "FIRECRAWL_API_KEY") {
		t.Fatalf("only genuinely key-gated high-impact extract providers should be named: %s", summary)
	}
	if strings.Contains(lower, "built-in fallback") {
		t.Fatalf("high-impact copy should not reintroduce AI-tool built-in fallback wording: %s", summary)
	}
}
