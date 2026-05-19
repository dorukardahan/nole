# Cursor client

Status: repo-tested, live client verification pending.

Nólë is a free, local web search router for AI agents and coding CLI tools. Cursor-style MCP clients can launch Nólë through `nole mcp`.

## Setup

Nólë includes a Cursor setup flag and repo coverage for the shared MCP JSON merge helper, but the real Cursor client has not been verified in this milestone:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole setup --cursor
nole doctor --mcp
```

If Cursor does not inherit PATH, use the generic config template with an absolute binary path.

## Generic config template

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

## Verification checklist

Mark Cursor verified only after:

- Cursor version and config path are recorded;
- Nólë tools `search`, `extract`, `provider_status`, `budget_status` are visible in Cursor;
- one small search works;
- no secrets appear in config/logs;
- provider keys are visible to the Cursor-launched process.

Until then, treat this as a generic documented path.
