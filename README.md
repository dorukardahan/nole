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
| Claude Code | Setup writer present; config merge tested; live client verification pending | Uses MCP stdio config. |
| Codex CLI | Setup writer present; TOML merge tested; live client verification pending | Codex setup can source `~/.config/nole/.env` without writing secrets into config. |
| OpenCode | Setup writer present; config merge path tested; live client verification pending | Uses OpenCode MCP config shape. |
| OpenClaw | Generic MCP documentation present; live verification pending | Marked unverified until tested against the real client. |
| Hermes Agent | Generic MCP documentation present; live verification pending | Marked unverified until tested against a local Hermes config. |

Also documented as secondary/generic paths: Cursor CLI, Kimi and generic MCP clients. A client is only called verified after config path, tool visibility and `doctor --mcp` behavior are checked without printing credentials.

See `docs/CLIENTS/` and `docs/AGENT-INSTALL.md`.

## Providers

Nólë currently supports these provider adapters:

- Brave Search API: search, BYOK/free-tier capable.
- Tavily: search + extract, BYOK/free-tier/premium-capable depending on your account.
- Jina: search + extract, BYOK/free-tier/premium-capable depending on your account.
- Firecrawl: search + extract, BYOK/free-tier/premium-capable depending on your account.
- DDGS: keyless search fallback.

Nólë reads provider credentials from environment variables. It should only report whether a key is present; it must never print key values, auth headers or raw provider payloads.

See `docs/PROVIDER-KEYS.md` for provider-by-provider setup and overage cautions.

## Quick start

Prerequisites:

- Go 1.23+ for building from source.
- Optional provider keys for Brave, Tavily, Jina and Firecrawl.
- No provider key is required for the deterministic benchmark or DDGS keyless fallback.

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

Install the binary somewhere on PATH:

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
nole doctor
```

Configure an agent when the setup writer is available:

```bash
nole setup --claude
nole setup --codex
nole setup --opencode
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

Default stance: free-tier/BYOK-safe. Nólë should not create hidden paid usage by default. If you add premium-capable provider accounts, Nólë should use them according to policy, route evidence and task fit rather than picking a paid provider just because a key exists.

Environment variables:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export JINA_API_KEY="..."
export FIRECRAWL_API_KEY="..."
```

Do not paste real keys into chat, GitHub issues, docs, PRs or logs. If a GUI agent does not inherit your shell environment, put keys in a local-only env file such as `~/.config/nole/.env` and configure the client launcher to source it. Keep that file out of git and restrict permissions.

## Benchmarks and evidence

Nólë has two different benchmark/evidence concepts:

- Deterministic offline harness: validates routing/fallback contracts using fixtures. It does not measure live web quality.
- Optional live benchmark summaries: low-limit, explicit smoke/evidence runs against configured providers. They must be sanitized before sharing or committing.

Use:

```bash
nole bench --json
# Optional, explicit, low-limit live smoke only:
nole bench --live --max-live-cases 3 --json
```

Route matrix changes should be backed by sanitized evidence. Do not commit raw provider payloads, headers, private URLs or private queries.

See `docs/BENCHMARKS.md`.

## Interfaces

Stable/core:

- CLI: `nole search`, `nole extract`, `nole classify`, `nole route-plan`, `nole providers`, `nole doctor`, `nole bench`.
- MCP stdio: `nole mcp` for agent tools `search`, `extract`, `provider_status`, `budget_status`.

Experimental:

- HTTP/REST via `nole serve`. Keep REST claims conservative until it has the same hardening and compatibility coverage as CLI/MCP.

## Safety rules

- Keep MCP stdout protocol-clean; logs go to stderr.
- Preserve user config files; merge unknown fields, write backups and do not widen permissions.
- Never print or commit secrets, bearer tokens, auth headers or raw provider bodies.
- Keep default cost behavior free-tier/BYOK-safe.
- Mark client integrations unverified until tested against the real client.
- Do not change provider route ordering without sanitized benchmark/evidence.

## Roadmap

Near-term v0.1 private-prep:

1. Product framing and agent-readable install docs.
2. CI gates for tests, vet, doctor, bench and public-safety checks.
3. LLM-free multi-intent planner with `--task`/`--tasks` override compatibility.
4. Compact one-line Nólë insight alongside structured `route_trace`.
5. Cost policy model: free-first default, premium-capable support, fail-closed no-hidden-spend behavior.
6. Honest benchmark/evidence docs and optional sanitized live summaries.
7. TTL cache and quota/cost ledger.
8. Priority agent verification matrix.

See `docs/NEXT-STEPS.md` for the detailed execution roadmap.
