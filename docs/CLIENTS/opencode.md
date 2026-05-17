# OpenCode client

Status: repo-tested, live client verification pending.

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenCode can use Nólë through MCP stdio by launching `nole mcp`.

## What is tested in this repo

- Nólë has an OpenCode setup writer.
- The writer upserts a `nole` MCP server entry into the OpenCode JSON config shape while preserving existing data.
- `nole doctor --mcp` verifies Nólë's own MCP stdio behavior.

Not yet claimed:

- End-to-end OpenCode client tool visibility on this machine.

## Setup

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
nole setup --opencode
nole doctor --mcp
```

## Generic config template

If manual config is needed:

```json
{
  "mcp": {
    "nole": {
      "command": "/absolute/path/to/nole",
      "args": ["mcp"]
    }
  }
}
```

Confirm the exact OpenCode config location and schema for the installed version before marking the integration verified.

## Provider keys

Prefer process environment or a local wrapper script. Do not put raw key values into shared config.

## Verification checklist

Mark this client `verified` only after:

- `nole doctor --mcp` passes;
- OpenCode sees `search`, `extract`, `provider_status`, `budget_status`;
- a small docs search works;
- no secret values are present in config or logs.

## Troubleshooting

- Restart OpenCode after config changes.
- Use absolute binary paths if PATH is not inherited.
- Check provider key visibility with `nole doctor`.
- Keep HTTP/REST disabled unless explicitly testing experimental REST behavior.
