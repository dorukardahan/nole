# Nólë v0.7.1 release notes

A focused **honest-quota data correction** on top of v0.7.0's trust pillar. v0.7.0
shipped the *mechanism* for honesty (drift signal, breaker state, estimate
labelling); v0.7.1 re-verifies the *numbers* against current (June 2026) provider
pricing and fixes a credit-vs-call unit mismatch that v0.7.0 missed. Data, a
one-line upgrade-path ledger clamp, and docs — no schema or signature change, and
no new MCP tools.

## Changed (read this if you rely on `free_remaining`)

- **Tavily and Firecrawl free floors: 1000 → 500.** Their free tiers grant 1000
  *credits*/month, but Nólë's ledger debits 1 per *call* — and an advanced Tavily
  search/extract and a Firecrawl search each cost **2 credits**. So the old
  `FreeQuota=1000` over-read remaining headroom up to 2×: the provider dashboard
  could reach zero while Nólë still reported room (the exact *dishonest-quota*
  failure the trust pillar exists to prevent). The floor is now
  `credits ÷ worst-case-credits-per-call` = `1000 ÷ 2 = 500`. Undercounting basic
  usage is the safe direction; the drift signal catches whatever slips past.
- **Brave: 1000 kept, metadata corrected.** Brave's $5/month credit meters a
  uniform `$0.005/query`, so 1 call = 1 query and 1000 stays the exact fail-safe
  floor. But the *notes* were wrong: the "legacy accounts keep 2000/month"
  grandfathering claim is removed (Brave eliminated the flat tier on 12 Feb 2026
  and published no migration policy), the rate cap is corrected from **1 req/sec
  to 50 req/sec** (1 req/sec was the eliminated legacy tier), and the notes now
  state the required public attribution and the overage behaviour — past the $5
  credit the card is billed unless you set a usage limit in the Brave dashboard
  (which Brave recommends). That overage path is the biggest surprise-bill vector
  across the three providers, and the dashboard limit is how you cap it.
- **Firecrawl "monthly vs one-time" hedge resolved.** Verified as 1000
  credits/month, reset monthly with no rollover. The prior "in flux" wording is
  gone.

## Fixed — existing ledgers correct themselves on first load

Lowering the seed alone would not have fixed an **existing** user: a persisted
current-month ledger entry sized for the old 1000 floor inherits its
`free_remaining` across the merge, so it would keep reporting up to ~1000
remaining until the next monthly rollover — the exact over-read this release
targets. `mergeLedgerEntries` now re-bases the loaded counter on calls already
consumed against the new floor (`new_floor − (old_quota − old_remaining)`,
clamped to ≥ 0) whenever the seeded floor dropped, and persists the result so the
on-disk ledger self-heals on first load. The same re-base fires across a
`NOLE_<PROVIDER>_PAID` toggle (disabling paid mode after the upgrade can't inherit
the stale counter). It only ever *lowers* the counter, is idempotent once disk
carries the new floor, and leaves v1 migrations and same-floor entries untouched.
(Caught by Codex review on PR #40 — two rounds.)

## What this does NOT change

- The north-star holds: Nólë counts only its *own* issued requests, uses a
  fail-safe *floor*, and never reads your live dashboard balance. Lowering floors
  is pure undercounting toward safety — no quota was *raised*.
- `metering_model` stays `credit-based` for all three (the per-call cost is
  variable in credits — which is precisely why the floor divides by worst-case
  credits/call). `estimate_only` stays true. No envelope field was added or removed.

## Provider re-verification (the full sweep)

Re-verified every provider against official sources via a grounding workflow with
an adversarial cross-check:

- **Brave** (brave.com/search/api, api-dashboard pricing): $5/mo auto-renewing
  credit ≈ 1000 Web Search queries @ $5/1K; 50 req/sec; card required, no spending
  cap; attribution required; legacy flat tier ended Feb 2026.
- **Tavily** (docs.tavily.com): 1000 credits/mo Researcher free (no card); basic
  search 1 / advanced 2 / extract 1–2 credits; ~100 RPM.
- **Firecrawl** (firecrawl.dev/pricing): 1000 credits/mo, monthly reset, no
  rollover, no card; scrape 1 / search 2 / Enhanced 5 credits; /scrape 10 rpm,
  /search 5 rpm.
- **DDGS** — no change needed. Nólë's DDGS provider is **pure-Go** (POSTs
  `html.duckduckgo.com/html/` directly and already handles HTTP 202 rate-limits),
  so the upstream `duckduckgo_search` → `ddgs` PyPI rename and 202/403 churn do not
  affect Nólë.
- **Scrapling** — no change needed. Nólë's subprocess script is written
  defensively (no removed `css_first`/`xpath_first`; `getattr` fallbacks for the
  fetch method; fails *closed* with an "upgrade scrapling" message if
  `follow_redirects=False` is rejected), so it is compatible with Scrapling
  v0.4.x. The new v0.4 `follow_redirects="safe"` default even aligns with Nólë's
  existing SSRF posture.

## Correction note

v0.7.0's release notes stated "free-tier quota numbers are unchanged (1000/month
per BYOK provider) … ~1000 is the honest fail-safe floor for all three." That was
right for Brave but missed the credit-vs-call gap for Tavily and Firecrawl, whose
1000 is a *credit* grant, not a *call* count. v0.7.1 is that correction.

## Verified

- `gofmt`, `go vet ./...`
- `go test -race ./...` (floor change propagates to the live-slice CLI tests;
  ledger-mechanics tests using inline seeds are correctly insulated; new
  `v071_quota_test.go` covers the upgrade clamp: partial-use re-base, heavy-use
  clamp-to-zero, no-use drop-to-floor, and idempotency)
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh --ci`
- Grounding + adversarial-verification workflow (web-verified provider docs, 7
  agents) and adversarial diff review (post-implementation).
