# Provider keys and cost safety

Nólë is BYOK-first: you use your own provider accounts and keys. It should never print key values, auth headers or raw provider payloads. It should only report whether a key is present.

Default policy is free-tier/BYOK-safe. That means no hidden paid usage by default, but it does not mean Nólë is forever free-only. Premium-capable providers can be used when the user explicitly chooses a policy that permits them and routing evidence supports the choice.

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

## Local env file pattern

For GUI clients or agent CLIs that do not inherit shell env:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

Example shape only; do not commit real values:

```bash
BRAVE_API_KEY=replace-with-local-value
TAVILY_API_KEY=replace-with-local-value
JINA_API_KEY=replace-with-local-value
FIRECRAWL_API_KEY=replace-with-local-value
```

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

## Future cost policy model

The product direction is:

- `free-first`: default; prefer keyless/free-tier/BYOK-safe routes that meet quality needs.
- `cost-capped`: fail closed when local budget or known free quota is exhausted.
- `quality-first`: allow premium-capable providers when quality/evidence justifies the cost and policy permits it.

Until cost policy is fully implemented, behave conservatively: do not run live calls that may create paid usage unless the user explicitly approves.
