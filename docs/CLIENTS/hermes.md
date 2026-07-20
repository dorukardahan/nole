# Hermes Agent client

Status: verified (Hermes Agent MCP profile path + chat-agent tool dispatch).

Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools. Hermes Agent live verification is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md` from a 2026-05-20 run on an Ubuntu x86_64 host with Hermes Agent installed. Hermes Agent v2026.5.28 / v0.15.0 source compatibility was reviewed on 2026-05-28; keep the status label at v0.14 live-verified until a real v0.15 client run is recorded.

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
    timeout: 120
    connect_timeout: 60
    tools:
      resources: false
      prompts: false
```

Hermes v0.15 filters stdio MCP subprocess environments by default: only safe baseline variables plus values explicitly placed in the MCP server config are passed through. For Nólë, the safer pattern is still the wrapper/env-file path, not putting provider key values in Hermes config. The wrapper sources `~/.config/nole/.env` and execs `nole mcp`; Nólë also loads that env file on startup. Do not put key values in Hermes config.

The `tools.resources=false` and `tools.prompts=false` policy keeps Hermes from adding MCP resource/prompt utility wrappers for Nólë. Nólë's intended Hermes tool surface is its native MCP tools: `search`, `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop), `provider_status` and `budget_status`.

## Hermes v0.15 source review

Reviewed upstream release: Hermes Agent v2026.5.28 / v0.15.0, published 2026-05-28.

Findings:

- Required fix: none for the core config schema. Hermes still reads MCP servers from `~/.hermes/config.yaml` under `mcp_servers`, with `command`, `args`, `timeout`, `connect_timeout`, `tools`, and `enabled`.
- High-value enhancement implemented in Nólë: new Hermes Nólë entries now include `tools.resources=false` and `tools.prompts=false`.
- Operational note: prefer `nole setup --hermes --local-extract` so local Scrapling and provider keys are available through the wrapper after Hermes' stdio env filtering.
- Optional exploration: a future upstream Hermes MCP catalog entry could make Nólë discoverable in `hermes mcp`, but that requires a Hermes upstream PR and is not part of this Nólë repo change.

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
- `extract` works out of the box (keyless, no JavaScript) via the `httpfetch` backstop. For higher-fidelity / JS-rendered extraction, run `nole setup --local-extract` (or set a Tavily/Firecrawl key) and start a new Hermes session.
- If keys are missing, check the environment/profile of the process that launches MCP tools, not just the interactive shell.
- Do not enable hosted/proxy behavior unless the user explicitly requests it.
