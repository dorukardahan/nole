# OpenClaw client

Status: verified (OpenClaw Gateway/agent MCP path). The host bridge setup and current stable compatibility mode were live-checked on OpenClaw 2026.7.1; full keyless search waits for an OpenClaw release that exposes `firecrawl-free`.

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenClaw can use Nólë through its saved outbound MCP server registry and Gateway-backed agent runtime.

Live verification for OpenClaw 2026.5.18 and a compatibility re-check for OpenClaw 2026.5.27 are recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.

## Verified setup shape

Use the dedicated OpenClaw setup path:

```bash
nole setup --openclaw
openclaw mcp show nole --json
```

`nole setup --openclaw` performs the OpenClaw-specific work in one command:

1. resolves the installed `openclaw` CLI;
2. installs and pins the official `@openclaw/firecrawl-plugin` when it is absent, then enables it;
3. inspects the plugin's advertised providers and chooses one of two modes:
   - **full:** `firecrawl-free` search plus keyless Firecrawl fetch;
   - **fetch-only:** OpenClaw `web_fetch` with keyless Firecrawl fallback, while Nólë search keeps its existing non-Firecrawl fallbacks;
4. writes `~/.local/bin/nole-mcp-openclaw`, which scopes `NOLE_CLIENT=openclaw`, the absolute OpenClaw CLI path and the selected bridge mode to this MCP process only;
5. registers that wrapper with `openclaw mcp set nole`.

The saved OpenClaw entry contains no provider key or Gateway token:

```json
{
  "mcp": {
    "servers": {
      "nole": {
        "command": "/absolute/path/to/nole-mcp-openclaw",
        "args": []
      }
    }
  }
}
```

When this wrapper is active, Nólë delegates supported host search/fetch operations through
OpenClaw's authenticated `gateway call tools.invoke` RPC. The OpenClaw CLI
resolves Gateway authentication from OpenClaw's own configuration, so Nólë does
not copy or persist a Gateway token. Do not ask the user for
`FIRECRAWL_API_KEY`. Current stable OpenClaw releases expose `web_fetch` with
keyless Firecrawl fallback but still key-gate Firecrawl search, so Nólë
advertises only extract for that host route and sends search to its existing
fallbacks. Once the
plugin advertises `firecrawl-free`, rerunning setup automatically enables the
full search + extract bridge.

This behavior is intentionally OpenClaw-only. The generic `nole` binary,
`nole-mcp`, and every other client continue to use Nólë's existing direct
Firecrawl API/BYOK adapter. The dedicated wrapper also sources
`~/.config/nole/.env`, so `nole setup --local-extract` can still add local
Scrapling without putting secrets in OpenClaw config.

Sources: [OpenClaw Firecrawl docs](https://docs.openclaw.ai/tools/firecrawl/),
[Gateway CLI](https://docs.openclaw.ai/cli/gateway),
[Gateway tool invocation](https://docs.openclaw.ai/gateway/tools-invoke-http-api), and
[upstream keyless-search change](https://github.com/openclaw/openclaw/commit/8809848b1999).

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole setup --openclaw
./nole doctor --mcp
openclaw mcp show nole --json
```

## 2026-07-17 Host Bridge Verification

A disposable OpenClaw 2026.7.1 runtime and HOME verified the current stable path without touching an existing OpenClaw profile:

- `nole setup --openclaw` installed and pinned the official Firecrawl plugin, enabled it, configured `web_fetch`, registered the dedicated wrapper and selected `fetch-only` because the stable plugin advertised `firecrawl` rather than `firecrawl-free` for search.
- A direct authenticated `tools.invoke web_search` probe confirmed that stable `firecrawl` search still requires `FIRECRAWL_API_KEY`; Nólë therefore does not advertise that search capability or pretend it is keyless.
- A direct `tools.invoke web_fetch` probe returned HTTP 200. The host used its normal `readability` extractor for the tested page; keyless Firecrawl remains OpenClaw's configured fallback.
- The same extraction succeeded end to end through Nólë. The internal route ID remains `firecrawl` for registry/quota compatibility, while metadata preserved `host_tool=openclaw_web_fetch`, `host_provider=web_fetch` and the actual `readability` extractor.
- A docs search skipped Firecrawl with `missing_search_capability`, fell through to DDGS and returned `https://pkg.go.dev/net/http`.
- The generated wrapper contained the scoped client/CLI/mode variables only. No Firecrawl key or Gateway token was copied into the wrapper or MCP registry.

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

- Use `nole setup --openclaw`; do not set `NOLE_CLIENT=openclaw` in the global Nólë env file.
- The saved command must be the absolute `nole-mcp-openclaw` wrapper path.
- If OpenClaw cannot see Nólë after setup, start a fresh agent session so the runtime reloads MCP definitions.
- If the official Firecrawl plugin was just installed or enabled while the Gateway was running, restart the Gateway once so OpenClaw loads it.
- A new OpenClaw CLI device may require the normal OpenClaw scope/pairing approval before `tools.invoke`; do not bypass or auto-approve an unrelated pending request.
- Rerun `nole setup --openclaw` after upgrading OpenClaw. It upgrades the wrapper from fetch-only to full automatically when the plugin advertises `firecrawl-free`. A denied/unavailable host tool fails normally and Nólë continues through its route fallbacks.
- `extract` still has Nólë's built-in `httpfetch` backstop. `nole setup --local-extract` can additionally install local Scrapling; the dedicated OpenClaw wrapper sources the same local env file.
- Run `nole doctor --mcp` directly to separate Nólë stdio issues from OpenClaw runtime issues.
- Inspect OpenClaw logs only after redacting provider keys, Gateway tokens, auth headers, raw provider payloads, private URLs and local paths.
