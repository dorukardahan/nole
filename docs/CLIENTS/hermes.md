# Hermes Agent client

Status: verified (Hermes Agent MCP profile path + chat-agent tool dispatch).

Nólë is a free, local web search router for AI agents and coding CLI tools. Hermes Agent live verification is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md` from a 2026-05-20 run on an Ubuntu x86_64 host with Hermes Agent installed.

The verified path used a disposable Hermes profile, not the active default gateway profile. The Nólë MCP server was registered as `nole` with a direct absolute Nólë binary command and `args: ["mcp"]`; committed docs use placeholder paths only.

## Verified setup shape

Use the setup writer or Hermes Agent's MCP manager with an absolute binary path unless you have confirmed the Hermes process inherits the right PATH and provider environment. For normal installs, include `--local-extract` so the local Scrapling fallback and env-sourcing wrapper are ready before Hermes starts a session:

```bash
nole setup --hermes --local-extract
# or, if Hermes needs an env-sourcing wrapper for provider keys:
nole setup --hermes --mcp-wrapper /absolute/path/to/nole-mcp

hermes mcp list
hermes mcp test nole
```

Equivalent manual command:

```bash
hermes mcp add nole --command /absolute/path/to/nole --args mcp
```

Sanitized config shape:

```yaml
mcp_servers:
  nole:
    command: /absolute/path/to/nole-mcp
    args: []
```

If the Hermes runtime that will launch MCP tools does not inherit provider keys, use a local-only wrapper that sources `~/.config/nole/.env` and execs `nole mcp`, then point Hermes at the wrapper instead. Do not put key values in Hermes config.

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
export PATH="$HOME/.local/go/bin:$PATH"  # if Go is installed under ~/.local/go
go test ./...
go build -o nole .
./nole doctor
./nole setup --local-extract
./nole doctor --mcp
```

## Provider keys and cost policy

Hermes launch environments may differ between CLI, gateway, service and profile modes. Keep provider keys in the process environment, Hermes profile `.env`, or a local wrapper/env file. Never print values.

Default Nólë policy is `free-first`. In the recorded Hermes verification, no keyed paid providers were available to the Nólë MCP subprocess, so Brave, Firecrawl and Tavily were skipped as `disabled_no_key`; DDGS was used as the keyless-free fallback. This is acceptable for live smoke verification but is not a claim that DDGS is the benchmark-primary docs provider.

## Recorded verification checklist

The 2026-05-20 Hermes Agent run recorded:

- Hermes Agent version/config mode tested: Hermes Agent v0.14.0, disposable cloned profile, gateway stopped for that profile.
- Active default gateway profile: left untouched.
- Config surface: `hermes -p <temporary-profile> mcp add/list/test`.
- MCP entry: server name `nole`, direct absolute Nólë binary command, args `["mcp"]`.
- `nole doctor --mcp`: startup stdout clean; initialize/tools/list succeeded; tools `[budget_status extract provider_status search]`.
- Hermes MCP tools visible: `mcp_nole_search`, `mcp_nole_extract`, `mcp_nole_provider_status`, `mcp_nole_budget_status`.
- Hermes chat-agent dispatch: `provider_status` succeeded, then exactly one `search` succeeded with query `Go net/http Client Timeout documentation`, task `docs`, limit `1`.
- Search provider used: `ddgs` keyless-free fallback.
- Result URL: `https://pkg.go.dev/net/http`.
- Paid spend: none.
- Committed evidence: sanitized; no provider key values, bearer tokens, auth headers, raw provider payloads, private URLs, machine-specific absolute paths or chat transcripts are included.

## Suggested first prompt

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite result URLs.
```

## Troubleshooting

- Use absolute paths if the Hermes process cannot find `nole`.
- Use a dedicated Hermes profile for verification or experiments so active gateway profiles are not disturbed.
- Start a new Hermes session after MCP config changes; tool schemas are loaded at session start.
- If `extract` is missing and no Tavily/Firecrawl key is configured, run `nole setup --local-extract` and start a new Hermes session.
- If keys are missing, check the environment/profile of the process that launches MCP tools, not just the interactive shell.
- Do not enable hosted/proxy behavior unless the user explicitly requests it.
