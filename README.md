# Nólë

Nólë (Quenya for "Deep Knowledge / Research") is an MCP server that orchestrates diverse search tools to perform deep, task-oriented internet research. It ships as a BYOK free-tier search/retrieval router for AI agents: single Go binary, MCP stdio + CLI.

## Why?

AI coding agents (Claude Code, Cursor, Codex CLI, OpenCode, Windsurf, Hermes, OpenClaw, Kimi CLI, and other MCP clients) need web search and content extraction. Nólë is not trying to replace any single model or CLI native browser; it provides a shared, measurable BYOK/free-tier research layer that agents can use through MCP or the CLI. Existing options are often paid-only, runtime-specific, or use blind sequential fallback. Nólë does **task-based routing** across free-tier providers with a $0 hard cap.

## Providers

| Provider | Search | Extract | Cost | Env Var |
|----------|--------|---------|------|---------|
| Brave | yes | no | Free (2k/mo) | `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY` |
| Tavily | yes | yes | Free tier | `TAVILY_API_KEY` |
| Jina | yes | yes | Free tier | `JINA_API_KEY` |
| Firecrawl | yes | yes | Free tier | `FIRECRAWL_API_KEY` |
| DuckDuckGo | yes | no | Free (keyless) | none |

All providers are optional. Set only the keys you have. DDGS works with zero configuration.

## Routing

Task types route to the best available provider from the current route matrix. Route changes should be evidence-backed by offline fixtures and/or explicit low-limit live benchmarks; this hardening pass keeps the existing matrix unchanged because it adds measurement infrastructure rather than new live provider evidence:

| Task | Route |
|------|-------|
| general | brave -> firecrawl -> ddgs -> tavily -> jina |
| news | brave -> ddgs -> tavily -> firecrawl -> jina |
| docs | brave -> firecrawl -> tavily -> ddgs -> jina |
| academic | brave -> tavily -> firecrawl -> ddgs -> jina |
| factcheck | brave -> tavily -> ddgs -> firecrawl -> jina |
| semantic | tavily -> firecrawl -> brave -> ddgs -> jina |
| code | brave -> firecrawl -> ddgs -> tavily -> jina |
| social | firecrawl -> ddgs -> brave -> tavily -> jina |
| people | tavily -> firecrawl -> brave -> ddgs -> jina |
| pricing | brave -> tavily -> firecrawl -> ddgs -> jina |
| research | brave -> firecrawl -> ddgs -> tavily -> jina |
| extract | tavily -> firecrawl -> jina |

Unavailable providers (no API key), quota-blocked providers, providers without the requested capability, empty search results, and empty extracted content are skipped automatically. Search and extract responses include a `route_trace` with provider, status, structured reason (`quota_blocked`, `provider_error`, `empty_results`, `empty_content`, etc.), latency, and result count so agents can explain why a provider was selected or skipped.

## Benchmark / Eval

Nólë includes a deterministic offline benchmark harness for routing-contract and failure-mode evaluation. Offline mode is the default and makes no provider network calls. Treat offline scores as fixture/contract evidence only, not proof that a route order is better on the live web.

```bash
nole bench
nole bench --json
```

Optional live smoke benchmarks require an explicit flag and use a low default case limit:

```bash
nole bench --live --max-live-cases 3
```

Do not commit raw live benchmark logs. If live runs are used to justify route changes, publish only sanitized summaries such as success counts, latency ranges, result counts, citation/source quality notes, and the fixture version. Route matrix changes should include that evidence in the same change.

## Install

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go build -o nole .
```

## Quick Start

```bash
# 1. Set your API keys
export BRAVE_API_KEY=...
export TAVILY_API_KEY=...
export JINA_API_KEY=...
export FIRECRAWL_API_KEY=...

# 2. Verify
nole doctor
nole doctor --mcp

# 3. Run the offline eval harness
nole bench --json

# 4. Search
nole search "Go MCP SDK" --task docs --json

# 5. Extract
nole extract "https://go.dev" --json

# 6. Research (multi-step search + extract + synthesis)
nole research "What is MCP Model Context Protocol"

# 7. Setup your agent
nole setup --all
```

## Commands

```
nole doctor              # Check config and provider health
nole doctor --mcp        # Also run MCP stdio initialize + tools/list smoke
nole providers --json    # List available providers
nole bench [--json]      # Run deterministic offline eval fixtures
nole search <query>      # Search with task-based routing
nole extract <url>       # Extract clean content from URL
nole research <question> # Multi-step search + extract + synthesis
nole setup --all         # Configure AI agents
nole mcp                 # Start MCP stdio server
nole serve --mcp         # Start HTTP MCP + REST API server
```

### Search Task Types

```
--task general    # Default broad web search
--task news       # Current events and headlines
--task docs       # Technical documentation lookup
--task academic   # Papers and research
--task factcheck  # Fact verification queries
--task semantic   # Conceptual/similar-to searches
--task code       # Code and implementation examples
--task social     # Forum and community discussions
--task people     # People and biography lookups
--task pricing    # Product/service pricing queries
--task research   # Deep multi-source research
```

## Agent Setup

One command configures the MCP clients that Nólë can currently write safely. Existing config files are merged rather than clobbered; when a file already exists, Nólë writes a `.bak` backup before updating the `nole` server entry. JSON merges preserve client-specific fields on unrelated servers, and config/backup writes preserve existing permissions or use `0600` for new sensitive files.

```bash
nole setup --all
```

Or individually:

```bash
nole setup --claude    # ~/.claude/mcp.json
nole setup --cursor    # ~/.cursor/mcp.json
nole setup --codex     # ~/.codex/config.toml
nole setup --opencode  # ~/opencode.json
nole setup --windsurf  # ~/.codeium/windsurf/mcp_config.json
```

For Hermes, OpenClaw, Kimi CLI, or any generic MCP client, add an MCP server named `nole` that runs the local binary with `mcp` as its argument:

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

Client config formats vary, but the server command is always the same: `nole mcp` over stdio. Start the client from an environment that contains any provider keys you want to use, or configure those keys in the client-specific MCP env block if supported.

### Will agents use Nólë automatically?

Usually yes after the MCP server is installed and enabled: agents see `search` for public web/current information/docs/fact-checking/code/pricing/source discovery and `extract` for reading public web pages. The user should not need to say "use Nólë" if their client automatically exposes MCP tools and allows the model to choose tools for web research.

Auto-use is still client-dependent. Some clients require MCP servers to be enabled per workspace, restarted after config changes, or approved before tool calls. If an agent is not selecting Nólë on its own, ask it to "use the search tool" once, check `nole doctor`, and verify the MCP server appears in the client's tool list.

## Observability and failure handling

- Search and extract responses include `route_trace` entries with selected provider, skip/fallback reason, provider latency, and result count. Empty search results and empty extracted content are fallback reasons, not successes.
- Transient provider responses (`429`, `502`, `503`, `504`) use bounded retry/backoff and respect `Retry-After` when present.
- Retries are intentionally low and configurable; Nólë never retries forever.
- MCP and CLI JSON error payloads preserve `route_trace` where practical while redacting provider error detail.
- MCP stdio keeps stdout reserved for JSON-RPC protocol. Run `nole doctor --mcp` to smoke-check startup stdout cleanliness and an actual `initialize` + `tools/list` exchange.

## MCP Tools

When running as an MCP server (`nole mcp`), exposes:

- **search** -- public web/current information/docs/fact-checking/code/pricing/source discovery (`query`, optional `task`, optional `limit`)
- **extract** -- clean readable content from a public web page URL (`url`, optional `format`)
- **provider_status** -- which providers are available
- **budget_status** -- quota usage

## Architecture

```
internal/
  core/        -- types, registry, router, service, quota, errors
  bench/       -- deterministic offline benchmark/eval fixtures and scoring
  cli/         -- cobra commands: search, extract, research, providers, bench, doctor, setup, mcp, serve
  mcpserver/   -- MCP stdio server using mark3labs/mcp-go
  providers/
    brave/     -- Brave Web Search API
    tavily/    -- Tavily Search + Extract API
    jina/      -- Jina s.jina.ai + r.jina.ai
    firecrawl/ -- Firecrawl /v2/search + /v2/scrape
    ddgs/      -- DuckDuckGo HTML scraping (keyless)
    mock/      -- Deterministic mock for testing
    providerhttp/ -- bounded retry/backoff helper for transient HTTP failures
```

## Security

- API keys are read from environment variables only. Never stored or logged.
- `$0.00 local hard cap`: Nólë only routes through configured free-tier/keyless entries and returns a structured error when no free route is locally allowed. Provider-side overage controls still depend on your BYOK account settings; configure providers to disable paid overage where available.
- MCP stdio mode keeps stdout clean for JSON-RPC; all logs go to stderr. `nole doctor --mcp` checks startup stdout cleanliness and lists the expected MCP tools without printing stderr contents.
- Provider HTTP errors do not print raw response bodies by default because providers can echo request bodies, auth headers, URLs, or keys.
- No Nólë-owned telemetry, tracking, or external data collection. Configured providers still receive the search/extract requests that you route to them.

## License

MIT
