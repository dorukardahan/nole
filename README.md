# searchmcp

BYOK free-tier search/retrieval router for AI agents. Single Go binary, MCP stdio + CLI.

## Why?

AI coding agents (Claude Code, Cursor, Codex CLI, OpenCode, Windsurf) need web search and content extraction. Existing options are either paid-only, TypeScript-only, or use blind sequential fallback. searchmcp does **task-based routing** across free-tier providers with a $0 hard cap.

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

Task types route to the best available provider:

- **general**: brave -> tavily -> ddgs
- **news**: brave -> tavily -> ddgs
- **docs**: brave -> firecrawl -> jina -> ddgs
- **research**: tavily -> brave -> jina -> firecrawl -> ddgs
- **extract**: jina -> firecrawl

Unavailable providers (no API key) are skipped automatically.

## Install

```bash
go build -o searchmcp .
# or download from Releases (TODO)
```

## Quick Start

```bash
# 1. Set your API keys
export BRAVE_API_KEY=...
export JINA_API_KEY=...

# 2. Verify
searchmcp doctor

# 3. Search
searchmcp search "Go MCP SDK" --task docs --json

# 4. Extract
searchmcp extract "https://go.dev" --json

# 5. Setup your agent
searchmcp setup --claude
searchmcp setup --cursor
searchmcp setup --all
```

## Commands

```
searchmcp doctor              # Check config and provider health
searchmcp providers --json    # List available providers
searchmcp search <query>      # Search with task-based routing
searchmcp extract <url>       # Extract clean content from URL
searchmcp setup --all         # Configure AI agents
searchmcp mcp                 # Start MCP stdio server
```

## Agent Setup

One command configures all supported agents:

```bash
searchmcp setup --all
```

Or individually:

```bash
searchmcp setup --claude    # ~/.claude/mcp.json
searchmcp setup --cursor    # ~/.cursor/mcp.json
searchmcp setup --codex     # ~/.codex/config.toml
searchmcp setup --opencode  # ~/opencode.json
searchmcp setup --windsurf  # ~/.codeium/windsurf/mcp_config.json
```

## MCP Tools

When running as an MCP server, searchmcp exposes:

- **search** -- query, task (general/news/docs/research), limit
- **extract** -- url, format
- **provider_status** -- which providers are available
- **budget_status** -- quota usage

## Architecture

```
internal/
  core/        -- types, registry, router, service, quota, errors
  cli/         -- cobra commands: search, extract, providers, doctor, setup, mcp
  mcpserver/   -- MCP stdio server using mark3labs/mcp-go
  providers/
    brave/     -- Brave Web Search API
    tavily/    -- Tavily Search + Extract API
    jina/      -- Jina s.jina.ai + r.jina.ai
    firecrawl/ -- Firecrawl /v2/search + /v2/scrape
    ddgs/      -- DuckDuckGo HTML scraping (keyless)
    mock/      -- Deterministic mock for testing
```

## License

MIT
