# Nólë v0.2.3 release notes

Nólë v0.2.3 refreshes provider routing after a new task-provider live benchmark
and fixes the comprehensive benchmark harness so local extraction is measured
alongside remote providers.

## Highlights

- Default routes now use task-specific evidence from a 2026-05-26 local live
  benchmark covering 39 public cases and 150 provider measurements.
- Search routes are no longer treated as one broad ordering. General search,
  docs, news, code, academic, fact-check, pricing, people, social, semantic and
  research tasks each have their own provider order.
- Configured local Scrapling now leads the default `extract` route. If the local
  runtime is unavailable, Nólë skips it through normal provider status checks
  and falls back to Firecrawl and Tavily.
- DDGS remains available as keyless search fallback, but moves to the end of
  default search routes after the live sample observed repeated rate limits.

## Route summary

Current route leaders:

- `general`: Brave
- `docs`, `news`, `factcheck`, `pricing`, `people`, `social`, `research`:
  Firecrawl
- `code`, `academic`, `semantic`: Tavily
- `extract`: Scrapling, then Firecrawl, then Tavily

See `docs/ROUTE-EVIDENCE.md` for the sanitized evidence table and caveats.

## Benchmark harness fixes

- `nole bench --live --comprehensive` now loads `~/.config/nole/.env` before
  constructing providers, matching normal CLI/MCP startup behavior.
- Comprehensive benchmark runs now include the local Scrapling provider when it
  is configured.
- The doctor free-tier BYOK test now uses an isolated quota ledger so local
  developer state cannot change expected test output.

## Safety notes

The benchmark evidence is summary-only. It does not include raw provider
payloads, provider key values, auth headers, private URLs or private queries.
The route matrix is still a local evidence-backed default, not a provider SLA or
global provider ranking.
