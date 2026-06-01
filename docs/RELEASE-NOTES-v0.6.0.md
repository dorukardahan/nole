# Nólë v0.6.0 release notes

Nólë v0.6.0 makes the pipe **cheaper and easier for agents to consume**: fewer
wasted tokens, fewer round-trips, and the richest output (multi-source evidence)
finally reachable over MCP and REST. Nólë stays a dumb-but-honest gateway — it
hands the agent evidence, never a composed answer.

## New for agents

- **`search_and_extract`** (MCP tool + `POST /api/search_and_extract`) — search,
  then extract the top result(s) in a single call. Collapses the dominant
  "search then read the top hit" workflow from two round-trips into one.
  `extract_top` (default 1, max 3) controls how many top results are read. A
  per-URL extract failure is non-fatal and recorded in `extract_errors`, so one
  bad URL never sinks the call. The SSRF preflight still runs per URL.
- **`research` over MCP + REST** — the multi-step search→extract pass (the same
  pipeline `nole research` uses) is now an MCP `research` tool and a `POST
  /api/research` route. It returns the deduplicated `sources` and `extracts` for
  the agent to synthesize.
- **`include_trace` opt-in** — the `search` and `extract` surfaces accept an
  `include_trace` boolean to return the full per-attempt `route_trace` when
  debugging.

## Changed (read this if you parse Nólë's output)

- **`route_trace` is now opt-in on MCP/REST.** The `search` and `extract` success
  responses omit the per-attempt `route_trace` debug blob by default; the compact
  `routing_insight` string is still always present. Pass `include_trace: true` to
  get the full trace. The CLI is unchanged — `--insight off|compact|verbose` still
  governs trace output there. Rationale: the trace was inflating every agent
  turn's context for a debugging aid most calls never read.
- **`research` returns evidence, not an answer.** `nole research` and the new
  research MCP/REST surfaces no longer return a composed `summary`. The previous
  `summary` was a string-concatenation of page extracts (nav cruft included) — a
  degraded answer at odds with "the agent synthesizes." It is removed; you get
  clean `sources` + `extracts` instead.

## Honest notes

- `search_and_extract` can return up to 3 full extract bodies in one response —
  potentially large in an agent's context. Use `extract_top: 1` (the default) or
  the plain `search` tool when you want a smaller payload.
- The research pipeline moved into the core package so MCP/REST can share it; a
  degraded research sub-step now logs to the `nole serve` / `nole mcp` server's
  stderr.

## Verified

- `go build ./...`, `go vet ./...`
- `go test -race ./...` (incl. new tests: `SearchAndExtract` non-fatal/SSRF-
  preflight-first/clamp/dedup/sanitization, research moved-to-core + no-summary
  key guard, MCP + REST trace opt-in, search_and_extract gating, research over
  both surfaces)
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh --ci`
- Grounding + design-review workflow (pre-implementation) and adversarial diff
  review (post-implementation).
