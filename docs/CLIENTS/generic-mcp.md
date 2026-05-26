# Generic MCP clients

Status: generic/unverified template.

Nólë exposes MCP stdio through:

```bash
nole mcp
```

Any MCP-compatible client that can launch a local stdio command should be able to call Nólë if configured with the right command and args.

## Generic config

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

Some clients use `servers`, `mcp`, TOML or YAML instead of `mcpServers`; adapt the schema but keep command semantics unchanged.

## Expected tools

A working client should show these tools:

- `search`
- `provider_status`
- `budget_status`
- `extract` when Tavily, Firecrawl or local Scrapling is configured

## Install and verify Nólë

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole setup --local-extract
./nole doctor --mcp
```

## Environment variables

Provider keys should come from the process environment or local env wrappers, not shared config:

Set these variable names locally when you have the matching provider accounts:

- `BRAVE_API_KEY` or `BRAVE_SEARCH_API_KEY`
- `TAVILY_API_KEY`
- `FIRECRAWL_API_KEY`

Do not print actual values. Use `nole doctor` to check presence only.

## Optional env-sourcing wrapper

If the client does not inherit the shell environment that owns provider keys, register Nólë via a local wrapper that sources `~/.config/nole/.env` and execs `nole mcp`:

```bash
nole setup --local-extract
```

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

See `docs/PROVIDER-KEYS.md` and `docs/AGENT-INSTALL.md` for the wrapper template. The wrapper is local-only; do not commit it.

If the client matches one of Nólë's setup writers (`--cursor`, `--opencode`, `--kimi`, `--windsurf`), the writer accepts `--mcp-wrapper /absolute/path/to/nole-mcp` and will emit the wrapper-direct entry for you. `nole setup --claude --mcp-wrapper /absolute/path/to/nole-mcp` prints the matching `claude mcp add` command.

## MCP stdout requirement

The `nole mcp` process must keep stdout for JSON-RPC only. If a client fails to initialize, run:

```bash
nole doctor --mcp
```

This checks startup cleanliness, `initialize` and `tools/list`.

## Verification checklist

A generic client can be marked verified after:

- exact config path and schema are documented;
- `nole doctor --mcp` passes;
- the client lists or can call all four tools;
- one low-limit search works;
- provider key values are not present in logs or config;
- any client-specific environment pitfalls are documented.
