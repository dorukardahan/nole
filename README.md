# Nólë

Free web search router for AI agents and coding CLI tools.

Nólë gives Claude Code, Codex, OpenClaw, Hermes, OpenCode and other AI tools a local search/extract layer backed by multiple free or BYOK providers. It is not a hosted SaaS and it is not a replacement for your agent. Keep your existing agent, run Nólë on your own machine or VPS, and make that agent's web search better.

Core idea: use your own keys, keep control of cost, route by task fit and evidence, and return enough routing context for the agent to explain what happened without clutter.

## What Nólë is

Nólë is an agent web-search layer:

- A free/local web search router for AI agents and coding CLI tools.
- A BYOK provider router for search and page extraction.
- A task and multi-intent routing substrate for docs, news, code, academic, pricing, fact-checking, social/community and research queries.
- A benchmark/evidence-informed fallback layer for agents that need reliable internet context.
- A small single-binary tool that can be used by humans, shell scripts, MCP clients and agent CLIs.

Nólë is not primarily an MCP server. MCP is one important entrypoint; the product is the local routing layer that MCP, CLI commands and future integrations can call.

Nólë is not:

- A hosted search SaaS or cloud proxy.
- An agent replacement or a research assistant that takes over the workflow.
- A Perplexity clone.
- A provider marketplace.
- A promise of unlimited free search forever.

## Why agents use it

Agents and coding CLIs often need web search, docs lookup, URL extraction and source discovery. Nólë lets them delegate that internet layer to a local router that can:

1. Select a route for the task or intent.
2. Prefer free-tier/keyless/BYOK-safe providers by default.
3. Fall back safely when a provider is unavailable, out of quota or returns empty results.
4. Keep provider errors sanitized and avoid leaking secrets.
5. Expose `route_trace` for debugging and a compact routing insight for user-facing output.

Current routing is task-based with an LLM-free multi-intent planner: `nole classify` explains detected intents and `nole route-plan` shows provider routes before any provider call.

## Supported and priority agents

Priority v0.1 agent targets:

| Client | Status in this repo | Notes |
| --- | --- | --- |
| Claude Code | Verified on macOS via `claude mcp list/get` (M11); see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --claude` prints the official `claude mcp add` command, including wrapper mode when requested. |
| Codex CLI | Verified on macOS via `codex mcp list/get` (M11); see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --codex --local-extract` inlines `~/.config/nole/.env` sourcing in TOML output and prepares local Scrapling; `--mcp-wrapper` emits wrapper-direct config. |
| OpenCode | Verified on macOS via `opencode mcp list` (M11); see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --opencode` writes OpenCode's native `{type, command, enabled, environment}` schema. |
| Kimi | Verified on macOS via `kimi mcp list` and `kimi mcp test` (M11); see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --kimi` writes the same shape that `kimi mcp add` produces. |
| OpenClaw | Verified on OpenClaw 2026.5.18 via the Gateway/agent MCP path; compatibility re-checked on OpenClaw 2026.5.27 with the wrapper-backed MCP registry; see `docs/CLIENTS/LIVE-VERIFICATION.md` | Run `nole setup --local-extract`, then configure OpenClaw with `openclaw mcp set` and the generated env-sourcing wrapper. |
| Hermes Agent | Verified on Hermes Agent v0.14.0 via disposable profile MCP path and chat-agent dispatch; v2026.5.28 / v0.15.0 source compatibility reviewed; see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --hermes --local-extract` writes `~/.hermes/config.yaml`, prepares local Scrapling, uses the env-sourcing wrapper and preserves unrelated config. |
| Cursor | Verified on macOS Cursor 3.4.20 via GUI MCP path and chat-agent dispatch; see `docs/CLIENTS/LIVE-VERIFICATION.md` | `nole setup --cursor --local-extract` preserves unrelated MCP servers and emits wrapper-direct config through the generated wrapper. |
| Gemini CLI | Repo-tested: the `nole setup --gemini` writer and config merge are covered by repo tests; the real Gemini CLI was not launched here, so live tool visibility is unobserved. See `docs/CLIENTS/gemini.md` | `nole setup --gemini` writes `~/.gemini/settings.json` with an object-keyed `mcpServers` entry, merging only the `nole` server and preserving sibling servers and unknown root keys. |
| Grok CLI | Repo-tested: the `nole setup --grok` writer and array upsert-by-`id` are covered by repo tests; the real Grok CLI was not launched here, so live tool visibility is unobserved. See `docs/CLIENTS/grok.md` | `nole setup --grok` writes `~/.grok/user-settings.json` with an `mcp.servers` array, upserting the `id == "nole"` entry in place (or appending) and preserving other servers and unknown root keys. |

A client is only called verified after config path, tool visibility and `doctor --mcp` behavior are checked without printing credentials.

See `docs/CLIENTS/README.md` for the client support matrix, `docs/AGENT-INSTALL.md` for an agent-readable install/handoff checklist, `docs/CLIENTS/LIVE-VERIFICATION.md` for M11 live-client evidence and `docs/INTEGRATION-VERIFICATION.md` for the offline/CI integration evidence record.

## Providers

Nólë currently supports these provider adapters:

- Brave Search API: search, BYOK/free-tier capable.
- Tavily: search + extract, BYOK/free-tier/premium-capable depending on your account.
- Firecrawl: search + extract, BYOK/free-tier/premium-capable depending on your account.
- DDGS: keyless search fallback.
- Scrapling: optional local Python URL extraction fallback. `nole setup --local-extract` creates an isolated venv, installs `scrapling[fetchers]` there and writes `NOLE_SCRAPLING_PYTHON` locally. Nólë does not vendor or redistribute Scrapling.

Nólë reads provider credentials from environment variables. It should only report whether a key is present; it must never print key values, auth headers or raw provider payloads.

See `docs/PROVIDER-KEYS.md` for provider-by-provider setup and overage cautions.

## Quick start

Prerequisites:

- Go 1.25+ for building from source (matches the `go 1.25.10` directive in `go.mod`).
- Optional provider keys for Brave, Tavily and Firecrawl.
- Optional Python 3.10+ runtime for `nole setup --local-extract`, which prepares the local Scrapling extraction fallback.
- No provider key is required for the deterministic benchmark, DDGS keyless fallback or configured local Scrapling fallback.

Build and run locally:

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

Try the CLI:
```bash
./nole providers --json
./nole bench --json
./nole classify "OpenAI API docs pricing and latest changelog" --json
./nole route-plan "OpenAI API docs pricing and latest changelog" --json
./nole search "Go net/http Client Timeout documentation" --task docs --json
./nole extract "https://go.dev/doc/" --json
```

Search, extract, classify and route-plan JSON responses include a short `routing_insight` by default; search, extract and route-plan keep detailed `route_trace` for debugging where available. Human search/extract output prints the same one-line insight before results. Use `--insight off` to omit the user-facing insight, or `--insight verbose` to print the compact line plus route trace lines in human output. The insight is deterministic and sanitized; it should not contain API keys, auth headers, raw provider payloads or private URLs.

Install the binary somewhere on PATH, or keep the absolute path for MCP configs:

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole doctor
```

If the agent/client process does not inherit PATH, use `/absolute/path/to/nole` in the MCP config.

Optional, but recommended for agent installs that should expose URL extraction without provider keys:

```bash
nole setup --local-extract
nole doctor --mcp
```

This creates `~/.local/share/nole/scrapling-venv`, installs Scrapling into that isolated Python environment, writes `~/.config/nole/.env`, and generates an env-sourcing MCP wrapper at `~/.local/bin/nole-mcp`.

Configure an agent when the setup writer is available:

```bash
nole setup --claude
nole setup --codex
nole setup --hermes
nole setup --opencode
nole setup --codex --local-extract
nole setup --hermes --local-extract
# see `nole setup --help` for the full client list (cursor, kimi, windsurf, gemini, grok, etc.)
```

For unverified or generic clients, use the MCP command template:

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole",
      "args": ["mcp"]
    }
  }
}
```

## Provider keys and cost control

Default stance: `free-first`. Nólë treats each supported BYOK provider as `free-tier-BYOK` by default and tracks a hardcoded monthly free quota locally (currently 1000 calls/month for Brave, Tavily and Firecrawl, reset at the start of each UTC calendar month). DDGS and a configured local Scrapling runtime are keyless-free. No hidden paid usage is created without an explicit opt-in.

A key by itself is enough to start using a provider under the default policy; you only have to flip `NOLE_<PROVIDER>_PAID=1` when you want Nólë to treat that provider as premium-capable (e.g. you are on a paid plan and the cost-capped or quality-first policy should apply). See `docs/PROVIDER-KEYS.md` for per-provider free-tier sourcing and the Brave subscription/CC caveat.

Cost status classes exposed in `provider_status`, `budget_status`, `route_trace` and JSON CLI/MCP surfaces are:

- `keyless-free` — no key required, currently used for DDGS search fallback and optional local Scrapling extraction fallback.
- `free-tier-BYOK` — user-keyed provider running against the local free-tier quota. Default for keyed Brave / Tavily / Firecrawl.
- `premium-capable` — keyed provider that may incur paid usage depending on account/plan. Reached by setting `NOLE_<PROVIDER>_PAID=1`.
- `unknown-cost` — fail-closed unless an explicit quality-first policy is selected.
- `disabled-no-key` — provider is present but no key is configured.

Cost policy modes:

- `free-first` (default): allow keyless and free-tier-BYOK routes; block premium-capable providers so there is no hidden paid spend.
- `cost-capped`: allow premium-capable providers only when a local hard cap, persisted ledger state when configured and explicit per-provider estimated cost keep the call inside the cap.
- `quality-first`: explicitly allow premium-capable providers when the user accepts provider-account cost risk for quality/task fit.

Environment variables:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export FIRECRAWL_API_KEY="..."
export NOLE_SCRAPLING_PYTHON="/absolute/path/to/python3"  # written by `nole setup --local-extract`

# Opt into paid mode for a specific provider (default: free-tier-BYOK).
# Use only when you actively want Nólë to bill the provider account.
export NOLE_BRAVE_PAID="1"
export NOLE_TAVILY_PAID="1"
export NOLE_FIRECRAWL_PAID="1"

# Optional policy controls; omit for no-hidden-paid-spend default.
export NOLE_COST_POLICY="free-first"        # free-first | cost-capped | quality-first
export NOLE_HARD_CAP_CENTS="0"              # used by cost-capped
export NOLE_TAVILY_ESTIMATED_COST_CENTS=""  # set explicitly before cost-capped live use

# Optional local state controls. The ledger is file-backed by default at
# $XDG_STATE_HOME/nole/quota-ledger.json (or ~/.local/state/nole/quota-ledger.json
# when XDG_STATE_HOME is unset); set NOLE_QUOTA_LEDGER_PATH only if you want a
# different location, or "memory"/"off"/"none" to disable file persistence.
export NOLE_QUOTA_LEDGER_PATH="$HOME/.local/state/nole/quota-ledger.json"
export NOLE_CACHE_TTL="5m"                  # or NOLE_CACHE_TTL_SECONDS="300"
```

Nólë's quota ledger is **file-backed by default** at `$XDG_STATE_HOME/nole/quota-ledger.json` (or `~/.local/state/nole/quota-ledger.json` when `XDG_STATE_HOME` is unset). Durability is required for the monthly free-tier cap to be meaningful: an in-memory ledger resets to the full free quota on every process restart, which defeats the cap when nole is spawned per session (the typical MCP client pattern). Set `NOLE_QUOTA_LEDGER_PATH` to override the default location, or to `memory`/`off`/`none` to explicitly disable file persistence — only do that if you understand the per-restart reset implication. The ledger stores provider names, cost classes, local free-quota counters and local estimated spend; it does not store provider keys or raw provider payloads. If a configured ledger is corrupt, Nólë backs it up and fails closed for paid/quota-tracked providers while still allowing keyless-free providers. `NOLE_CACHE_TTL` enables an in-memory TTL cache for normalized search/extract responses inside a running process, such as `nole mcp`; cache hit/miss status appears in `route_trace` and compact `routing_insight`.

Do not paste real keys into chat, GitHub issues, docs, PRs or logs. Put keys in the process environment or a local-only env file such as `~/.config/nole/.env`. Nólë commands load that file without overriding existing process env values; GUI/service clients should still use the generated `nole-mcp` wrapper so their MCP subprocess gets the same env. Keep that file out of git and restrict permissions.

For Scrapling, prefer `nole setup --local-extract`; it installs Scrapling into a Nólë-owned venv and points `NOLE_SCRAPLING_PYTHON` at that executable. Manual env setup is still supported. Nólë calls Scrapling as a local optional runtime only. Respect target website terms, robots.txt and rate limits.

## Benchmarks and evidence

Nólë has two different benchmark/evidence concepts:

- Deterministic offline harness: validates routing/fallback contracts using fixtures. It does not measure live web quality.
- Optional live benchmark summaries: low-limit, explicit smoke/evidence runs against configured providers. They must be sanitized before sharing or committing.

Use:

```bash
nole bench --json
nole bench --evidence-md
# Optional, explicit, low-limit live smoke only:
nole bench --live --max-live-cases 3 --json
nole bench --live --max-live-cases 3 --evidence-md
```

Route matrix changes should be backed by sanitized evidence. `docs/ROUTE-EVIDENCE.md` records the current deterministic fixture summary, dated live task-provider evidence where available, and states what each artifact does not measure. Do not commit raw provider payloads, headers, private URLs or private queries.

See `docs/BENCHMARKS.md`.

## Interfaces

Stable/core:

- CLI: `nole search`, `nole extract`, `nole classify`, `nole route-plan`, `nole providers`, `nole doctor`, `nole bench`, `nole version`.
- MCP stdio: `nole mcp` for agent tools `search`, `extract`, `provider_status`, `budget_status`.
- Routing insight: `routing_insight` is a compact user-facing explanation; `route_trace` remains the structured debugging surface. Agents should cite the compact insight in normal answers and reserve full traces for troubleshooting.

Higher-level/aggregate:

- `nole research <question>` runs a multi-step search + extract + synthesis pass with citations on top of the core routing layer.
- `nole version` prints the binary's version, commit, and build date (stamped into release builds via `ldflags`; a development build reports `unknown` for the unstamped fields).

Experimental:

- HTTP/REST via `nole serve`. Keep REST claims conservative until it has the same hardening and compatibility coverage as CLI/MCP.

## Safety rules

- Keep MCP stdout protocol-clean; logs go to stderr.
- Preserve user config files; merge unknown fields, write backups and do not widen permissions.
- Never print or commit secrets, bearer tokens, auth headers or raw provider bodies.
- Keep default cost behavior free-tier/BYOK-safe.
- Treat local extract providers as user-controlled runtimes; do not vendor third-party scraper code into Nólë.
- Mark client integrations unverified until tested against the real client.
- Do not change provider route ordering without sanitized benchmark/evidence.

## Release and packaging

Nólë is a public repository, but published GitHub Releases are created only
from approved semantic version tags. The release workflow builds the
cross-platform binaries, generates `SHA256SUMS`, and creates a GitHub Release
automatically when a `v*.*.*` tag is pushed.

See:

- `docs/PUBLIC-RELEASE-CHECKLIST.md` for the release decision checklist.
- `docs/RELEASE-NOTES-v0.3.2.md` for the current release notes.
- `docs/PACKAGING.md` for release build automation and future package channels.
- `docs/COST-QUOTA-CACHE-QUALITY.md` for the cost/quota/cache/output-quality audit.

Docs do not publish releases, upload assets, publish packages or deploy
endpoints by themselves. A release is published by the tag-triggered workflow
after a maintainer creates an approved version tag.

## Roadmap

Current maintenance line:

1. Product framing and agent-readable install docs.
2. CI and release gates for tests, vet, doctor, bench and public-safety checks.
3. LLM-free multi-intent planner with `--task`/`--tasks` override compatibility.
4. Compact one-line Nólë insight alongside structured `route_trace`.
5. Cost policy model: free-first default, premium-capable support, fail-closed no-hidden-spend behavior.
6. Honest benchmark/evidence docs and optional sanitized live summaries.
7. TTL cache and quota/cost ledger.
8. Priority agent verification matrix.

See `docs/NEXT-STEPS.md` for the detailed execution roadmap.
