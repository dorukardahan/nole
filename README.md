# Nólë

Nólë (Quenya for "Deep Knowledge / Research") is an MCP server that orchestrates diverse search tools to perform deep, task-oriented internet research. It ships as a BYOK free-tier search/retrieval router for AI agents: single Go binary, MCP stdio + CLI.

## Why?

AI coding agents (Claude Code, Cursor, Codex CLI, OpenCode, Windsurf) need web search and content extraction. Existing options are either paid-only, TypeScript-only, or use blind sequential fallback. Nólë does **task-based routing** across free-tier providers with a $0 hard cap.

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

Task types route to the best available provider based on benchmark testing:

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

Unavailable providers (no API key) are skipped automatically.

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

# 3. Search
nole search "Go MCP SDK" --task docs --json

# 4. Extract
nole extract "https://go.dev" --json

# 5. Research (multi-step search + extract + synthesis)
nole research "What is MCP Model Context Protocol"

# 6. Setup your agent
nole setup --all
```

## Commands

```
nole doctor              # Check config and provider health
nole providers --json    # List available providers
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

One command configures all supported agents:

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

## MCP Tools

When running as an MCP server (`nole mcp`), exposes:

- **search** -- query, task, limit
- **extract** -- url, format
- **provider_status** -- which providers are available
- **budget_status** -- quota usage

## Architecture

```
internal/
  core/        -- types, registry, router, service, quota, errors
  cli/         -- cobra commands: search, extract, research, providers, doctor, setup, mcp, serve
  mcpserver/   -- MCP stdio server using mark3labs/mcp-go
  providers/
    brave/     -- Brave Web Search API
    tavily/    -- Tavily Search + Extract API
    jina/      -- Jina s.jina.ai + r.jina.ai
    firecrawl/ -- Firecrawl /v2/search + /v2/scrape
    ddgs/      -- DuckDuckGo HTML scraping (keyless)
    mock/      -- Deterministic mock for testing
```

## Security

- API keys are read from environment variables only. Never stored or logged.
- `$0.00 hard cap`: no paid requests are ever made. If no free route exists, returns a structured error.
- MCP stdio mode keeps stdout clean for JSON-RPC; all logs go to stderr.
- No telemetry, no tracking, no external data collection.

## License

MIT
