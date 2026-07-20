# Claude Code client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools. Claude Code can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it).

## What is verified

- Real Claude Code release was exercised on macOS (M11 live verification).
- The MCP entry is registered via the official `claude mcp add` CLI at user scope, pointing at `/absolute/path/to/nole-mcp`.
- `claude mcp list` reports `nole` connected.
- `claude mcp get nole` reports `Scope: User config`, `Type: stdio`, `Status: Connected`.
- Tools observable through the same wrapper command path: `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability).
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback.
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed during verification.

## What is also tested in this repo

- `nole setup --claude` no longer writes a misleading `~/.claude/mcp.json`. It prints the exact `claude mcp add nole -s user -- <command>` invocation the installed Claude Code release reads, with the wrapper path substituted in when `--mcp-wrapper` is given. Repo tests cover both the bare-binary and wrapper-mode instruction output and assert no stale config files are created.
- `nole doctor --mcp` verifies Nólë's own MCP stdio startup and tool list.

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
# 1. Create the local extract runtime and env-sourcing wrapper.
nole setup --local-extract
# 2. Have nole print the official register command (no file is written):
nole setup --claude --mcp-wrapper "$HOME/.local/bin/nole-mcp"
# 3. Run the printed command:
claude mcp add nole -s user -- /absolute/path/to/nole-mcp
# 4. Confirm the server is connected:
claude mcp list
claude mcp get nole
# 5. Sanity-check Nólë's MCP stdio outside the client:
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
- Claude Code shows MCP tools `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability);
- a small docs search works;
- no credentials appear in logs or chat.

Suggested first prompt:

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite result URLs.
```

## Troubleshooting

- If tools are missing, restart Claude Code after config changes.
- `extract` works out of the box (keyless, no JavaScript) via the `httpfetch` backstop. For higher-fidelity / JS-rendered extraction, run `nole setup --local-extract` (or set a Tavily/Firecrawl key) and restart Claude Code.
- If provider keys are missing, check launch environment.
- If MCP fails immediately, run `nole doctor --mcp` outside Claude Code.
- If stdout pollution appears, file a bug; `nole mcp` stdout must remain protocol-only.
