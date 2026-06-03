# Gemini CLI client

Status: repo-tested. The `nole setup --gemini` writer and its config-merge behavior are covered by repo tests; the real Gemini CLI was **not** launched in this environment, so live tool visibility has not been observed. Do not upgrade to `verified` without the live evidence described below.

Nólë is a free, local web search router for AI agents and coding CLI tools. Gemini CLI (`@google/gemini-cli`, repo `google-gemini/gemini-cli`) can use Nólë through MCP stdio by launching `nole mcp` (or an env-sourcing wrapper around it). Gemini CLI has a first-class MCP manager (`gemini mcp add/list/remove/enable/disable`), and it also reads MCP servers from `~/.gemini/settings.json`.

## What is repo-tested

- `nole setup --gemini` writes `~/.gemini/settings.json` with a top-level `mcpServers` object keyed by server name, merging only the `nole` entry and preserving unknown root keys and sibling MCP servers (`internal/cli/setup_gemini_test.go`).
- The entry shape, config path, and object-keyed (not array) structure are pinned from primary source: `packages/cli/src/config/settingsSchema.ts` (`mcpServers` is an object, shallow-merged), `packages/cli/src/commands/mcp/add.ts` (`mcpServers[name] = newServer`), and `packages/core/src/config/storage.ts` + `utils/paths.ts` (`~/.gemini/settings.json`). See `docs/RESEARCH-FINDINGS.md §1`.
- Backups (`.bak`), preserved permissions, and idempotency are covered by the shared JSON-writer tests (`internal/cli/setup_doctor_test.go`, `setup_writers_test.go`).

## What is NOT verified here

- The real Gemini CLI was not installed/launched, so `/mcp list` showing Nólë tools as `Connected` was not observed. Status stays `repo-tested` until that live check is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.

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

Then either run Nólë's built-in writer:

```bash
nole setup --gemini
```

…or register the entry through Gemini's MCP manager directly:

```bash
gemini mcp add --scope user nole /absolute/path/to/nole mcp
```

If Gemini CLI does not inherit the shell environment that holds your provider keys, point the entry at an env-sourcing wrapper instead:

```bash
nole setup --local-extract                 # writes ~/.local/bin/nole-mcp (chmod 700)
nole setup --gemini --mcp-wrapper /absolute/path/to/nole-mcp
```

Then verify in Gemini:

```bash
gemini mcp list        # expect nole listed
# inside the Gemini CLI: /mcp        (lists connected servers and their tools)
```

## Manual config shape

Gemini CLI stores MCP servers under `~/.gemini/settings.json` as a top-level `mcpServers` object keyed by server name:

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

In wrapper mode the entry becomes `{ "command": "/absolute/path/to/nole-mcp", "args": [] }`. Do not place provider key values into shared config; the wrapper sources `~/.config/nole/.env` only at launch.

## Verification checklist (to upgrade to `verified`)

Mark this client `verified` only after, on a host with Gemini CLI installed:

- `nole doctor` passes with key presence only;
- `nole doctor --mcp` passes;
- Gemini's `/mcp` (or `gemini mcp list`) shows `nole` connected with `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability);
- one low-limit docs search works through Gemini;
- no key/auth/header/raw-payload/private-path leakage appears in config, logs or chat.

Record the evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` before changing the status label here or in `docs/CLIENTS/README.md`.

## Troubleshooting

- If `nole` tools do not appear, run `nole doctor --mcp` directly to confirm Nólë's MCP stdio is healthy and key presence is detected, then restart the Gemini session.
- `extract` works out of the box (keyless, no JavaScript) via the `httpfetch` backstop. For higher-fidelity / JS-rendered extraction, run `nole setup --local-extract` (or set a Tavily/Firecrawl key) and restart Gemini.
- If provider keys are missing inside Gemini but present in your interactive shell, prefer the wrapper form (`--mcp-wrapper`) so keys are sourced at launch.
- If stdout pollution appears in Gemini's MCP logs, file a bug; `nole mcp` stdout must remain JSON-RPC protocol only.
