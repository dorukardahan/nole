# Agent install guide

This guide is written for humans and AI agents that need to install Nólë on a local machine or VPS and connect it to an agent or coding CLI.

Nólë is a free, local web search router for AI agents and coding CLI tools. It should run where the user's agent runs, or on a VPS the user controls. It is not a hosted SaaS by default.

## Install path overview

1. Clone the repository.
2. Build the `nole` binary.
3. Put the binary on PATH or use an absolute path in MCP config.
4. Run `nole setup --local-extract` when Python 3.10+ is available, so keyless local URL extraction is ready without extra user work.
5. Set provider keys in the local environment if the user has them.
6. Run `nole doctor` and `nole doctor --mcp`.
7. Configure the chosen client to launch `nole mcp` or the generated `nole-mcp` wrapper.
8. Verify MCP tool visibility.
9. Run a first low-risk search.

## M8 agent handoff checklist

When an AI agent receives only the GitHub link and a user request to install Nólë, complete this checklist before declaring success:

1. Confirm the target environment: local machine, SSH/VPS, container, GUI app, gateway or service process.
2. Build from source and run `go test ./...`, `go vet ./...`, `./nole doctor` and `./nole doctor --mcp`.
3. Install the binary to a user-local PATH directory or capture the absolute binary path for MCP config.
4. Run `nole setup --local-extract` after the binary is installed. This creates `~/.local/share/nole/scrapling-venv`, installs `scrapling[fetchers]`, writes `~/.config/nole/.env` and generates `~/.local/bin/nole-mcp`. If Python 3.10+ is missing, install Python through the user's normal package manager before declaring local extraction ready.
5. Configure provider key variable names only if the user already owns the matching provider accounts; never ask the user to paste real values into chat.
6. Keep default `NOLE_COST_POLICY=free-first` unless the user explicitly accepts premium-capable provider risk.
7. Configure the selected client with the setup writer and include `--local-extract` when possible, for example `nole setup --codex --local-extract` or `nole setup --hermes --local-extract`. For OpenClaw, use `nole setup --openclaw`: it installs/enables the official Firecrawl plugin, selects full or fetch-only host delegation based on the plugin's advertised providers, writes a dedicated `nole-mcp-openclaw` wrapper, and registers the MCP entry. Do not ask an OpenClaw user for `FIRECRAWL_API_KEY`.
8. Verify the client sees `search`, `research`, `provider_status`, `budget_status`, `extract` and `search_and_extract` — advertised out of the box via the keyless httpfetch backstop (zero keys); a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity rather than being required.
9. Run one low-limit docs search and include only the compact `routing_insight` plus result URLs in the user-facing answer.
10. Record unresolved client/env limitations truthfully; do not upgrade a client status label without real-client evidence.
11. Scan changed configs/log snippets for key values, bearer tokens, auth headers, raw provider payloads, private paths and private URLs before sharing.

## Tool decision recipe

Use the smallest Nólë tool that gives the agent enough evidence. Nólë returns sources and routing context; the calling agent still decides, synthesizes and writes the final answer.

- Use `search` when you need a source/result list for one intent: docs lookup, current news, pricing/status pages, people/company lookup, code/community search, or quick fact discovery. Prefer an explicit `task` when you know it (`docs`, `news`, `pricing`, `academic`, `factcheck`, `code`, `social`, `people`, `semantic`, `general`).
- Use `search_and_extract` when you need the top search hit(s) read immediately. It is the right first move for “find the docs and quote the relevant page” or “search then summarize this top source.” Partial URL extraction failures are non-fatal and appear in `extract_errors` with sanitized `routing_insight`.
- Use `extract` when the user already gave a URL and you only need that page read. It works with zero keys through the keyless `httpfetch` backstop; Scrapling, Tavily or Firecrawl can improve fidelity when configured.
- Use `research` when the question needs several sources across one or more intents, deduped evidence, and per-step observability. `research` is for source discovery and evidence collection, not answer synthesis, ranking claims, or final prose.

Practical default: start with `search` for a single clear lookup, `search_and_extract` for “find + read the top result,” and `research` only when the answer would otherwise need multiple searches or source cross-checking. If `research` returns `evidence_steps`, use it as a compact receipt for what was found, skipped or failed; do not treat it as a quality score.

## Agent copy/paste install block

Use this block for a local install from a GitHub link. It avoids provider keys and live paid-provider calls:

```bash
set -euo pipefail
NOLE_SRC="${NOLE_SRC:-$HOME/src/nole}"
NOLE_BIN_DIR="${NOLE_BIN_DIR:-$HOME/.local/bin}"
NOLE_BIN="$NOLE_BIN_DIR/nole"

git clone https://github.com/dorukardahan/nole.git "$NOLE_SRC"
cd "$NOLE_SRC"
go test ./...
go vet ./...
go build -o nole .
mkdir -p "$NOLE_BIN_DIR"
cp ./nole "$NOLE_BIN"
"$NOLE_BIN" setup --local-extract
"$NOLE_BIN" doctor
"$NOLE_BIN" doctor --mcp
```

After this succeeds, use `"$NOLE_BIN"` as the MCP command path if the client inherits the env file, or use `$HOME/.local/bin/nole-mcp` as the MCP command path for GUI/service clients that need the generated env-sourcing wrapper.

To install a prebuilt release binary instead of building from source, `scripts/install.sh` detects OS/arch, downloads the matching `nole-<os>-<arch>` asset, verifies its `SHA256SUMS` checksum before installing (fails closed on mismatch), and installs to `~/.local/bin`. Since v0.10.0 the installer also runs an **additive, optional** build-provenance check: when the GitHub CLI (`gh >= 2.93.0`) is present it verifies the release's keyless Sigstore attestation against the exact release-workflow identity and fails closed on a real mismatch; when `gh` is absent it soft-skips so the zero-dependency path is unaffected. Set `NOLE_INSTALL_VERIFY=require` to make attestation verification mandatory (supply-chain-strict), or `off` to skip it (SHA256 stays mandatory regardless). Prefer download-then-run over pipe-to-bash. After install, `nole doctor --check-updates` prints a fail-soft notice when a newer release exists (silent offline; never fails `doctor`), and `nole self-update` applies it — same verification contract as the installer (mandatory SHA256 + additive `gh attestation verify`; `--verify require` to make the attestation mandatory, `--check-only` to preview). Nólë works with ZERO keys via keyless DDGS search, so an install with no provider keys is fully functional.

## PATH and absolute binary discovery

Prefer a user-local binary directory:

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole doctor
```

For GUI clients, service managers and remote agents, assume PATH may differ from the interactive shell. Capture the absolute path and put that path in MCP config:

```bash
NOLE_BIN="$(command -v nole || true)"
if [ -z "$NOLE_BIN" ]; then
  NOLE_BIN="$HOME/.local/bin/nole"
fi
printf 'Use this MCP command path: %s\n' "$NOLE_BIN"
```

Do not publish machine-specific absolute paths in shared docs or PRs; replace them with `/absolute/path/to/nole` when documenting examples.

## Cost-aware environment template

Use this template locally. It intentionally uses variable names and policy controls, not real values:

```bash
# Optional provider keys. Set only in your local shell, service env or local-only env file.
# export BRAVE_API_KEY="set-locally"
# export TAVILY_API_KEY="set-locally"
# export FIRECRAWL_API_KEY="set-locally"

# Default no-hidden-paid-spend mode.
export NOLE_COST_POLICY="free-first"

# Optional persistent local accounting and in-process cache for long-running MCP sessions.
export NOLE_QUOTA_LEDGER_PATH="$HOME/.local/state/nole/quota-ledger.json"
export NOLE_CACHE_TTL="5m"

# Optional diagnostic logging to stderr (never stdout). text (default) | json | off.
export NOLE_LOG="text"
```

The quota ledger is file-backed by default at `$XDG_STATE_HOME/nole/quota-ledger.json` (or `~/.local/state/nole/quota-ledger.json`). Set `NOLE_QUOTA_LEDGER_PATH` to override the path, or to `memory`/`off`/`none` to opt into per-restart reset. `NOLE_CACHE_TTL_SECONDS=300` is also accepted. Explicit `cost-capped` or `quality-first` settings can allow premium-capable providers, so do not promise absolute no-paid behavior when those policies are selected.

`NOLE_LOG` controls Nólë's structured diagnostic logging. It always writes to **stderr only**, so it never pollutes the MCP JSON-RPC stream on stdout; values and errors are redacted, so logs never carry a provider key. For machine inspection without spending quota, `nole config dump --json` and `nole doctor --json` print the effective config and health as JSON (secrets shown as set/unset only).

For keyless local URL extraction, do not ask the user to hand-create `NOLE_SCRAPLING_PYTHON`. Run:

```bash
nole setup --local-extract
```

This writes `NOLE_SCRAPLING_PYTHON` into `~/.config/nole/.env` and creates the env-sourcing wrapper.

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
nole setup --local-extract
nole doctor --mcp
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
~/.local/bin/nole setup --local-extract
~/.local/bin/nole doctor --mcp
```

Use a VPS only if that is where the agent runs or where the user explicitly wants Nólë. Do not create a public search proxy unless the user explicitly asks for hosted deployment.

## Provider keys

Nólë works best with user-owned provider keys but can still use DDGS as a keyless fallback.

Environment variables:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export FIRECRAWL_API_KEY="..."
```

Rules:

- Do not paste real keys into chat or docs.
- Do not commit `.env` files.
- Do not print key values while debugging.
- Prefer provider dashboards with free-tier limits or overage disabled where available.
- Default `NOLE_COST_POLICY` is `free-first`: a key alone is treated as `free-tier-BYOK` and tracked against the local monthly quota. A provider becomes `premium-capable` only when the user explicitly sets `NOLE_<PROVIDER>_PAID=1`.

A local env file can be useful for GUI apps that do not inherit shell env:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

The file should contain shell-compatible `KEY=value` lines. `nole setup --local-extract` writes `NOLE_SCRAPLING_PYTHON` there automatically. Nólë commands load this file without overriding existing process env values. Codex setup currently launches through `/bin/sh -lc` and sources this file before `nole mcp`; generic clients may need a wrapper script if they do not load shell env.

See `docs/PROVIDER-KEYS.md`.

## Optional env-sourcing MCP wrapper

Several MCP clients launch the configured `command` without inheriting the user's interactive shell environment (this is common for GUI apps, gateway/service processes and some agent runtimes). For those clients, register the MCP server through a small local wrapper that sources `~/.config/nole/.env` and execs `nole mcp`. The wrapper is local-only; do not commit it.

Preferred path:

```bash
nole setup --local-extract
```

That command writes this wrapper to `~/.local/bin/nole-mcp`. The manual template below is only for recovery or custom paths.

```bash
mkdir -p ~/.local/bin
cat > ~/.local/bin/nole-mcp <<'SH'
#!/bin/sh
set -a
if [ -f "$HOME/.config/nole/.env" ]; then
  . "$HOME/.config/nole/.env"
fi
set +a

if [ -n "${NOLE_BIN:-}" ] && [ -x "$NOLE_BIN" ]; then
  exec "$NOLE_BIN" mcp
fi
if command -v nole >/dev/null 2>&1; then
  exec nole mcp
fi
if [ -x "$HOME/.local/bin/nole" ]; then
  exec "$HOME/.local/bin/nole" mcp
fi

echo "nole binary not found. Install nole to PATH or set NOLE_BIN." >&2
exit 127
SH
chmod 700 ~/.local/bin/nole-mcp
```

Then point the client's MCP command at `/absolute/path/to/nole-mcp` with empty args. The Codex setup writer already inlines an equivalent env-sourcing shell line, so Codex does not need a separate wrapper.

This pattern keeps provider keys out of every per-client config file and ensures `nole mcp` always launches with the same set of keys regardless of how the client itself is started.

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
nole setup --claude     # prints `claude mcp add` instructions; no file is written
nole setup --codex      # writes ~/.codex/config.toml with env-sourcing launch line
nole setup --opencode   # writes ~/.config/opencode/opencode.json (native schema)
nole setup --kimi       # writes ~/.kimi/mcp.json
nole setup --cursor     # writes ~/.cursor/mcp.json
nole setup --windsurf   # writes ~/.codeium/windsurf/mcp_config.json
nole setup --hermes     # writes ~/.hermes/config.yaml
nole setup --antigravity # writes ~/.gemini/config/mcp_config.json for Antigravity CLI (agy)
nole setup --gemini      # writes ~/.gemini/settings.json for Gemini CLI Standard/Enterprise/Cloud/paid API-key users
nole setup --local-extract          # prepares local Scrapling and ~/.local/bin/nole-mcp
nole setup --codex --local-extract  # does both in one command
nole setup --hermes --local-extract # does both in one command
nole setup --all        # all client writers above; add --local-extract to prepare Scrapling too
```

When `--local-extract` is present and no custom wrapper is provided, setup writes and registers `~/.local/bin/nole-mcp` automatically. Non-Codex writers (and the Codex writer when the flag is given) also accept `--mcp-wrapper /absolute/path/to/nole-mcp` to register a custom env-sourcing wrapper instead of the bare `nole mcp` binary. Use the wrapper form when the client launches without inheriting your interactive shell environment. This is the recommended Hermes v0.15+ path because Hermes filters stdio MCP subprocess environments unless variables are explicitly configured:

```bash
nole setup --opencode --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --kimi     --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --antigravity --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --cursor   --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --claude   --mcp-wrapper /absolute/path/to/nole-mcp   # prints the matching claude mcp add
```

For any client, read the generated config before declaring success, and verify tool visibility inside the real client when possible. The Codex writer is the only one that inlines `~/.config/nole/.env` sourcing in its default output; with `--mcp-wrapper` it also defers env sourcing to the wrapper. See `docs/CLIENTS/LIVE-VERIFICATION.md` and `docs/NEXT-STEPS.md` for current per-client status.

For Hermes, the setup writer also sets `tools.resources=false` and
`tools.prompts=false` on new Nólë entries. That keeps Hermes' MCP utility-tool
wrappers out of Nólë's search/extract surface unless the user deliberately
changes the policy.

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
- `provider_status`
- `budget_status`
- `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity)

## Troubleshooting

### Go missing

Install Go 1.25+ or use a user-local Go toolchain. Do not commit the toolchain into the repo.

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

### Scrapling/local extract not active

Run:

```bash
nole setup --local-extract
nole doctor --mcp
```

If Python 3.10+ is missing, install Python through the user's normal package manager and rerun the setup command. Do not tell the user to set `NOLE_SCRAPLING_PYTHON` by hand unless the automatic setup cannot be used in that environment.

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
