# OpenCode client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenCode can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it).

## What is verified

- Real OpenCode release was exercised on macOS (M11 live verification).
- A `nole` MCP entry written directly into `~/.config/opencode/opencode.json` with the OpenCode-native schema is read by `opencode mcp list` and reports `nole connected`.
- Tools observable through the same wrapper command path: `search`, `extract`, `provider_status`, `budget_status`.
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback.
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths appear in the OpenCode entry; provider keys are loaded only by the wrapper at launch.

## What is also tested in this repo

- Nólë has an OpenCode setup writer.
- The writer upserts a `nole` MCP server entry into a JSON config while preserving existing data.
- `nole doctor --mcp` verifies Nólë's own MCP stdio behavior.

## Known limitation in this run

`nole setup --opencode` writes to `~/opencode.json` using a `{command, args}` schema, but the installed OpenCode release reads `~/.config/opencode/opencode.json` and uses a `{type, command:[…], enabled, environment}` schema. Until the writer is updated, write the entry directly or use `opencode mcp add`. Tracked as a follow-up in `docs/NEXT-STEPS.md`.

## Setup

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole doctor --mcp
```

Recommended path against the installed OpenCode release (direct config entry, OpenCode-native schema):

```json
// inside ~/.config/opencode/opencode.json
{
  "mcp": {
    "nole": {
      "type": "local",
      "command": ["/absolute/path/to/nole-mcp"],
      "enabled": true,
      "environment": {}
    }
  }
}
```

After editing, run:

```bash
opencode mcp list
```

and confirm `nole` is listed as connected.

## Generic config template (older OpenCode schema)

If manual config is needed for an older OpenCode build that still reads `{command, args}`:

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
