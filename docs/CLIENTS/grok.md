# Grok CLI client

Status: repo-tested. The `nole setup --grok` writer and its config-merge behavior are covered by repo tests; the targeted `superagent-ai/grok-cli` was **not** installed on the 2026-06-04 verification host, so its live tool visibility has not been observed. (That host had a *different* product — xAI's "Grok Build TUI" — see the note below; Nólë's MCP server connected to it cleanly, but via a config Nólë does not write.) Do not upgrade to `verified` without the live evidence described below.

Nólë is a free, local web search router for AI agents and coding CLI tools. This page documents **`superagent-ai/grok-cli`** (`@vibe-kit/grok-cli` / `grok-dev`), which reads MCP servers from `~/.grok/user-settings.json` and is what `nole setup --grok` writes. It can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it). At the time the writer was pinned, `superagent-ai/grok-cli` did not expose a dedicated `grok mcp add` command, so servers are added via Nólë's writer, the in-TUI `/mcps` command, or by editing the JSON.

> **Two different `grok` CLIs.** There is also xAI's **"Grok Build TUI"** (a distinct Rust product, e.g. `grok 0.2.20`), which reads `~/.grok/config.toml` (`[mcp_servers.<name>]`, TOML — **not** `user-settings.json`) and **does** have a full `grok mcp add/list/remove/doctor` manager. `nole setup --grok` does **not** write that TOML file, so its output is ignored by the Grok Build TUI (`grok mcp list` → `No MCP servers configured`). Nólë's MCP server itself works fine with the Grok Build TUI when configured via its own `grok mcp add` (verified 2026-06-04: `grok mcp doctor` reported handshake OK + 6 tools discovered). Whether to add a `config.toml` writer for the Grok Build TUI is tracked in **issue #64**. To wire Nólë into the Grok Build TUI today, run `grok mcp add nole --command /absolute/path/to/nole --args mcp` (or point it at the env-sourcing wrapper).

## What is repo-tested

- `nole setup --grok` writes `~/.grok/user-settings.json` with a top-level `mcp` object whose `servers` is an **array** of server objects keyed by an `id` field. The writer upserts the `id == "nole"` element in place (preserving unknown fields on it and all other servers) or appends a new one, and preserves unknown root keys (`internal/cli/setup_grok_test.go`).
- The array structure, `id`/`label`/`enabled`/`transport`/`command`/`args` fields, and config path are pinned from primary source: `src/utils/settings.ts:92-106` (`McpServerConfig` / `McpSettings.servers: McpServerConfig[]`), `:185-186` (`~/.grok/user-settings.json`), `:651` (`saveMcpServers` writes `{ mcp: { servers } }`). See `docs/RESEARCH-FINDINGS.md §1`.
- Backups (`.bak`), preserved permissions, idempotency, in-place upsert, and append-when-absent are covered by tests.

## What is NOT verified here

- `superagent-ai/grok-cli` was not installed on the 2026-06-04 host, so its in-TUI `/mcps` view showing Nólë tools connected was not observed. Status stays `repo-tested` until that live check is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`. (The xAI "Grok Build TUI" that *was* present is a different product Nólë's `--grok` writer does not target; its clean `grok mcp doctor` handshake + 6-tool discovery is recorded in `LIVE-VERIFICATION.md`, and the writer-target gap is tracked in issue #64.)

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
- Grok's `/mcps` view shows `nole` connected with `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability);
- one low-limit docs search works through Grok;
- no key/auth/header/raw-payload/private-path leakage appears in config, logs or chat.

Record the evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` before changing the status label here or in `docs/CLIENTS/README.md`.

## Troubleshooting

- If `nole` tools do not appear, run `nole doctor --mcp` directly to confirm Nólë's MCP stdio is healthy, then restart the Grok session so it re-reads `~/.grok/user-settings.json`.
- If an existing `nole` entry is present but disabled, the writer preserves your `enabled` flag — set `enabled: true` in `~/.grok/user-settings.json` (or via `/mcps`) to turn it on.
- `extract` works out of the box (keyless, no JavaScript) via the `httpfetch` backstop. For higher-fidelity / JS-rendered extraction, run `nole setup --local-extract` (or set a Tavily/Firecrawl key) and restart Grok.
- If provider keys are missing inside Grok but present in your interactive shell, prefer the wrapper form (`--mcp-wrapper`) so keys are sourced at launch.
- If stdout pollution appears in Grok's MCP logs, file a bug; `nole mcp` stdout must remain JSON-RPC protocol only.
