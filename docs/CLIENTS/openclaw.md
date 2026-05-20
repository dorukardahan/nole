# OpenClaw client

Status: verified (OpenClaw Gateway/agent MCP path).

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenClaw can use Nólë through its saved outbound MCP server registry and Gateway-backed agent runtime.

Live verification for OpenClaw 2026.5.18 is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.

## Verified setup shape

OpenClaw's verified MCP configuration mechanism is the CLI-managed MCP registry:

```json
{
  "mcp": {
    "servers": {
      "nole": {
        "command": "/absolute/path/to/nole-mcp",
        "args": []
      }
    }
  }
}
```

Use OpenClaw's CLI rather than hand-editing when possible:

```bash
openclaw mcp set nole '{"command":"/absolute/path/to/nole-mcp","args":[]}'
openclaw mcp show nole --json
```

The wrapper sources `~/.config/nole/.env` and execs `nole mcp`; OpenClaw config must not contain provider key values.

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

## What is verified

The 2026-05-20 OpenClaw run verified:

- OpenClaw version: 2026.5.18.
- Config surface: `openclaw mcp set/show/list`, stored under OpenClaw's `mcp.servers` config.
- Nólë launch: wrapper-direct command, `command_basename=nole-mcp`, 0 args, 0 env keys.
- `nole doctor --mcp`: startup-clean, `initialize` and `tools/list` ok, tools `[budget_status extract provider_status search]`.
- OpenClaw tool visibility: `search`, `extract`, `provider_status`, `budget_status`.
- OpenClaw tool dispatch: `provider_status` succeeded through the Nólë MCP path.
- Search smoke through OpenClaw: query `Go net/http Client Timeout documentation`, task `docs`, limit `1`, provider `ddgs`, result URL `https://pkg.go.dev/net/http`.
- Cost policy: `free-first`; paid spend: none.
- Secret-safety: no provider key values, bearer tokens, auth headers, raw provider payloads, private URLs or machine-specific absolute paths are recorded.

## Troubleshooting

- Use an absolute path to `nole-mcp`.
- If OpenClaw cannot see Nólë after `openclaw mcp set`, start a fresh agent session so the runtime reloads MCP definitions.
- Run `nole doctor --mcp` directly to separate Nólë stdio issues from OpenClaw runtime issues.
- Inspect OpenClaw logs only after redacting provider keys, auth headers, raw provider payloads, private URLs and local paths.
