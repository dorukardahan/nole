# Grok CLI client

Status: repo-tested. The `nole setup --grok` writer and its config-merge behavior are covered by repo tests; the real Grok CLI was **not** launched in this environment, so live tool visibility has not been observed. Do not upgrade to `verified` without the live evidence described below.

Nólë is a free, local web search router for AI agents and coding CLI tools. Grok CLI (`@vibe-kit/grok-cli` / `grok-dev`, repo `superagent-ai/grok-cli`) can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it). Grok CLI reads MCP servers from `~/.grok/user-settings.json`; it does not expose a dedicated `grok mcp add` command, so servers are added via Nólë's writer, the in-TUI `/mcps` command, or by editing the JSON.

## What is repo-tested

- `nole setup --grok` writes `~/.grok/user-settings.json` with a top-level `mcp` object whose `servers` is an **array** of server objects keyed by an `id` field. The writer upserts the `id == "nole"` element in place (preserving unknown fields on it and all other servers) or appends a new one, and preserves unknown root keys (`internal/cli/setup_grok_test.go`).
- The array structure, `id`/`label`/`enabled`/`transport`/`command`/`args` fields, and config path are pinned from primary source: `src/utils/settings.ts:92-106` (`McpServerConfig` / `McpSettings.servers: McpServerConfig[]`), `:185-186` (`~/.grok/user-settings.json`), `:651` (`saveMcpServers` writes `{ mcp: { servers } }`). See `docs/RESEARCH-FINDINGS.md §1`.
- Backups (`.bak`), preserved permissions, idempotency, in-place upsert, and append-when-absent are covered by tests.

## What is NOT verified here

- The real Grok CLI was not installed/launched, so its in-TUI `/mcps` view showing Nólë tools connected was not observed. Status stays `repo-tested` until that live check is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.

## Setup

Build and install Nólë first:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole doctor --mcp
```

Then run Nólë's built-in writer:

```bash
nole setup --grok
```

If Grok CLI does not inherit the shell environment that holds your provider keys, point the entry at an env-sourcing wrapper instead:

```bash
nole setup --local-extract                 # writes ~/.local/bin/nole-mcp (chmod 700)
nole setup --grok --mcp-wrapper /absolute/path/to/nole-mcp
```

Then verify inside Grok CLI with the `/mcps` command (lists configured MCP servers and their tools).

## Manual config shape

Grok CLI stores MCP servers under `~/.grok/user-settings.json` as a top-level `mcp` object whose `servers` is an array; each entry is keyed by its `id` field:

```json
{
  "mcp": {
    "servers": [
      {
        "id": "nole",
        "label": "nole",
        "enabled": true,
        "transport": "stdio",
        "command": "/absolute/path/to/nole",
        "args": ["mcp"]
      }
    ]
  }
}
```

In wrapper mode the entry becomes `"command": "/absolute/path/to/nole-mcp", "args": []`. Do not place provider key values into shared config; the wrapper sources `~/.config/nole/.env` only at launch.

## Verification checklist (to upgrade to `verified`)

Mark this client `verified` only after, on a host with Grok CLI installed:

- `nole doctor` passes with key presence only;
- `nole doctor --mcp` passes;
- Grok's `/mcps` view shows `nole` connected with `search`, `provider_status`, `budget_status`, plus `extract` when Tavily, Firecrawl or local Scrapling is configured;
- one low-limit docs search works through Grok;
- no key/auth/header/raw-payload/private-path leakage appears in config, logs or chat.

Record the evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` before changing the status label here or in `docs/CLIENTS/README.md`.

## Troubleshooting

- If `nole` tools do not appear, run `nole doctor --mcp` directly to confirm Nólë's MCP stdio is healthy, then restart the Grok session so it re-reads `~/.grok/user-settings.json`.
- If an existing `nole` entry is present but disabled, the writer preserves your `enabled` flag — set `enabled: true` in `~/.grok/user-settings.json` (or via `/mcps`) to turn it on.
- If `extract` is missing and no Tavily/Firecrawl key is configured, run `nole setup --local-extract` and restart Grok.
- If provider keys are missing inside Grok but present in your interactive shell, prefer the wrapper form (`--mcp-wrapper`) so keys are sourced at launch.
- If stdout pollution appears in Grok's MCP logs, file a bug; `nole mcp` stdout must remain JSON-RPC protocol only.
