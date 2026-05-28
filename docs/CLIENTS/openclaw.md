# OpenClaw client

Status: verified (OpenClaw Gateway/agent MCP path).

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenClaw can use Nólë through its saved outbound MCP server registry and Gateway-backed agent runtime.

Live verification for OpenClaw 2026.5.18 and a compatibility re-check for OpenClaw 2026.5.27 are recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.

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
nole setup --local-extract
openclaw mcp set nole '{"command":"/absolute/path/to/nole-mcp","args":[]}'
openclaw mcp show nole --json
```

The wrapper sources `~/.config/nole/.env` and execs `nole mcp`; OpenClaw config must not contain provider key values. `nole setup --local-extract` creates that wrapper and writes the local Scrapling Python path into the env file.

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole setup --local-extract
./nole doctor --mcp
```

## 2026-05-28 Compatibility Re-Check

OpenClaw 2026.5.27 was re-checked against the wrapper-backed MCP registry shape:

- OpenClaw version: 2026.5.27 (`27ae826`).
- Config surface: `mcp.servers.nole` stored in OpenClaw config.
- Nólë launch: wrapper-direct command, `command_basename=nole-mcp`, 0 args, 0 env keys in OpenClaw config.
- `nole doctor --mcp` passed with `NOLE_MCP_SMOKE_BINARY` pointed at the OpenClaw wrapper: startup-clean, `initialize` and `tools/list` ok, tools `[budget_status extract provider_status search]`.
- Provider surface through the wrapper: Brave, DDGS, Firecrawl, Scrapling and Tavily available; Scrapling was local Python package `0.4.8`.
- Cost policy: `free-first`; paid spend: none.
- Secret-safety: provider key values stayed in the wrapper/env file and were not stored in OpenClaw config or recorded in verification notes.

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
- If `extract` is missing and no Tavily/Firecrawl key is configured, run `nole setup --local-extract` and start a fresh agent session.
- Run `nole doctor --mcp` directly to separate Nólë stdio issues from OpenClaw runtime issues.
- Inspect OpenClaw logs only after redacting provider keys, auth headers, raw provider payloads, private URLs and local paths.
