# Client docs index

Status: generic/unverified index.

Nólë is a free, local web search router for AI agents and coding CLI tools. Client support is documented conservatively: a client is `verified` only after the real client was configured, Nólë MCP tools were visible, one low-limit search worked, and no credentials appeared in config, logs or chat.

## Client support matrix

| Client | Current status | Setup path | Notes |
| --- | --- | --- | --- |
| Claude Code | verified (CLI MCP manager) | `nole setup --claude` (prints `claude mcp add` instructions) or `claude mcp add nole -s user -- /absolute/path/to/nole-mcp` directly | Verified on macOS via `claude mcp list/get`; see `docs/CLIENTS/LIVE-VERIFICATION.md`. `nole setup --claude` no longer writes a stale `~/.claude/mcp.json`; it prints the exact `claude mcp add` invocation the installed Claude Code release reads. |
| Codex CLI | verified (CLI MCP manager) | `nole setup --codex --local-extract` or TOML block | Verified on macOS via `codex mcp list/get`; setup writer correctly sources `~/.config/nole/.env` inline. Add `--local-extract` during install so keyless local extraction is ready before Codex starts a session. With `--mcp-wrapper` the writer emits a wrapper-direct launch line instead. See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| OpenCode | verified (CLI MCP manager) | `nole setup --opencode` (writes `~/.config/opencode/opencode.json`) or `opencode mcp add` | Verified on macOS via `opencode mcp list`. The `nole setup --opencode` writer now targets OpenCode's native path and schema (`{type, command, enabled, environment}`). See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| OpenClaw | verified (OpenClaw Gateway/agent MCP path) | `nole setup --local-extract`, then `openclaw mcp set nole '{"command":"/absolute/path/to/nole-mcp","args":[]}'` | Verified on OpenClaw 2026.5.18 via a fresh Gateway-backed agent turn. OpenClaw loaded the wrapper-direct Nólë MCP entry, exposed `search`, `extract`, `provider_status`, `budget_status`, and dispatched `provider_status` plus one `limit=1` `free-first` DDGS search. See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| Hermes Agent | verified (Hermes Agent MCP profile path) | `nole setup --hermes --local-extract` or equivalent wrapper-direct MCP config | Verified on Hermes Agent v0.14.0 in a 2026-05-20 follow-up run using a disposable cloned profile. Hermes v2026.5.28 / v0.15.0 source compatibility was reviewed on 2026-05-28; its `mcp_servers` config shape remains compatible, and its stricter MCP env filtering makes the wrapper/env-file path the recommended setup. See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| Cursor | verified (GUI MCP path) | `nole setup --cursor --mcp-wrapper /absolute/path/to/nole-mcp` or generic MCP JSON | Verified on macOS in a 2026-05-20 follow-up run: Cursor 3.4.20 GUI MCP client loaded the wrapper-direct entry, and its chat agent dispatched Nólë `provider_status` and `search` by name end-to-end (one `limit=1` `free-first` DDGS result through Cursor). An unrelated existing MCP server in the user's Cursor config was preserved by the writer. See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| Kimi | verified (CLI MCP manager) | `nole setup --kimi` (writes `~/.kimi/mcp.json`) or `kimi mcp add nole -- /absolute/path/to/nole-mcp` | Verified on macOS via `kimi mcp list` and `kimi mcp test nole` (4 tools reported). The `nole setup --kimi` writer is now in-repo and tested; it produces the same `{"mcpServers":{"nole":{...}}}` shape that `kimi mcp add` writes. See `docs/CLIENTS/LIVE-VERIFICATION.md`. |
| Gemini CLI | repo-tested | `nole setup --gemini` (writes `~/.gemini/settings.json`) or `gemini mcp add --scope user nole /absolute/path/to/nole mcp` | 2026-06-04 live run (Gemini CLI 0.40.1): the writer's path + `mcpServers` schema match the client's own `gemini mcp add` output. Stays `repo-tested` because 0.40.1's `gemini mcp list` prints nothing non-interactively (no `mcp test`/`doctor`), so in-client tool visibility was not observable. See `docs/CLIENTS/gemini.md` + `LIVE-VERIFICATION.md`. |
| Grok CLI | repo-tested | `nole setup --grok` (writes `~/.grok/user-settings.json`) | Targets `superagent-ai/grok-cli` (JSON `mcp.servers` array, upsert-by-`id`, repo-tested). 2026-06-04: that CLI was not installed; the host's `grok` was xAI's **"Grok Build TUI"** 0.2.20 — a different product reading `~/.grok/config.toml` (TOML, with `grok mcp add/list/doctor`). Nólë's MCP server connected to it cleanly (`grok mcp doctor`: handshake OK, 6 tools) but via a config Nólë does not write. Writer-target decision tracked in issue #64. See `docs/CLIENTS/grok.md` + `LIVE-VERIFICATION.md`. |
| Generic MCP clients | generic/unverified | command `/absolute/path/to/nole`, args `["mcp"]`, or `/absolute/path/to/nole-mcp` if env-sourcing is desired | Use for clients not listed above. Pass `--mcp-wrapper /absolute/path/to/nole-mcp` to any non-Codex setup writer to point the entry at the wrapper. |

## Status labels

- `verified`: real client tested, config path/schema recorded, tools visible, one low-limit search worked, and no key/auth/header/raw payload leakage was observed. Live evidence is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md`.
- `verified (CLI MCP manager)`: same as `verified`, but the client's first-class MCP manager CLI (e.g. `<client> mcp list/get/test`) is what observed the connected Nólë MCP server and tool list; the verification did not require launching the client's interactive UI.
- `repo-tested`: setup writer or config merge behavior is covered by repo tests, but the real client has not been verified in this environment.
- `generic/unverified`: only a generic MCP command template or future checklist exists.

Do not upgrade a status label without evidence. Evidence should record the client version, config path, sanitized config shape, `nole doctor --mcp` result, visible tools, first low-limit prompt/result, and secret-safety notes.

## Common MCP command

Use an absolute binary path unless you have confirmed the client inherits PATH:

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

For TOML/YAML clients, map the same command/args into the client schema. Keep `nole mcp` on stdio; do not point unverified clients at a hosted proxy unless the user explicitly requested a hosted deployment.

### Optional env-sourcing wrapper

If the client does not inherit the shell environment that owns provider keys (common for GUI clients, gateway/service processes and some agent runtimes), use a local wrapper that sources `~/.config/nole/.env` and execs `nole mcp`. Then point the client's MCP command at the wrapper rather than at the `nole` binary:

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole-mcp",
      "args": []
    }
  }
}
```

A wrapper template is documented in `docs/PROVIDER-KEYS.md` and `docs/AGENT-INSTALL.md`. `nole setup --local-extract` writes the standard wrapper to `~/.local/bin/nole-mcp`; use that before configuring OpenClaw or GUI/service clients. The Codex CLI setup writer already inlines the same env-sourcing pattern in its TOML output, so Codex does not need a separate wrapper unless `--mcp-wrapper` is explicitly used.

Non-Codex setup writers accept `--mcp-wrapper /absolute/path/to/nole-mcp` to emit the wrapper-direct entry instead of the bare-binary command. The Codex writer also accepts the flag and switches to a simpler wrapper-direct launch line (no inline `/bin/sh -lc`). For Hermes v0.15 and newer, prefer the wrapper form for provider keys and local Scrapling because Hermes filters stdio MCP subprocess environments unless variables are explicitly configured. Examples:

```bash
nole setup --opencode --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --kimi     --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --cursor   --mcp-wrapper /absolute/path/to/nole-mcp
nole setup --claude   --mcp-wrapper /absolute/path/to/nole-mcp   # prints the matching claude mcp add command
nole setup --hermes   --local-extract                            # writes Hermes config and standard wrapper
```

## Environment and cost safety

Provider keys should come from the process environment or a local-only wrapper/env file. Nólë commands load `~/.config/nole/.env` automatically without overriding existing process env values. Do not write key values into shared project config.

Recommended local-only env file pattern:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

The file may contain provider variable names such as `BRAVE_API_KEY`, `TAVILY_API_KEY` and `FIRECRAWL_API_KEY` when the user owns those accounts. Do not paste real values into docs, PRs, issues or chat.

Default cost policy is `free-first`. Explicit `cost-capped` or `quality-first` settings can allow premium-capable providers, so client docs must not make absolute “no paid requests” claims.

## Verification checklist for any client

Before calling a client verified, record:

1. Client name and version.
2. OS/environment and whether the client is a CLI, GUI, gateway or service process.
3. Config file path and schema, with secrets omitted.
4. Exact Nólë MCP entry or setup command used.
5. `nole doctor` result summarized without key values.
6. `nole doctor --mcp` result.
7. Tool visibility for `search`, `provider_status`, `budget_status`, and `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity).
8. One low-limit docs search with compact `routing_insight` and result URLs.
9. Confirmation that logs/config/chat contain no provider key values, bearer tokens, auth headers, raw provider payloads, private paths or private URLs.
10. Troubleshooting notes for PATH/env inheritance.

See also `docs/AGENT-INSTALL.md` and `docs/PROVIDER-KEYS.md`.
