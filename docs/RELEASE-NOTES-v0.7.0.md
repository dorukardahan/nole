# Nólë v0.7.0 release notes

Nólë v0.7.0 makes the **center trustworthy**: every number the router reports
about money and health is now labelled true, estimated, or unknown. Nólë stays a
dumb-but-honest search and extraction routing layer — it surfaces raw signals (a
provider's 429, a tripped breaker, a missing cost cap) and lets the agent decide
what to do. No new MCP
tools; this release adds fields to the existing `provider_status` /
`budget_status` envelopes and turns `/health` into a real readiness check.

## New for agents

- **Drift signal.** When a provider rejects a call as over-quota (HTTP 429) while
  Nólë's local free-tier counter still shows room, `budget_status` reports it
  (`has_drift`, `drift_signals[]`) and the affected provider carries a
  `drift_warning` in `provider_status`. This is the mechanism that makes
  "estimate" honest — Nólë admitting *"my counter disagreed with the provider."*
  It never debits, never reorders routes, never judges health. Signals persist
  across restarts and age out of output after 24h. Best-effort EARLY signal: once
  repeated 429s trip the circuit breaker, calls short-circuit (`ErrCircuitOpen`,
  not a 429) and drift correctly stops.
- **Circuit-breaker state in `provider_status`.** Breakered providers report
  `breaker_state` (`closed`/`open`/`half-open`), `breaker_consec_fails`, and
  `breaker_opened_at` — raw signals, no Nólë-computed recovery ETA or recommended
  fallback. A provider currently short-circuiting also reports
  `available: false` / `reason: circuit_open`, so the route walk and `/health`
  treat it as not-ready.
- **Honest per-provider quota metadata.** Each BYOK entry now carries a
  `metering_model`, and `budget_status` states up front (`estimate_note`) that
  `free_remaining` is Nólë's own issued-request estimate, not your provider
  dashboard. Per-provider notes spell out the real caveats: Brave's Feb-2026 move
  to a $5 monthly credit + metered billing and 1 req/sec cap; Tavily's per-credit
  search/extract cost (Nólë debits 1/call); Firecrawl's monthly-vs-one-time
  ambiguity — verify your dashboard.
- **Cost-cap clarity.** `budget_status` exposes `hard_cap_source`
  (`explicit`/`unset`); `nole doctor` now says loudly when `cost-capped` is set
  without `NOLE_HARD_CAP_CENTS` (premium blocked until you set it). Nólë never
  authorizes an unrequested default spend — it stays fail-closed.

## Changed (read this if you parse Nólë's output)

- **`/health` is now a real readiness check.** `200 {"status":"ready", ...}` iff
  at least one search-capable provider is available and allowed by the cost
  policy, else `503 {"status":"not_ready", "reason": ...}`. The body shape
  changed from `{"status":"ok"}` to
  `{status, timestamp, reason?, available_providers}`. Keyless DDGS keeps a
  zero-key deployment "ready". Readiness is orthogonal to budget — a hard-cap hit
  is a `/api/budget` concern, not a health one.

## Honest notes

- Free-tier quota numbers are unchanged (1000/month per BYOK provider). Verified
  against current provider pricing (June 2026): ~1000 is the honest fail-safe
  floor for all three today (Brave's flat free tier ended Feb 2026, so a higher
  number would risk a surprise bill). The honesty work is metadata + the drift
  signal, not new integers.
- Drift is a *local* observation. Nólë cannot read your provider dashboard; it
  reports when its own estimate disagreed with a provider's answer.

## Verified

- `go build ./...`, `go vet ./...`, `gofmt`
- `go test -race ./...` (incl. new tests: drift classify/record/no-debit/
  persist/union-merge/age-out/concurrency/router-ignores-drift; breaker
  state readers + nil-safe + race + circuit_open Available; honest quota
  metadata + estimate note; cost-cap source resolution + loud doctor; `/health`
  ready/503/body-shape)
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh --ci`
- Grounding + design-review workflow (pre-implementation, 8 agents) and
  adversarial diff review (post-implementation).
