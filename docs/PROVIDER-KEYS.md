# Provider keys and cost safety

Nólë is BYOK-first: you use your own provider accounts and keys. It should never print key values, auth headers or raw provider payloads. It should only report whether a key is present.

Default policy is `free-first` and each supported BYOK provider is classified as `free-tier-BYOK` when its key is set. Nólë seeds a hardcoded monthly free quota per provider (currently 1000 calls/month for Brave, 500 for Tavily, 250 for Firecrawl — the lower floors reflect those two providers' variable per-credit metering: the ledger debits 1 per call, but an advanced Tavily call costs 2 credits and a 20-result Firecrawl search costs 4), tracks it in the local ledger and refills it at the start of each UTC calendar month. Premium-capable behavior is opt-in via `NOLE_<PROVIDER>_PAID=1`; in that mode the cost-capped or quality-first policies decide eligibility for paid calls.

## Provider cost/overage checklist

| Provider | Key variable(s) | Free-tier default | Paid opt-in | Cost/overage note |
| --- | --- | --- | --- | --- |
| Brave Search API | `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` | 1000 calls/month, monthly reset | `NOLE_BRAVE_PAID=1` | Free tier is now a $5/month auto-renewing credit (~1000 Web Search queries at $0.005/query, 50 req/sec) with a credit card on file (overages billed unless you set a usage limit in the Brave dashboard, which Brave recommends); the old flat 2000+/month tier ended Feb 2026. Nólë caps usage at the local monthly quota, but any overage outside Nólë (concurrent process, ledger desync) bills the CC unless you set that dashboard limit. `nole doctor` surfaces this when the key is set. |
| Tavily | `TAVILY_API_KEY` | 500 calls/month, monthly reset | `NOLE_TAVILY_PAID=1` | Free Researcher tier = 1000 credits/month, no card required; Nólë seeds a 500-call floor because advanced search/extract cost 2 credits while the ledger debits 1 per call. Paid plans charge per credit; review the dashboard before flipping the opt-in. |
| Firecrawl | `FIRECRAWL_API_KEY` | 250 calls/month, monthly reset | `NOLE_FIRECRAWL_PAID=1` | Free plan = 1000 credits/month, reset monthly with no rollover, no card; Nólë seeds a 250-call floor because search is 2 credits per 10 results, so a 20-result search (Nólë permits up to 20) costs 4 credits while the ledger debits 1. Verify the dashboard balance matches Nólë's local counter before high-volume use. |
| DDGS | none | Keyless fallback search, no counter | n/a | Keyless does not mean guaranteed availability, SLA or unlimited use. |
| Wikipedia/MediaWiki | none | Keyless encyclopedic search, no counter | n/a | Reinforces `factcheck`/`people`/`academic` routing only (not a general fallback). Uses the official MediaWiki Action API with a descriptive `User-Agent` per Wikimedia policy. Keyless does not mean guaranteed availability or unlimited use. |
| arXiv | none | Keyless academic search, no counter | n/a | Reinforces the `academic` route only (tried before the DDGS backstop; not a general fallback). Uses the keyless arXiv Atom query API (`https://export.arxiv.org/api/query`) with a descriptive `User-Agent`. Keyless does not mean guaranteed availability or unlimited use. |
| Scrapling | `NOLE_SCRAPLING_PYTHON` | Local keyless extraction fallback, no counter | n/a | Prefer `nole setup --local-extract`, which creates an isolated venv and writes this variable locally. Nólë validates public URLs before calling it, but website terms and robots.txt remain the user's responsibility. |
| httpfetch | none | Keyless pure-Go extraction backstop, no counter | n/a | Always available, no setup. Last-resort `extract` fallback (after Scrapling and the keyed remotes). Pure stdlib HTTP fetch + HTML-to-text; runs **no JavaScript**, so it is weaker than Scrapling/Firecrawl on SPA/JS-rendered pages. SSRF-preflighted on every redirect hop. Keyless does not mean guaranteed availability or unlimited use. |

The free-tier numbers above are conservative anchors verified 2026-06 against each provider's published pricing. Tavily and Firecrawl meter in variable credits while the ledger debits 1 per call, so each floor is credits ÷ the priciest call Nólë can issue: Tavily 1000 ÷ 2 (advanced search/extract) = 500; Firecrawl 1000 ÷ 4 (a 20-result search at 2 credits per 10 results) = 250. This avoids over-reading remaining headroom; undercounting is the safe direction and the drift signal catches the rest. They are encoded in `internal/core/byok_metadata.go` as `byokProviders` (accessed via `core.BYOKProviders()` and `core.LookupBYOK()`); bump them only with sanitized evidence (provider dashboard screenshot or doc URL).

Use `nole doctor`, `nole providers --json` and MCP `provider_status`/`budget_status` to inspect status safely. These surfaces should report presence/status and local policy decisions, never key values.

## General rules

- Create provider keys in each provider's dashboard.
- Prefer free-tier plans with overage disabled or hard limits where available.
- Store keys in local environment variables or a local-only env file.
- Do not paste keys into chat, PRs, issues or docs.
- Do not commit `.env` files.
- Rotate a key if it was exposed.

## Environment variables

Nólë currently reads:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export FIRECRAWL_API_KEY="..."
export NOLE_SCRAPLING_PYTHON="/absolute/path/to/python3"  # written by `nole setup --local-extract`
```

DDGS is keyless and does not need a key. Scrapling is also keyless, but it is local Python software rather than a remote account. Prefer `nole setup --local-extract`; it sets `NOLE_SCRAPLING_PYTHON` only after the created Python environment can import `scrapling.fetchers`.

Per-provider paid opt-in (default: free-tier-BYOK). Use only when the user has a paid plan and wants Nólë to treat the provider as premium-capable so cost-capped or quality-first policies apply:

```bash
export NOLE_BRAVE_PAID="1"
export NOLE_TAVILY_PAID="1"
export NOLE_FIRECRAWL_PAID="1"
```

Optional cost policy controls (only relevant once a provider is in paid mode):

```bash
# Default if unset: free-first.
export NOLE_COST_POLICY="free-first"     # free-first | cost-capped | quality-first

# Used by cost-capped. Without explicit estimates, premium-capable calls fail closed.
export NOLE_HARD_CAP_CENTS="0"
export NOLE_BRAVE_ESTIMATED_COST_CENTS=""
export NOLE_TAVILY_ESTIMATED_COST_CENTS=""
export NOLE_FIRECRAWL_ESTIMATED_COST_CENTS=""
```

Do not set `quality-first` or non-zero cost estimates unless you intentionally accept the provider-account cost risk. Nólë does not know your provider dashboard's exact real-time balance in v0.1; when `NOLE_QUOTA_LEDGER_PATH` is configured it persists only local quota counters and estimated spend across process restarts.

Optional local state controls:

```bash
# File-backed local quota/cost ledger. Defaults to $XDG_STATE_HOME/nole/quota-ledger.json
# (or ~/.local/state/nole/quota-ledger.json). Override only if you need a
# different path, or set to memory/off/none to opt out of persistence —
# memory mode resets the free-tier counter on every restart.
export NOLE_QUOTA_LEDGER_PATH="$HOME/.local/state/nole/quota-ledger.json"

# Enable in-process TTL cache for normalized search/extract responses, useful for long-running MCP sessions.
export NOLE_CACHE_TTL="5m"                  # Go duration syntax
export NOLE_CACHE_TTL_SECONDS="300"         # alternative integer seconds form
```

The ledger file stores provider names, cost classes, local free-quota counters and local estimated spend. It must not store provider keys, auth headers or raw provider payloads. If the ledger is corrupt, Nólë backs it up and fails closed for paid/quota-tracked providers while still allowing keyless-free providers. Cache entries are in-memory only and expire after the configured TTL; cache hit/miss status is visible in `route_trace` and compact `routing_insight`.

## Local env file pattern

For GUI clients or agent CLIs that do not inherit shell env:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

Variable names to set locally when you have the matching provider accounts:

- `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY`
- `TAVILY_API_KEY`
- `FIRECRAWL_API_KEY`
- `NOLE_SCRAPLING_PYTHON` is written automatically by `nole setup --local-extract`; manual edits are optional.

Do not commit real values. Nólë commands load `~/.config/nole/.env` automatically and do not override variables that are already present in the process environment.

Codex setup sources `~/.config/nole/.env` before launching `nole mcp`. Other clients may need a wrapper command such as `/bin/sh -lc 'set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec /absolute/path/to/nole mcp'`.

### Recommended wrapper script

For non-Codex clients, the cleanest pattern is to let Nólë create a tiny env-sourcing wrapper at `~/.local/bin/nole-mcp`. Prefer this for Hermes v0.15+ because Hermes filters stdio MCP subprocess environments by default:

```bash
nole setup --local-extract
```

The manual wrapper template is:

```sh
#!/bin/sh
set -a
[ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"
set +a
if [ -n "${NOLE_BIN:-}" ] && [ -x "$NOLE_BIN" ]; then exec "$NOLE_BIN" mcp; fi
if command -v nole >/dev/null 2>&1; then exec nole mcp; fi
if [ -x "$HOME/.local/bin/nole" ]; then exec "$HOME/.local/bin/nole" mcp; fi
echo "nole binary not found. Install nole to PATH or set NOLE_BIN." >&2
exit 127
```

The wrapper is local-only; do not commit it. Then register the MCP server with the wrapper as the command and empty args:

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole-mcp",
      "args": []
    }
  }
}
```

Nólë's setup writers accept `--mcp-wrapper /absolute/path/to/nole-mcp` to emit this shape directly for the supported clients:

```bash
nole setup --opencode --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --kimi     --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --cursor   --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --claude   --mcp-wrapper /absolute/path/to/nole-mcp   # prints the matching claude mcp add command
nole setup --codex    --mcp-wrapper /absolute/path/to/nole-mcp   # uses a wrapper-direct launch line
```

When `--local-extract` is passed without `--mcp-wrapper`, setup writes and registers `~/.local/bin/nole-mcp` automatically:

```bash
nole setup --codex --local-extract
nole setup --hermes --local-extract
```

The wrapper keeps provider keys out of each per-client config file and ensures `nole mcp` always launches with the same env regardless of how the client is started.

## Brave Search API

Use for: broad search and search fallback routes; current route evidence keeps it first for `general` and near the front for docs, people, pricing and semantic discovery. For `news` and `factcheck` tasks, the Brave adapter uses the dedicated News Search endpoint while preserving the conservative `freshness=pm` window.

Default classification: `free-tier-BYOK`, 1000 calls/month, refilled at the start of each UTC month.

> Note on the $5 credit model (verified 2026-06): Brave eliminated its flat free tier (formerly 2,000, briefly 5,000 queries/month) on 12 Feb 2026. New accounts get a $5/month auto-renewing credit covering ~1,000 Web Search queries at $5 per 1,000 requests ($0.005/query), with a 50 req/sec rate cap on the Search plan. A credit card is required; by default overages bill the CC, but Brave lets you set a monthly usage limit in the dashboard ("My subscriptions" tab) to cap spend and recommends doing so. Legacy-account grandfathering is unconfirmed (Brave published no migration policy). Brave also requires public attribution of the Brave Search API on your site to grant the credit. Nólë's local anchor of 1,000 maps 1:1 to the $5 credit (1 call = 1 query = $0.005), so it stays the right fail-safe floor. Bump `byokProviders.FreeQuota` in `internal/core/byok_metadata.go` only with sanitized evidence (provider dashboard screenshot or doc URL).

Setup:

1. Create a Brave Search API key in the Brave dashboard.
2. Choose the free subscription plan unless you already need a paid tier.
3. Export `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` locally.
4. Run `nole doctor` and confirm presence only.

Notes:

- Brave's free tier rides a subscription model with credit card on file. Nólë's monthly ledger caps usage at the local free quota, but any usage outside Nólë (concurrent process, ledger desync, dashboard test calls) will bill the CC. `nole doctor` surfaces a `brave_note:` line whenever the key is set.
- Set `NOLE_BRAVE_PAID=1` only when you intentionally want Nólë to treat Brave as premium-capable (e.g. you are on a paid plan and want cost-capped routing).
- Brave's Search-plan calls used by Nólë are the Web Search endpoint for general/non-recency tasks and the News Search endpoint for `news`/`factcheck`. Brave News allows a higher result cap (up to 50) than Web Search's 20; Nólë clamps per endpoint. Brave Answers/chat-completions are a separate plan and are not used by Nólë's free-first Search adapter.
- Route matrix changes involving Brave should remain evidence-backed.

## Tavily

Use for: search, extract, academic/code/semantic tasks and fallback routes depending on evidence and policy.

Default classification: `free-tier-BYOK`, 500 calls/month, refilled at the start of each UTC month. No credit card on file. The free Researcher tier grants 1000 credits/month; Nólë seeds a 500-call floor because an advanced search or extract costs 2 credits while the ledger debits 1 per call.

Setup:

1. Create a Tavily API key in the provider dashboard.
2. Export `TAVILY_API_KEY` locally.
3. Run `nole doctor`.

Notes:

- Set `NOLE_TAVILY_PAID=1` only when on a paid Tavily plan and you want Nólë to treat the provider as premium-capable.
- Tavily's "advanced" search and extract endpoints consume 2 credits per call vs 1 for basic search; the local counter debits 1 per call, so Nólë's 500-call floor (1000 credits ÷ 2 worst-case) keeps the local count from over-reading the dashboard. Heavy advanced use can still hit the dashboard limit before the local counter — verify your dashboard.

## Firecrawl

Use for: search and extraction, especially docs/news/fact-check/people/pricing/research/social scenarios when evidence supports it.

Default classification: `free-tier-BYOK`, 250 calls/month, refilled at the start of each UTC month. No credit card on file. The free plan grants 1000 credits/month (reset monthly, no rollover); Nólë seeds a 250-call floor because search is 2 credits per 10 results, so a 20-result search (Service permits up to maxSearchLimit=20) costs 4 credits while the ledger debits 1 per call (scrape is 1 credit; the 5-credit Enhanced Mode is never used by Nólë).

Setup:

1. Create a Firecrawl API key.
2. Export `FIRECRAWL_API_KEY` locally.
3. Run `nole doctor`.

Notes:

- Firecrawl's free quota semantics changed in early 2026; verify the dashboard balance matches Nólë's local counter before high-volume use, and bump the hardcoded default if the provider raises it.
- Set `NOLE_FIRECRAWL_PAID=1` to treat Firecrawl as premium-capable when on a paid plan.
- Live extraction may consume the local counter quickly; keep dry-run experiments small.

## DDGS

Use for: keyless fallback search.

Setup: none.

Notes:

- Keyless does not mean guaranteed availability or unlimited use.
- Treat DDGS as a useful fallback, not a hard SLA.

## Wikipedia/MediaWiki

Use for: keyless encyclopedic search reinforcing `factcheck`, `people`, and
`academic` routing (tried before the DDGS backstop; not a general fallback).

Setup: none. No key, no account.

Notes:

- Backed by the official MediaWiki Action API (`https://en.wikipedia.org/w/api.php`,
  `list=search`). Nólë sends a descriptive `User-Agent` identifying the project,
  per Wikimedia's User-Agent policy.
- English Wikipedia only; results (including disambiguation/list pages) are passed
  through verbatim for your agent to weigh — Nólë never judges result quality.
- A result's `published_at` is the article's **last-edit timestamp**, not an
  original publication date (that is what MediaWiki exposes). On the `factcheck`
  route the service applies a recency tie-break, so a recently-edited article may
  sort above an older one; the timestamp is passed through verbatim and `score`
  stays unset (Nólë never computes relevance).
- Keyless does not mean guaranteed availability or unlimited use; it honors the
  API's `maxlag` backpressure and falls through to DDGS on error. Because it is
  routed before the DDGS fallback, it carries a circuit breaker, so a persistently
  slow/down upstream short-circuits fast in a long-lived `serve`/MCP process
  rather than stalling the route on every request.

## arXiv

Use for: keyless academic search reinforcing the `academic` route (tried before
the DDGS backstop; not a general fallback).

Setup: none. No key, no account.

Notes:

- Backed by the keyless arXiv Atom query API (`https://export.arxiv.org/api/query`,
  `search_query=all:<query>`). Nólë sends a descriptive `User-Agent` identifying
  the project; arXiv does not require one, but it is good-citizen practice.
- Primary-source scholarly **preprints** (CS/physics/math/stat/econ/q-bio/q-fin).
  Results are passed through in arXiv's native relevance order; `score` stays unset
  (arXiv exposes no numeric relevance score and Nólë never fabricates one), and a
  result's `published_at` is the paper's first-version submit time, verbatim.
- The agent's query is passed through verbatim (Nólë does not parse or rewrite
  arXiv field operators). A query arXiv rejects comes back as an error entry that
  is skipped — an honest empty fall-through to Wikipedia/DDGS, never an error.
- arXiv's Terms of Use ask for at most one request every three seconds on a single
  connection. Nólë issues exactly one request per search and disables retries for
  arXiv specifically, so it never fires a rapid second request; a transient failure
  simply falls through. Because it is routed before the DDGS fallback it carries a
  circuit breaker (a persistently slow/down upstream, or sustained edge rate-limit,
  short-circuits fast rather than stalling the route).
- Keyless does not mean guaranteed availability or unlimited use; respect arXiv's
  Terms of Use and per-paper licenses.

## Scrapling

Use for: local keyless URL extraction when configured, with remote extract providers as fallback.

Preferred setup:

```bash
nole setup --local-extract
nole doctor --mcp
```

This command:

1. Creates or reuses `~/.local/share/nole/scrapling-venv`.
2. Installs `scrapling[fetchers]` into that isolated Python environment when needed.
3. Writes `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env`.
4. Writes `~/.local/bin/nole-mcp`, an env-sourcing MCP wrapper for GUI/service clients.
5. Lets `nole doctor --mcp` expose the MCP `extract` tool even when no Tavily or Firecrawl key is set.

Manual setup is still supported:

1. Create or choose a local Python 3.10+ environment.
2. Install the fetcher extras:

   ```bash
   pip install "scrapling[fetchers]"
   ```

3. Point Nólë at that Python executable:

   ```bash
   export NOLE_SCRAPLING_PYTHON="/absolute/path/to/python3"
   ```

4. Run `nole doctor --mcp` and confirm the MCP `extract` tool appears when no BYOK extract keys are set.

Notes:

- Current route evidence puts configured local Scrapling first for `extract`, then Firecrawl and Tavily. If Scrapling is not configured, service-level status checks skip it and continue to the remote providers.
- Nólë does not vendor, copy or redistribute Scrapling code. It optionally installs the user-side Python package into a local venv. Scrapling's PyPI metadata lists BSD-3-Clause licensing; still keep attribution and upstream license notices intact if packaging changes in the future.
- The service validates public URLs before calling providers. Scrapling still performs a real HTTP request from the local machine, so respect site terms, robots.txt and rate limits.
- Do not point `NOLE_SCRAPLING_PYTHON` at a shell snippet. It must be an executable Python path; wrappers should exec Python directly and keep secrets out of command lines.

## httpfetch

Use for: keyless, zero-setup URL extraction — the last-resort `extract` backstop.

- **No key, no runtime, no setup.** `httpfetch` is a pure-Go provider built on the
  standard library (`net/http` + a regexp/`html.UnescapeString` HTML-to-text pass).
  It is always registered, so `extract` / `search_and_extract` work out of the box.
- **Routing:** it is the LAST entry on the `extract` route
  (`scrapling -> firecrawl -> tavily -> httpfetch`), reached only when a configured
  local Scrapling and the keyed remotes are unavailable/blocked/exhausted. It is the
  extract-side analogue of DDGS on the search routes.
- **Honest limits:** it runs **no JavaScript**, so it is weaker than Scrapling and
  Firecrawl on SPA / client-rendered pages — it returns whatever static HTML the
  server sends, stripped to readable text. Configure Scrapling, Tavily, or Firecrawl
  for higher-fidelity / JS-rendered extraction.
- **Safety:** every redirect hop is re-validated by the local SSRF preflight (a
  public URL that 30x-redirects to a private / cloud-metadata host is rejected before
  it is fetched); the resolved IP is re-validated again at DIAL time (closing the
  DNS-rebinding window between preflight and connect); the response body is
  size-capped; non-text content types are refused; errors carry only HTTP status +
  byte-size metadata, never the URL/body. Because the dial-time guard must see the
  real target IP, httpfetch connects **directly and ignores `HTTP(S)_PROXY`** — use
  a keyed provider or local Scrapling if your outbound traffic must traverse a proxy.
- Keyless does not mean guaranteed availability or unlimited use; respect site terms,
  robots.txt and rate limits.

## Checking status safely

```bash
nole doctor
nole providers --json
nole bench --json
```

Safe output examples should say only `set`, `not set`, `available`, `unavailable`, `unknown quota` or similar. They must not include secret values.

## Live benchmark caution

Only run live provider calls intentionally:

```bash
nole bench --live --max-live-cases 3 --json
```

Before sharing benchmark output, sanitize:

- private queries;
- URLs that reveal private data;
- raw response bodies;
- headers;
- any token-like strings.

## Cost policy model

Nólë exposes cost policy/status in `nole providers --json`, `nole doctor`, MCP `provider_status`, MCP `budget_status`, runtime `route_trace` and compact `routing_insight` where relevant.

Cost classes:

- `keyless-free`: no provider key required; the DDGS search fallback, the Wikipedia and arXiv keyless search providers, the keyless httpfetch extraction backstop, and the optional local Scrapling extraction fallback are current examples.
- `free-tier-BYOK`: a user-keyed provider with a known local free quota tracked in the ledger. Default for keyed Brave / Tavily / Firecrawl.
- `premium-capable`: a keyed provider that may incur paid usage depending on the user's account/plan. Reached by setting `NOLE_<PROVIDER>_PAID=1`.
- `unknown-cost`: cost cannot be safely classified; fail closed except under explicit `quality-first`.
- `disabled-no-key`: provider exists but no key is configured.

Policy modes:

- `free-first`: default; allows keyless and free-tier-BYOK routes and blocks premium-capable routes. This is the no-hidden-paid-spend mode.
- `cost-capped`: allows premium-capable providers only if `NOLE_HARD_CAP_CENTS`, persisted local ledger spend when configured and explicit per-provider estimated cost env vars keep the call inside the local cap. Without an explicit local estimate, it fails closed with `unknown_cost_blocked`.
- `quality-first`: explicitly allows premium-capable providers when quality/evidence justifies it and the user accepts provider-account cost risk.

Quota refresh:

- Free-tier-BYOK entries carry a `refresh_window` and `period_start` in the ledger. With `refresh_window=monthly`, `FreeRemaining` is automatically refilled to the hardcoded per-provider `FreeQuota` (1000 for Brave, 500 for Tavily/Firecrawl) at the start of each UTC calendar month, both on ledger reload and on the next `Record` call.
- The ledger uses schema version 2. v1 ledgers from prior nole versions are migrated forward on first load. Cost-class transitions (e.g. v1 BYOK keys that were premium-capable becoming free-tier-BYOK) use the seed's fresh `FreeRemaining` instead of the stale on-disk counter.

Important: this is a conservative local policy model, not a live provider billing oracle. Check provider dashboards for real balances, plan limits and overage settings before live use.

## Partial-key behavior

Nólë is designed as a strict enhancement of whatever AI tool consumes it. Since
v1.3.0 the keyless `httpfetch` backstop is always registered, so `mcp__nole__extract`
and `mcp__nole__search_and_extract` are advertised **out of the box** (zero keys,
zero setup). Configuring a higher-fidelity provider upgrades the extract QUALITY
(JS rendering, cleaner content) but is no longer required for the tools to exist.
The MCP surface adapts to which keys/runtimes are configured:

- **No keys and no local Scrapling runtime:** `mcp__nole__search` is registered and routes via DDGS (keyless). `mcp__nole__extract` / `mcp__nole__search_and_extract` are registered too, backed by the keyless `httpfetch` backstop — a best-effort, no-JavaScript HTML-to-text fetch (weaker than Scrapling/Firecrawl on SPA pages). `mcp__nole__provider_status` and `mcp__nole__budget_status` are always available.
- **No keys plus `nole setup --local-extract`:** Search routes via DDGS; extract prefers the local Scrapling runtime (JS-capable), with `httpfetch` as the final keyless fallback.
- **Only `BRAVE_API_KEY`:** Search routes Brave-first with DDGS fallback. Extract is backed by Scrapling if configured, otherwise the keyless `httpfetch` backstop.
- **Only `TAVILY_API_KEY` or only `FIRECRAWL_API_KEY`:** Extract prefers the keyed remote (Scrapling first if also configured), with `httpfetch` as the final fallback.
- **Any two or all three:** Full feature set with redundancy on the overlapping capability.

If you add a key or run `nole setup --local-extract` mid-session, restart your AI tool (or its MCP connection) so the upgraded extract route is picked up.

`provider_status` returns a `setup_suggestions` array listing every missing key, what configuring it would unlock, where to sign up, and an `impact` rating (`high` / `medium` / `low`) so AI tools can decide what to surface. The first `search` response of an MCP session also carries a compact `setup_tip` summarizing the same information; subsequent searches in the same session omit it to avoid nagging.
