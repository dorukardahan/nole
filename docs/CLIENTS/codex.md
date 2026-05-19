# Codex CLI client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a free, local web search router for AI agents and coding CLI tools. Codex CLI can use Nólë through MCP stdio by launching `nole mcp`. Codex is the easiest target because its setup writer already inlines `~/.config/nole/.env` sourcing in the generated TOML.

## What is verified

- Real Codex CLI release was exercised on macOS (M11 live verification).
- `nole setup --codex` produced a working `[mcp_servers.nole]` block in `~/.codex/config.toml`.
- `codex mcp list` reports `nole` enabled; `codex mcp get nole` reports `transport: stdio` with the env-sourcing shell line.
- Tools observable through the same wrapper command path: `search`, `extract`, `provider_status`, `budget_status`.
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback.
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths appear in the TOML entry; the entry sources `~/.config/nole/.env` at launch only.

## What is also tested in this repo

- Nólë has a Codex TOML setup writer.
- Tests cover preserving existing config while replacing/upserting `[mcp_servers.nole]`.
- The default generated command launches through `/bin/sh -lc`, sources `~/.config/nole/.env` if present, then executes `nole mcp`.
- When `--mcp-wrapper /absolute/path/to/nole-mcp` is passed, the writer emits a simpler `command = "<wrapper>"`, `args = []` entry and defers env sourcing to the wrapper.
- `nole doctor --mcp` verifies Nólë's MCP stdio behavior.

## Setup

Build and install Nólë:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
```

Optionally create a local env file for provider keys:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

Run:

```bash
nole setup --codex
nole doctor --mcp
```

## Manual config shape

The setup writer manages this style of TOML block:

```toml
[mcp_servers.nole]
command = "/bin/sh"
args = ["-lc", "set -a; [ -f \"$HOME/.config/nole/.env\" ] && . \"$HOME/.config/nole/.env\"; set +a; exec /absolute/path/to/nole mcp"]
```

Do not place secret values directly in the TOML file.

## Verification checklist

Mark this client `verified` only after:

- `nole doctor` passes;
- `nole doctor --mcp` passes;
- Codex CLI lists or can call Nólë MCP tools `search`, `extract`, `provider_status`, `budget_status`;
- a small docs search works;
- no credentials appear in config, logs or chat.

Suggested first prompt:

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite result URLs.
```

## Troubleshooting

- If tools are missing, restart Codex CLI after setup.
- If keys are missing, inspect `~/.config/nole/.env` permissions and syntax without printing values.
- If existing Codex config changed unexpectedly, compare with the backup created by Nólë setup.
- If MCP fails, run `nole doctor --mcp` directly.
