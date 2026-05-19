# Claude Code client

Status: repo-tested, live client verification pending.

Nólë is a free, local web search router for AI agents and coding CLI tools. Claude Code can use Nólë through MCP stdio by launching `nole mcp`.

## What is tested in this repo

- Nólë has a setup writer for Claude-style MCP JSON config.
- Tests cover preserving existing MCP servers while upserting the `nole` server key.
- `nole doctor --mcp` verifies Nólë's own MCP stdio startup and tool list.

Not yet claimed:

- End-to-end Claude Code UI/CLI tool visibility on this machine.

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

Run:

```bash
nole setup --claude
nole doctor --mcp
```

If Claude Code does not inherit PATH, configure the MCP entry with `/absolute/path/to/nole` instead of relying on the `nole` command name.

The setup writer targets the user's Claude MCP config and adds a server named `nole` with command `nole` or the absolute current executable path.

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
