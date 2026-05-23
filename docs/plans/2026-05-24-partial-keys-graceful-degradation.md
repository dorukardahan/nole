# Partial-Keys Graceful Degradation

> Design spec — 2026-05-24
> Status: draft (pending user approval before implementation plan)

## Problem

Nólë is BYOK. The pitch is "bring your Brave / Tavily / Firecrawl keys and we route between them." In practice many users will configure **one or two of the three** and leave the others empty:

- Some skip a signup flow that asks for a credit card (Brave's free tier does).
- Some only want search and never extract.
- Some try Nólë before deciding which provider to commit to.

The system already partially handles this: search has DDGS as a keyless fallback, so search calls succeed with any subset of keys (including none). **Extract is the gap.** The current route matrix for extract is `{tavily, firecrawl}`; with neither key set, extract returns `no_free_quota: no free provider available for task "extract"`. To an AI tool (Claude Code, Codex, OpenClaw, Hermes) consuming Nólë over MCP, that error is opaque — the model has no way to know that adding a key would unlock the feature, or that it should fall back to its own built-in HTTP fetch tool.

The result, in the worst case, is a **regression** versus running the AI tool without Nólë at all: the AI tool would have used its own WebFetch and succeeded; with Nólë, it tries `mcp__nole__extract` first, hits an opaque error, and stops.

## Principle

**Nólë must be a strict enhancement.** Any configuration state — zero keys, one key, all keys — must be at least as good as the same AI tool running without Nólë. Never worse.

A second principle, derived from the first: **don't be silent about upgrade paths.** Users who could unlock a feature by adding a key shouldn't have to read documentation to find out; the system should mention it in passing, once, when it's relevant.

## Target surfaces

This design must work uniformly across the four MCP clients we treat as Tier 1:

- Claude Code
- Codex
- OpenClaw
- Hermes

Behavior on those clients is the bar; other MCP clients (Cursor, Continue, etc.) follow.

## Design

Three independent behaviors:

### 1. Hide extract when no extract-capable provider is configured

At MCP server startup, Nólë inspects the configured providers and their declared capabilities. If no provider declares `CapabilityExtract` AND has its key configured, **`mcp__nole__extract` is not registered** with the MCP server.

The AI tool sees only the tools that work. When the user says "extract this URL," the model uses its own built-in fetch (WebFetch on Claude Code; equivalent on the others) instead of calling a Nólë tool that would fail.

This is the cleanest path to "Nólë never makes things worse." Concretely:

- Zero keys → `mcp__nole__search` registered (via DDGS), `mcp__nole__extract` not registered.
- Only Brave → search registered, extract not registered (Brave has no extract capability).
- Only Tavily or Firecrawl → both search and extract registered.
- Brave + Tavily, Brave + Firecrawl, or all three → both registered.

The provider list itself stays internally complete — `provider_status` always returns all four providers, including the ones that are unavailable. What changes is the *MCP tool surface*.

**Trade-off accepted:** Users who add an extract-capable key mid-session need to restart their AI tool (or its MCP connection) for the new tool to appear. Documented; acceptable for v1.

### 2. `setup_suggestions` field on `provider_status`

The `mcp__nole__provider_status` response (and the equivalent CLI `nole providers --json`) gains a new top-level field: `setup_suggestions`. It lists, for each missing capability, what key would unlock it.

```json
{
  "providers": [ ... existing per-provider entries ... ],
  "setup_suggestions": [
    {
      "missing_key": "TAVILY_API_KEY",
      "impact": "high",
      "unlocks": ["url_extraction", "semantic_search_quality"],
      "current_workaround": "AI tool's built-in HTTP fetch for URL extraction; DDGS for semantic search",
      "free_tier": "1000 calls/month, no credit card",
      "signup_url": "https://tavily.com",
      "env_example": "export TAVILY_API_KEY=tvly-..."
    },
    {
      "missing_key": "FIRECRAWL_API_KEY",
      "impact": "high",
      "unlocks": ["url_extraction"],
      "current_workaround": "AI tool's built-in HTTP fetch",
      "free_tier": "1000 calls/month",
      "signup_url": "https://firecrawl.dev",
      "env_example": "export FIRECRAWL_API_KEY=fc-..."
    },
    {
      "missing_key": "BRAVE_API_KEY",
      "impact": "medium",
      "unlocks": ["fast_general_search", "news_search_quality"],
      "current_workaround": "DDGS fallback (slower, less reliable on multilingual queries)",
      "free_tier": "1000 calls/month — credit card required for signup",
      "signup_url": "https://api.search.brave.com",
      "env_example": "export BRAVE_API_KEY=BSA..."
    }
  ]
}
```

`impact` levels:
- `high` — without this key, an entire feature is unavailable (or in the unlucky-only-Brave case, `url_extraction` is missing).
- `medium` — the feature works, but adding the key materially improves speed or quality.
- `low` — adding the key provides redundancy only (no new feature, no measurable quality gain).

The AI tool decides what to surface. The convention encoded in the tool description is: "If a `setup_suggestions` entry has impact `high` or `medium`, mention it once per conversation when relevant. Skip `low`."

The `setup_suggestions` array is built from the same source of truth (`internal/cli/app.go:byokFreeDefaults`) that drives the existing per-provider `free_tier-BYOK` classification, so the doc surface and the runtime behavior cannot drift.

### 3. First-of-session tip on `search` responses

The `mcp__nole__search` response (when relevant) carries a `setup_tip` field on the **first call of an MCP session only**. Subsequent calls in the same session omit it. The MCP server tracks "have I emitted a tip yet this connection" in process memory; a new connection resets the flag.

```json
{
  "query": "...",
  "results": [ ... ],
  "route_trace": [ ... ],
  "setup_tip": {
    "summary": "URL extraction is disabled — add TAVILY_API_KEY or FIRECRAWL_API_KEY to enable it inside Nólë. Until then your AI tool will use its built-in HTTP fetch.",
    "see_also": "call provider_status for details and signup links"
  }
}
```

This is the one place where Nólë proactively raises the topic to an AI tool that didn't ask about provider status. Limiting it to the first call keeps it from becoming a nag.

If the session has no upgradeable capabilities (all keys configured), `setup_tip` is omitted entirely — no field, no overhead.

## Internationalization

All emitted strings are English. The AI tool — Claude / Codex / OpenClaw's underlying LLM / Hermes' underlying LLM — handles translation to the user's conversational language. This follows the standard MCP design pattern: structured data plus English defaults, model handles presentation. It also matches how Nólë already emits `routing_insight` text.

A future CLI-side `--lang tr` for `nole doctor` is out of scope for this design; track separately if it becomes a real need.

## Files touched (forecast)

- `internal/core/types.go` — add `SetupSuggestion`, `ProviderStatusResponse.SetupSuggestions`, `SearchResponse.SetupTip`.
- `internal/core/setup_hints.go` (new) — build `setup_suggestions` from configured providers and `byokFreeDefaults`. Pure function, easy to test.
- `internal/mcpserver/*` — at startup, inspect configured providers; conditionally register the extract tool. Track first-call flag per connection.
- `internal/cli/app.go` — `byokFreeDefaults` becomes the single source of truth for both quotas (existing) and setup hints (new). No behavior change for already-configured providers.
- `internal/cli/providers.go` — JSON output gains `setup_suggestions`.
- `docs/PROVIDER-KEYS.md` — short addendum: "Nólë's MCP server hides the `extract` tool if neither Tavily nor Firecrawl is configured. Add a key and restart your AI tool to enable."
- Tests in `internal/core/setup_hints_test.go`, `internal/mcpserver/*_test.go`, `internal/cli/providers_test.go`.

## Out of scope

- A `nole setup keys` interactive wizard. The `.env` workflow is already documented; a wizard is its own design.
- Auto-detecting partial-string keys (e.g., trimming surrounding whitespace). Handled separately if it turns out to be a support burden.
- Changes to the route matrix itself. Capability-based hiding affects only the MCP tool surface; routing logic is unchanged.
- Per-language localization in Nólë itself. AI tools translate.
- Quantitative claims about quality gains ("3x faster", "20% more accurate"). The benchmark-claims guard would reject those; the design uses qualitative language ("better for paywalled / JS-rendered content") instead.

## Testing

Three property-level guarantees the test suite must pin:

1. **MCP tool surface matches configured capability.** With no extract-capable provider, the MCP server does not advertise `extract`. With at least one (Tavily or Firecrawl), it does. Tested via the existing MCP server test harness with controlled env.
2. **`setup_suggestions` accurately reflects the gap.** Given a configured-providers set, the suggestions list contains exactly the keys that would change behavior, with the right `impact` level. Pure-function test, no provider mocks.
3. **`setup_tip` appears exactly once per MCP session.** Repeated `search` calls on the same connection emit it only on call #1. A new MCP connection resets the flag. Tested via the MCP server in-process harness.

The benchmark-claims guard (`internal/bench/claims_guard_test.go`) gets a new test ensuring `setup_suggestions` strings don't fall foul of its rules — no superlative or quantitative provider rankings.

## Roll-out

- Single PR: changes are tightly coupled (the tool-hiding, the suggestion data, the first-call tip all share the same source-of-truth helper). Splitting into two would force one to land with the helper exported but unused, which is uglier than landing them together.
- No migration. The new fields are additive; existing consumers of `provider_status` and `search` responses keep working untouched.
- README and `docs/PROVIDER-KEYS.md` get the addendum in the same PR.
