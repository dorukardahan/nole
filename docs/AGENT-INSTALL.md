# Agent install guide

This guide is written for humans and AI agents that need to install Nólë on a local machine or VPS and connect it to an agent or coding CLI.

Nólë is a free, local web search router for AI agents and coding CLI tools. It should run where the user's agent runs, or on a VPS the user controls. It is not a hosted SaaS by default.

## Install path overview

1. Clone the repository.
2. Build the `nole` binary.
3. Put the binary on PATH or use an absolute path in MCP config.
4. Set provider keys in the local environment if the user has them.
5. Run `nole doctor` and `nole doctor --mcp`.
6. Configure the chosen client to launch `nole mcp`.
7. Verify MCP tool visibility.
8. Run a first low-risk search.

## Local machine install

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go vet ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

Install to a user-local PATH directory:

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
nole doctor
```

Use the absolute binary path in agent configs if the agent does not inherit PATH.

## VPS install

On the VPS:

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
~/.local/bin/nole doctor
~/.local/bin/nole doctor --mcp
```

Use a VPS only if that is where the agent runs or where the user explicitly wants Nólë. Do not create a public search proxy unless the user explicitly asks for hosted deployment.

## Provider keys

Nólë works best with user-owned provider keys but can still use DDGS as a keyless fallback.

Environment variables:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export JINA_API_KEY="..."
export FIRECRAWL_API_KEY="..."
```

Rules:

- Do not paste real keys into chat or docs.
- Do not commit `.env` files.
- Do not print key values while debugging.
- Prefer provider dashboards with free-tier limits or overage disabled where available.
- Default `NOLE_COST_POLICY` is `free-first`: a key alone is `premium-capable` and remains blocked from live calls unless the user explicitly chooses `cost-capped` with local estimates or `quality-first`.

A local env file can be useful for GUI apps that do not inherit shell env:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

The file should contain shell-compatible `KEY=value` lines. Codex setup currently launches through `/bin/sh -lc` and sources this file before `nole mcp`; generic clients may need a wrapper script if they do not load shell env.

See `docs/PROVIDER-KEYS.md`.

## MCP configuration template

Use an absolute binary path:

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

If the client supports environment variables in config, reference variable names but do not write secret values into shared configs. Prefer process environment or a local wrapper.

## Built-in setup command

Nólë has setup writers for several clients:

```bash
nole setup --claude
nole setup --codex
nole setup --opencode
nole setup --all
```

Current setup command also includes Cursor and Windsurf flags. For any client, read the generated config before declaring success, and verify tool visibility inside the real client when possible.

## Verification commands

Run this after install and after client setup:

```bash
nole doctor
nole doctor --mcp
nole providers --json
nole bench --json
nole bench --evidence-md
nole classify "OpenAI API docs pricing and latest changelog" --json
nole route-plan "OpenAI API docs pricing and latest changelog" --json
```

Optional low-risk smoke:

```bash
nole search "Go net/http Client Timeout documentation" --task docs --json
nole extract "https://go.dev/doc/" --json
```

`nole bench --json` and `nole bench --evidence-md` are deterministic/offline. Search/extract may call live providers only after the cost policy allows a provider; keep usage low and cost-aware. Search, extract, classify and route-plan JSON include compact `routing_insight` by default; search, extract and route-plan keep detailed `route_trace` for debugging where available. Runtime traces include sanitized cost policy/class fields such as `free-first`, `keyless-free` or `premium-capable`, never provider keys. Use `--insight off` to omit the user-facing insight, or `--insight verbose` when you intentionally need trace lines in human search/extract output.

## First agent prompt

After configuring the client, ask the agent:

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite the result URLs.
```

The agent should report the compact insight, not the full route trace, unless the user asks for debugging detail. The compact insight and error envelopes are sanitized and must not contain provider keys, auth headers, raw provider payloads or private URLs.

The agent should see MCP tools similar to:

- `search`
- `extract`
- `provider_status`
- `budget_status`

## Troubleshooting

### Go missing

Install Go 1.23+ or use a user-local Go toolchain. Do not commit the toolchain into the repo.

### PATH issues

GUI apps often do not inherit shell PATH. Use an absolute path in MCP config:

```json
{"command":"/absolute/path/to/nole","args":["mcp"]}
```

### Provider key not seen

Run:

```bash
nole doctor
```

Doctor should show key presence only, never the value. If a GUI app cannot see env vars, use a wrapper script or local `.env` file sourced by the launcher.

### MCP stdout pollution

`nole mcp` must write only JSON-RPC messages to stdout. Logs and debug output belong on stderr. Use:

```bash
nole doctor --mcp
```

### Existing config overwritten

Setup writers should merge existing config and create backups. If a config was changed, inspect the backup next to the config file. Do not remove unknown existing servers.

### Paid usage risk

If a provider dashboard has overage controls, disable overage or set a hard limit before using live calls. If unsure, run deterministic commands only: `doctor`, `providers`, `bench --json`, `bench --evidence-md`.

## Verification status labels

Use these labels in client docs:

- `verified`: real client tested, MCP tools visible, no secret leaks.
- `repo-tested`: setup writer/config merge covered by repo tests, but real client not yet tested.
- `generic/unverified`: documented MCP template only.

Do not upgrade a status label without evidence.
