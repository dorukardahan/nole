# Claude Code client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a free, local web search router for AI agents and coding CLI tools. Claude Code can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it).

## What is verified

- Real Claude Code release was exercised on macOS (M11 live verification).
- The MCP entry is registered via the official `claude mcp add` CLI at user scope, pointing at `/absolute/path/to/nole-mcp`.
- `claude mcp list` reports `nole` connected.
- `claude mcp get nole` reports `Scope: User config`, `Type: stdio`, `Status: Connected`.
- Tools observable through the same wrapper command path: `search`, `extract`, `provider_status`, `budget_status`.
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback.
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed during verification.

## What is also tested in this repo

- Nólë has a setup writer for Claude-style MCP JSON config; tests cover preserving existing MCP servers while upserting the `nole` server key.
- `nole doctor --mcp` verifies Nólë's own MCP stdio startup and tool list.

## Known limitation in this run

`nole setup --claude` currently writes to a Claude MCP config path that the installed Claude Code release does not read for user-scope MCP servers. For now, prefer the official `claude mcp add` path. The writer is tracked as a follow-up in `docs/NEXT-STEPS.md`.

## Setup

Build and install Nólë first:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
```

Recommended path (works against the installed Claude Code release; sources keys via the wrapper):

```bash
# 1. Create the env-sourcing wrapper (see docs/PROVIDER-KEYS.md and docs/AGENT-INSTALL.md).
#    Wrapper lives at: ~/.local/bin/nole-mcp; chmod 700.
# 2. Register Nólë with Claude Code's official MCP manager:
claude mcp add nole -s user -- /absolute/path/to/nole-mcp
# 3. Confirm the server is connected:
claude mcp list
claude mcp get nole
# 4. Sanity-check Nólë's MCP stdio outside the client:
nole doctor --mcp
```

If you want to try the setup writer (subject to the limitation noted above), run:

```bash
nole setup --claude
nole doctor --mcp
```

If Claude Code does not inherit PATH, configure the MCP entry with `/absolute/path/to/nole-mcp` (env-sourcing wrapper) or `/absolute/path/to/nole` (bare binary; provider keys must already be in Claude Code's launch env).

## Generic config template

If you configure manually, use an absolute path:

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

## Provider keys

Claude Code may not inherit shell environment depending on how it is launched. If keys are not visible in `nole doctor` from inside the client context, use a local wrapper or env file pattern from `docs/PROVIDER-KEYS.md`.

Never put real API key values into project-shared config.

## Verification checklist

Mark this client `verified` only after:

- `nole doctor` passes with key presence only;
- `nole doctor --mcp` passes;
- Claude Code shows MCP tools `search`, `extract`, `provider_status`, `budget_status`;
- a small docs search works;
- no credentials appear in logs or chat.

Suggested first prompt:

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite result URLs.
```

## Troubleshooting

- If tools are missing, restart Claude Code after config changes.
- If provider keys are missing, check launch environment.
- If MCP fails immediately, run `nole doctor --mcp` outside Claude Code.
- If stdout pollution appears, file a bug; `nole mcp` stdout must remain protocol-only.
