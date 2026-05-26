# Provider keys and cost safety

Nólë is BYOK-first: you use your own provider accounts and keys. It should never print key values, auth headers or raw provider payloads. It should only report whether a key is present.

Default policy is `free-first` and each supported BYOK provider is classified as `free-tier-BYOK` when its key is set. Nólë seeds a hardcoded monthly free quota per provider (currently 1000 calls/month each), tracks it in the local ledger and refills it at the start of each UTC calendar month. Premium-capable behavior is opt-in via `NOLE_<PROVIDER>_PAID=1`; in that mode the cost-capped or quality-first policies decide eligibility for paid calls.

## Provider cost/overage checklist

| Provider | Key variable(s) | Free-tier default | Paid opt-in | Cost/overage note |
| --- | --- | --- | --- | --- |
| Brave Search API | `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` | 1000 calls/month, monthly reset | `NOLE_BRAVE_PAID=1` | Brave's free tier runs on a subscription with credit card on file. Nólë caps usage at the local monthly quota, but any overage outside Nólë (concurrent process, ledger desync) will bill the CC. `nole doctor` surfaces this when the key is set. |
| Tavily | `TAVILY_API_KEY` | 1000 calls/month, monthly reset | `NOLE_TAVILY_PAID=1` | Free Researcher tier; no card required. Paid plans charge per credit; review the dashboard before flipping the opt-in. |
| Firecrawl | `FIRECRAWL_API_KEY` | 1000 calls/month, monthly reset | `NOLE_FIRECRAWL_PAID=1` | Free quota refill semantics shifted in early 2026; verify the dashboard balance matches Nólë's local counter before high-volume use. |
| DDGS | none | Keyless fallback search, no counter | n/a | Keyless does not mean guaranteed availability, SLA or unlimited use. |
| Scrapling | `NOLE_SCRAPLING_PYTHON` | Local keyless extraction fallback, no counter | n/a | Points to a Python runtime with `scrapling[fetchers]` installed. Nólë validates public URLs before calling it, but website terms and robots.txt remain the user's responsibility. |

The free-tier numbers above are conservative anchors verified 2026-05. They are encoded in `internal/core/byok_metadata.go` as `byokProviders` (accessed via `core.BYOKProviders()` and `core.LookupBYOK()`); bump them only with sanitized evidence (provider dashboard screenshot or doc URL).

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
export NOLE_SCRAPLING_PYTHON="/absolute/path/to/python3"  # optional local extract fallback
```

DDGS is keyless and does not need a key. Scrapling is also keyless, but it is local Python software rather than a remote account; set `NOLE_SCRAPLING_PYTHON` only when that Python environment can import `scrapling.fetchers`.

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

Do not commit real values.

Codex setup sources `~/.config/nole/.env` before launching `nole mcp`. Other clients may need a wrapper command such as `/bin/sh -lc 'set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec /absolute/path/to/nole mcp'`.

### Recommended wrapper script

For non-Codex clients, the cleanest pattern is a tiny env-sourcing wrapper at `~/.local/bin/nole-mcp` (`chmod 700`):

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

The wrapper keeps provider keys out of each per-client config file and ensures `nole mcp` always launches with the same env regardless of how the client is started.

## Brave Search API

Use for: broad search, docs, news/freshness, pricing and fallback routes.

Default classification: `free-tier-BYOK`, 1000 calls/month, refilled at the start of each UTC month.

> Note on the 1000 vs 2000 discrepancy: Brave's official Free Data plan advertises a 2,000 calls/month allowance with a 1 request/second rate cap. Nólë's local anchor is intentionally set at 1,000 to add a safety margin (overage on Brave is billed to the credit card on file). Bump `byokProviders.FreeQuota` in `internal/core/byok_metadata.go` only with sanitized evidence (provider dashboard screenshot or doc URL).

Setup:

1. Create a Brave Search API key in the Brave dashboard.
2. Choose the free subscription plan unless you already need a paid tier.
3. Export `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` locally.
4. Run `nole doctor` and confirm presence only.

Notes:

- Brave's free tier rides a subscription model with credit card on file. Nólë's monthly ledger caps usage at the local free quota, but any usage outside Nólë (concurrent process, ledger desync, dashboard test calls) will bill the CC. `nole doctor` surfaces a `brave_note:` line whenever the key is set.
- Set `NOLE_BRAVE_PAID=1` only when you intentionally want Nólë to treat Brave as premium-capable (e.g. you are on a paid plan and want cost-capped routing).
- Route matrix changes involving Brave should be evidence-backed.

## Tavily

Use for: search, extract, semantic/people/fact-check/pricing tasks depending on evidence and policy.

Default classification: `free-tier-BYOK`, 1000 calls/month (Researcher free tier), refilled at the start of each UTC month. No credit card on file.

Setup:

1. Create a Tavily API key in the provider dashboard.
2. Export `TAVILY_API_KEY` locally.
3. Run `nole doctor`.

Notes:

- Set `NOLE_TAVILY_PAID=1` only when on a paid Tavily plan and you want Nólë to treat the provider as premium-capable.
- Tavily's "advanced" search and extract endpoints consume more credits per call than basic search; the local counter treats every call as one unit.

## Firecrawl

Use for: search and extraction, especially code/social/docs scenarios when evidence supports it.

Default classification: `free-tier-BYOK`, 1000 calls/month, refilled at the start of each UTC month. No credit card on file.

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

## Scrapling

Use for: local keyless URL extraction fallback.

Setup:

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

- Nólë keeps Tavily and Firecrawl first in the default extract route because that order is backed by existing route evidence. Scrapling is appended as a local fallback until comparable local extraction evidence exists.
- The service validates public URLs before calling providers. Scrapling still performs a real HTTP request from the local machine, so respect site terms, robots.txt and rate limits.
- Do not point `NOLE_SCRAPLING_PYTHON` at a shell snippet. It must be an executable Python path; wrappers should exec Python directly and keep secrets out of command lines.

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

- `keyless-free`: no provider key required; DDGS search fallback is the current example.
- `free-tier-BYOK`: a user-keyed provider with a known local free quota tracked in the ledger. Default for keyed Brave / Tavily / Firecrawl.
- `premium-capable`: a keyed provider that may incur paid usage depending on the user's account/plan. Reached by setting `NOLE_<PROVIDER>_PAID=1`.
- `unknown-cost`: cost cannot be safely classified; fail closed except under explicit `quality-first`.
- `disabled-no-key`: provider exists but no key is configured.

Policy modes:

- `free-first`: default; allows keyless and free-tier-BYOK routes and blocks premium-capable routes. This is the no-hidden-paid-spend mode.
- `cost-capped`: allows premium-capable providers only if `NOLE_HARD_CAP_CENTS`, persisted local ledger spend when configured and explicit per-provider estimated cost env vars keep the call inside the local cap. Without an explicit local estimate, it fails closed with `unknown_cost_blocked`.
- `quality-first`: explicitly allows premium-capable providers when quality/evidence justifies it and the user accepts provider-account cost risk.

Quota refresh:

- Free-tier-BYOK entries carry a `refresh_window` and `period_start` in the ledger. With `refresh_window=monthly`, `FreeRemaining` is automatically refilled to the hardcoded `FreeQuota` (1000) at the start of each UTC calendar month, both on ledger reload and on the next `Record` call.
- The ledger uses schema version 2. v1 ledgers from prior nole versions are migrated forward on first load. Cost-class transitions (e.g. v1 BYOK keys that were premium-capable becoming free-tier-BYOK) use the seed's fresh `FreeRemaining` instead of the stale on-disk counter.

Important: this is a conservative local policy model, not a live provider billing oracle. Check provider dashboards for real balances, plan limits and overage settings before live use.

## Partial-key behavior

Nólë is designed as a strict enhancement of whatever AI tool consumes it. The MCP surface adapts to which keys are configured:

- **No keys at all:** `mcp__nole__search` is registered and routes via DDGS (keyless). `mcp__nole__extract` is **not** registered — the AI tool uses its own built-in HTTP fetch instead. `mcp__nole__provider_status` and `mcp__nole__budget_status` are always available.
- **Only `BRAVE_API_KEY`:** Search routes Brave-first with DDGS fallback. Extract is still not registered (Brave has no extract capability); the AI tool's built-in fetch handles URL content.
- **Only `TAVILY_API_KEY` or only `FIRECRAWL_API_KEY`:** Both `mcp__nole__search` and `mcp__nole__extract` are registered.
- **Any two or all three:** Full feature set with redundancy on the overlapping capability.

If you add a key mid-session, restart your AI tool (or its MCP connection) so the new tool surface is picked up.

`provider_status` returns a `setup_suggestions` array listing every missing key, what configuring it would unlock, where to sign up, and an `impact` rating (`high` / `medium` / `low`) so AI tools can decide what to surface. The first `search` response of an MCP session also carries a compact `setup_tip` summarizing the same information; subsequent searches in the same session omit it to avoid nagging.
