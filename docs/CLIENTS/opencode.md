# OpenCode client

Status: verified (CLI MCP manager). Live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenCode can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it).

## What is verified

- Real OpenCode release was exercised on macOS (M11 live verification).
- A `nole` MCP entry written directly into `~/.config/opencode/opencode.json` with the OpenCode-native schema is read by `opencode mcp list` and reports `nole connected`.
- Tools observable through the same wrapper command path: `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability).
- One low-limit docs smoke search succeeded under `free-first` policy via the keyless DDGS fallback.
- No key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths appear in the OpenCode entry; provider keys are loaded only by the wrapper at launch.

## What is also tested in this repo

- `nole setup --opencode` now writes to `~/.config/opencode/opencode.json` using OpenCode's native schema: `{type: "local", command: [<bin>, "mcp"], enabled: true, environment: {}}` (or `{type: "local", command: [<wrapper>], enabled: true, environment: {}}` when `--mcp-wrapper` is given).
- Repo tests cover preserving unknown root keys and unrelated MCP entries, idempotent re-runs, replacing stale `nole` entries (no leftover `environment` secrets from older runs), and wrapper-mode output shape.
- `nole doctor --mcp` verifies Nólë's own MCP stdio behavior.

## Setup

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole setup --local-extract
nole doctor --mcp
```

Recommended path against the installed OpenCode release:

```bash
# Bare-binary form (OpenCode inherits your shell env / you do not need an env file):
nole setup --opencode
# Wrapper form (recommended when OpenCode launches without your shell env):
nole setup --opencode --mcp-wrapper "$HOME/.local/bin/nole-mcp"
```

Either invocation upserts the `nole` entry into `~/.config/opencode/opencode.json` using OpenCode's native schema:

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

After running setup (or editing the file by hand), run:

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
- OpenCode sees `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability);
- a small docs search works;
- no secret values are present in config or logs.

## Troubleshooting

- Restart OpenCode after config changes.
- Use absolute binary paths if PATH is not inherited.
- `extract` works out of the box (keyless, no JavaScript) via the `httpfetch` backstop. For higher-fidelity / JS-rendered extraction, run `nole setup --local-extract` (or set a Tavily/Firecrawl key) and restart OpenCode.
- Check provider key visibility with `nole doctor`.
- Use the stdio MCP path above; the HTTP/REST surface (`nole serve`) is for shared/remote setups (one keyed Nólë serving several machines), not needed for a local OpenCode. If you do expose it on a non-loopback bind, set `NOLE_SERVE_TOKEN`.
