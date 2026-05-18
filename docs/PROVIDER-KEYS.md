# Provider keys and cost safety

Nólë is BYOK-first: you use your own provider accounts and keys. It should never print key values, auth headers or raw provider payloads. It should only report whether a key is present.

Default policy is `free-first`. That means no hidden paid usage by default: a provider key by itself does not make a premium-capable provider eligible for live calls. Premium-capable providers can be used when the user explicitly chooses a policy that permits them, local cost controls are explicit, and routing evidence supports the choice.

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
export JINA_API_KEY="..."
export FIRECRAWL_API_KEY="..."
```

DDGS is keyless and does not need a key.

Optional cost policy controls:

```bash
# Default if unset: free-first.
export NOLE_COST_POLICY="free-first"     # free-first | cost-capped | quality-first

# Used by cost-capped. Without explicit estimates, premium-capable calls fail closed.
export NOLE_HARD_CAP_CENTS="0"
export NOLE_BRAVE_ESTIMATED_COST_CENTS=""
export NOLE_TAVILY_ESTIMATED_COST_CENTS=""
export NOLE_JINA_ESTIMATED_COST_CENTS=""
export NOLE_FIRECRAWL_ESTIMATED_COST_CENTS=""
```

Do not set `quality-first` or non-zero cost estimates unless you intentionally accept the provider-account cost risk. Nólë does not know your provider dashboard's exact real-time balance in v0.1; when `NOLE_QUOTA_LEDGER_PATH` is configured it persists only local quota counters and estimated spend across process restarts.

Optional local state controls:

```bash
# Enable a file-backed local quota/cost ledger. Leave unset, or use memory/off/none, for memory-only accounting.
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
- `JINA_API_KEY`
- `FIRECRAWL_API_KEY`

Do not commit real values.

Codex setup sources `~/.config/nole/.env` before launching `nole mcp`. Other clients may need a wrapper command such as `/bin/sh -lc 'set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec /absolute/path/to/nole mcp'`.

## Brave Search API

Use for: broad search, docs, news/freshness, pricing and fallback routes.

Setup:

1. Create a Brave Search API key in the Brave dashboard.
2. Choose a plan appropriate for your expected usage.
3. If the dashboard supports request caps or overage controls, set them before live use.
4. Export `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` locally.
5. Run `nole doctor` and confirm presence only.

Notes:

- Do not assume unlimited free usage.
- Route matrix changes involving Brave should be evidence-backed.

## Tavily

Use for: search, extract, semantic/people/fact-check/pricing tasks depending on evidence and policy.

Setup:

1. Create a Tavily API key in the provider dashboard.
2. Review free-tier and paid usage limits.
3. Disable overage or set limits if the account allows it.
4. Export `TAVILY_API_KEY` locally.
5. Run `nole doctor`.

Notes:

- Tavily can be premium-capable depending on account plan.
- Nólë should not prefer it merely because a key exists.

## Jina

Use for: search and page extraction/reader fallback.

Setup:

1. Create or locate the Jina key for the relevant service.
2. Review free quota and paid usage terms.
3. Export `JINA_API_KEY` locally.
4. Run `nole doctor`.

Notes:

- Jina is useful as an extraction fallback.
- Keep raw fetched content and provider payloads out of logs when they may contain private data.

## Firecrawl

Use for: search and extraction, especially code/social/docs scenarios when evidence supports it.

Setup:

1. Create a Firecrawl API key.
2. Review plan limits, rate limits and overage settings.
3. Export `FIRECRAWL_API_KEY` locally.
4. Run `nole doctor`.

Notes:

- Firecrawl can be premium-capable depending on account plan.
- Live extraction may consume quota; keep tests low-limit.

## DDGS

Use for: keyless fallback search.

Setup: none.

Notes:

- Keyless does not mean guaranteed availability or unlimited use.
- Treat DDGS as a useful fallback, not a hard SLA.

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
- `free-tier-BYOK`: a user-keyed provider with known local free quota remaining.
- `premium-capable`: a keyed provider that may incur paid usage depending on the user's account/plan.
- `unknown-cost`: cost cannot be safely classified; fail closed except under explicit `quality-first`.
- `disabled-no-key`: provider exists but no key is configured.

Policy modes:

- `free-first`: default; allows keyless/free-tier routes and blocks premium-capable routes. This is the no-hidden-paid-spend mode.
- `cost-capped`: allows premium-capable providers only if `NOLE_HARD_CAP_CENTS`, persisted local ledger spend when configured and explicit per-provider estimated cost env vars keep the call inside the local cap. Without an explicit local estimate, it fails closed with `unknown_cost_blocked`.
- `quality-first`: explicitly allows premium-capable providers when quality/evidence justifies it and the user accepts provider-account cost risk.

Important: this is a conservative local policy model, not a live provider billing oracle. Check provider dashboards for real balances, plan limits and overage settings before live use.
