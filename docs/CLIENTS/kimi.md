# Kimi client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a free, local web search router for AI agents and coding CLI tools. Kimi can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it). Kimi has a first-class MCP manager CLI (`kimi mcp add/list/test`), which makes it straightforward to register and verify Nólë.

## What is verified

- Real Kimi CLI release was exercised on macOS (M11 live verification).
- The MCP entry is registered via the official `kimi mcp add` CLI, pointing at `/absolute/path/to/nole-mcp`.
- `kimi mcp list` reports `nole (stdio)`.
- `kimi mcp test nole` reports `Connected` and the tools Nólë advertises: `budget_status`, `provider_status`, `search`, plus `extract` when Tavily, Firecrawl or local Scrapling is configured.
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback (shared smoke; recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`).
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed during verification; the wrapper sources `~/.config/nole/.env` only at launch.

## Setup

Build and install Nólë first:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole setup --local-extract
nole doctor --mcp
```

`nole setup --local-extract` creates the env-sourcing wrapper at `~/.local/bin/nole-mcp` (`chmod 700`); see `docs/PROVIDER-KEYS.md` and `docs/AGENT-INSTALL.md`.

Either run Nólë's built-in writer:

```bash
nole setup --kimi --mcp-wrapper /absolute/path/to/nole-mcp
```

…or register the entry through Kimi's MCP manager directly:

```bash
kimi mcp add nole -- /absolute/path/to/nole-mcp
```

Both paths produce the same `~/.kimi/mcp.json` shape. Then verify:

```bash
kimi mcp list
kimi mcp test nole
```

`kimi mcp test nole` is the single best per-client verification: it both connects and lists the available tools.

## Manual config shape

Kimi stores MCP server configuration under `~/.kimi/mcp.json`. The `kimi mcp add` CLI is preferred over hand-editing that file, but the shape Kimi writes is:

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole-mcp"
    }
  }
}
```

Do not place provider key values into shared config; the wrapper handles env sourcing at launch.

## Verification checklist

Mark this client `verified` only after:

- `nole doctor` passes with key presence only;
- `nole doctor --mcp` passes;
- `kimi mcp test nole` reports `Connected` and shows `search`, `provider_status`, `budget_status`, plus `extract` when Tavily, Firecrawl or local Scrapling is configured;
- a small docs search works;
- no credentials appear in config, logs or chat.

## Follow-ups

- Live verification on additional hosts/Kimi versions remains an open item — see `docs/CLIENTS/LIVE-VERIFICATION.md` and `docs/NEXT-STEPS.md`.
- The Kimi process surfaces an upstream `AuthlibDeprecationWarning` from `fastmcp` on stderr; that is unrelated to Nólë and does not affect the MCP protocol.

## Troubleshooting

- If `kimi mcp test nole` cannot connect, run `nole doctor --mcp` directly to confirm Nólë's MCP stdio is healthy and that key presence is detected.
- If `extract` is missing and no Tavily/Firecrawl key is configured, run `nole setup --local-extract` and restart the Kimi session.
- If provider keys are missing inside Kimi but present in your interactive shell, confirm the wrapper exists and points to a built `nole` binary (`NOLE_BIN`, `command -v nole` or `~/.local/bin/nole` in order of preference).
- If stdout pollution appears in Kimi's MCP logs, file a bug; `nole mcp` stdout must remain protocol-only.
