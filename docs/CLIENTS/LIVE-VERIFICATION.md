# Live client verification evidence (M11)

Scope: M11 live client verification, plus 2026-05-20 Cursor and OpenClaw follow-up runs.
Run kind: local maintainer run, real clients launched.
Run dates: 2026-05-19 (M11); 2026-05-20 (Cursor follow-up); 2026-05-20 (OpenClaw follow-up).
Host description: macOS arm64 workstation with Go toolchain installed for M11/Cursor, and an Ubuntu x86_64 VPS with OpenClaw installed for the OpenClaw follow-up.
Cost policy: free-first (default; no policy change during the run).
Live provider calls: low-limit keyless smoke searches via DDGS only; each follow-up run records its own single search where applicable.
Provider keys: presence-only via `nole doctor`; key values never printed, logged or committed.
Network required: yes (low-limit smoke searches only).
Secrets required: presence only; values not surfaced.

This document records real-client verification for installable agents/CLIs that could be exercised on the verification hosts. It is intentionally a separate artifact from `docs/INTEGRATION-VERIFICATION.md`, which remains an offline/CI integration evidence document.

## Method

1. Provider keys are read from a local-only `~/.config/nole/.env` file; values are not surfaced. `nole doctor` reports presence only.
2. A local env-sourcing MCP wrapper at `~/.local/bin/nole-mcp` loads `~/.config/nole/.env` and execs `nole mcp`, so client configs do not have to embed key values or shell snippets.
3. For each available client, the MCP server entry is registered with the client's first-class CLI (e.g. `claude mcp add`, `codex mcp` via `nole setup --codex`, `kimi mcp add`) or written directly into the client's documented config file. The MCP command always points at the wrapper or at a wrapper-equivalent env-sourcing shell line.
4. The client's own MCP management surface is then used to confirm the MCP server connects and exposes the four expected tools.
5. For each live-search smoke recorded here, the run uses `limit=1`, `free-first`, and DDGS/keyless routing, then records only the compact `routing_insight` plus result URL.

The verification is conservative. A client is only labeled `verified` when its real MCP client path reported or invoked a connected Nólë MCP server and the four expected tools were observable.

## Versions and binaries

- Nólë built from this branch; `nole doctor --mcp` reports `mcp: ok`, `stdout: startup-clean (0 bytes before protocol input)`, `protocol: initialize/tools/list (… non-json stdout lines: 0)`, `tools: [budget_status extract provider_status search]`.
- Clients available on the verification hosts (installed and exercised): Claude Code, Codex CLI, OpenCode, Kimi (M11 run); Cursor (2026-05-20 follow-up run); OpenClaw 2026.5.18 (2026-05-20 follow-up run).
- Clients absent on the verification hosts (not exercised): Hermes Agent.

## Wrapper used in all client configs

The wrapper sources the local env file and execs `nole mcp`. It is local-only and not committed.

Wrapper shape (placeholder paths only):

```sh
#!/bin/sh
set -a
[ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"
set +a
if [ -n "${NOLE_BIN:-}" ] && [ -x "$NOLE_BIN" ]; then exec "$NOLE_BIN" mcp; fi
if command -v nole >/dev/null 2>&1; then exec nole mcp; fi
if [ -x "$HOME/.local/bin/nole" ]; then exec "$HOME/.local/bin/nole" mcp; fi
echo "nole binary not found. Install nole to PATH or set NOLE_BIN." >&2
exit 127
```

Recommended permissions: `chmod 700 ~/.local/bin/nole-mcp`.

## Shared low-limit smoke search

A single search smoke is recorded at the Nólë binary layer because all MCP clients route to the same Nólë `search` code path. Under `free-first` policy only `ddgs` is policy-allowed, so the smoke incurs no paid spend.

- Query: `Go net/http Client Timeout documentation`
- Task: `docs`
- Limit: `1`
- Compact `routing_insight`: `Nólë: search docs via ddgs (cache miss, free-first, 5/6 attempts, 1 result)`
- Result URLs: `https://pkg.go.dev/net/http`

In addition, a single MCP stdio JSON-RPC round trip was performed against the wrapper (`initialize` → `tools/list` → `tools/call search` with the same query). The wire-level response listed the same four tools and returned the same result URL with the same compact `routing_insight`. No raw provider payloads are recorded.

## Client status

### Claude Code

- Status: verified (CLI MCP manager).
- Client version: a recent Claude Code release. Tested via `claude mcp list` and `claude mcp get nole`.
- Config location actually read by Claude Code in this run: `~/.claude.json` (user-scope `mcpServers`).
- MCP entry registered via official CLI:
  - `claude mcp add nole -s user -- /absolute/path/to/nole-mcp`
- `claude mcp list` reported `nole` connected.
- `claude mcp get nole` confirmed `Scope: User config`, `Type: stdio`, `Status: Connected`, `Command: /absolute/path/to/nole-mcp`.
- MCP tools observed via `nole doctor --mcp` and the JSON-RPC wire round trip against the same wrapper: `search`, `extract`, `provider_status`, `budget_status`.
- Smoke search: recorded in the shared smoke section above.
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed by the client or by Nólë during this verification.
- Known limitation in this run: `nole setup --claude` writes to a Claude config path the installed Claude Code release does not currently read for user-scope MCP servers. The official `claude mcp add` path is what wired Nólë in for this client. See follow-ups below.

### Codex CLI

- Status: verified (CLI MCP manager).
- Client version: a recent Codex CLI release. Tested via `codex mcp list` and `codex mcp get nole`.
- Config location read by Codex CLI in this run: `~/.codex/config.toml`.
- Setup performed via Nólë's writer: `nole setup --codex`.
- Resulting MCP entry shape (sanitized):
  ```toml
  [mcp_servers.nole]
  command = "/bin/sh"
  args = ["-lc", "set -a; [ -f \"$HOME/.config/nole/.env\" ] && . \"$HOME/.config/nole/.env\"; set +a; exec /absolute/path/to/nole mcp"]
  ```
- `codex mcp list` reports `nole` enabled. `codex mcp get nole` shows `transport: stdio` with the env-sourcing shell line.
- MCP tools observed: `search`, `extract`, `provider_status`, `budget_status`.
- Smoke search: recorded in the shared smoke section above.
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed; the TOML entry sources keys at launch but does not contain key values.

### OpenCode

- Status: verified (CLI MCP manager).
- Client version: a recent OpenCode release. Tested via `opencode mcp list`.
- Config location actually read by OpenCode in this run: `~/.config/opencode/opencode.json`.
- MCP entry written directly into the OpenCode config, schema matching other entries:
  ```json
  "nole": {
    "type": "local",
    "command": ["/absolute/path/to/nole-mcp"],
    "enabled": true,
    "environment": {}
  }
  ```
- `opencode mcp list` reports `nole connected`.
- MCP tools observed via `nole doctor --mcp` and the JSON-RPC wire round trip: `search`, `extract`, `provider_status`, `budget_status`.
- Smoke search: recorded in the shared smoke section above.
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths appear in the OpenCode entry; the wrapper sources keys at launch only.
- Known limitation in this run: `nole setup --opencode` writes to `~/opencode.json` with the Claude/Cursor-style `{command, args}` schema, which the installed OpenCode release does not read for MCP servers. Direct config-file write was used in this verification. See follow-ups below.

### Kimi

- Status: verified (CLI MCP manager).
- Client version: a recent Kimi CLI release. Tested via `kimi mcp list` and `kimi mcp test nole`.
- Config location read by Kimi: `~/.kimi/mcp.json` (managed via `kimi mcp add` / `kimi mcp remove`).
- MCP entry registered via official CLI:
  - `kimi mcp add nole -- /absolute/path/to/nole-mcp`
- `kimi mcp list` reports `nole (stdio)`.
- `kimi mcp test nole` reported `Connected` and listed `Available tools: 4`: `budget_status`, `extract`, `provider_status`, `search`.
- Smoke search: recorded in the shared smoke section above; Kimi's `mcp test` already exercises the full connect/list path.
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed by the client. Provider keys come from the wrapper at launch only.

### Cursor (2026-05-20 follow-up run)

- Status: verified (GUI MCP path + chat-agent tool dispatch).
- Client version: 3.4.20 (`Cursor.app` `CFBundleShortVersionString`).
- Config location read by Cursor in this run: `~/.cursor/mcp.json`.
- Setup performed via Nólë's writer:
  - `nole setup --cursor --mcp-wrapper /absolute/path/to/nole-mcp`
- An unrelated MCP server already present in the user's Cursor config was preserved unchanged by the writer; a writer-managed backup was written at `~/.cursor/mcp.json.bak`.
- Resulting Nólë MCP entry shape (sanitized): keys `[args, command]`, `command_basename=nole-mcp`, 0 args, 0 env keys (wrapper-direct launch).
- After a full Cursor restart, Cursor's chat agent successfully dispatched Nólë MCP tools by name end-to-end:
  - `provider_status` returned the same 5-provider table that `nole doctor --mcp` and the CLI `providers --json` produce — brave / ddgs / firecrawl / jina / tavily, each with availability, capabilities, cost class, and a `free-first` policy reason. Under `free-first` only `ddgs` was policy-allowed; the other four providers were correctly reported as `premium_blocked_free_first`.
  - `search` returned a single result through Nólë's `ddgs` provider under the `free-first` policy.
- MCP tools observed: `search`, `extract`, `provider_status`, `budget_status`. Successful dispatch of `provider_status` and `search` by name through Cursor's MCP client proves Cursor loaded the full `tools/list` schema; `extract` and `budget_status` are published by that same `tools/list` (confirmed independently by `nole doctor --mcp`).
- Smoke search through Cursor (sanitized):
  - Query: `Go net/http Client Timeout documentation`
  - Task: `docs`
  - Limit: `1`
  - Provider used: `ddgs`
  - Compact routing insight: cache miss → premium providers `brave`, `firecrawl`, `tavily` skipped under `free-first` policy → `ddgs` returned 1 result.
  - Result URL: `https://pkg.go.dev/net/http`
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or local user paths were printed by Cursor or by Nólë during this verification. The Cursor MCP entry contains no key values; the wrapper sources `~/.config/nole/.env` at launch only.

### OpenClaw (2026-05-20 follow-up run)

- Status: verified (OpenClaw Gateway/agent MCP path).
- Client version: OpenClaw 2026.5.18 (`50a2481`).
- Config surface verified from the installed CLI: `openclaw mcp list`, `openclaw mcp show`, and `openclaw mcp set`.
- Config schema used by OpenClaw: `mcp.servers.nole`.
- Setup command shape:
  - `openclaw mcp set nole '{"command":"/absolute/path/to/nole-mcp","args":[]}'`
- Resulting Nólë MCP entry shape (sanitized): `command_basename=nole-mcp`, 0 args, 0 env keys.
- `nole doctor --mcp` passed before OpenClaw verification: startup stdout clean, `initialize` and `tools/list` ok, tools `[budget_status extract provider_status search]`.
- A fresh Gateway-backed `openclaw agent` turn loaded the Nólë MCP server and dispatched Nólë tools by name:
  - `nole.provider_status` succeeded.
  - `nole.search` succeeded once with `limit=1`.
- MCP tools observed through the OpenClaw runtime: `search`, `extract`, `provider_status`, `budget_status`.
- Smoke search through OpenClaw (sanitized):
  - Query: `Go net/http Client Timeout documentation`
  - Task: `docs`
  - Limit: `1`
  - Provider used: `ddgs`
  - Compact routing insight: free-first route; paid/keyed providers skipped; DDGS keyless-free succeeded.
  - Result URL: `https://pkg.go.dev/net/http`
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or machine-specific absolute paths are recorded. The OpenClaw MCP entry contains no key values; the wrapper sources `~/.config/nole/.env` at launch only.
- Paid spend: none.

## Pending clients on these hosts

These clients were not installed on the verification hosts and remain `generic/unverified` per the matrix in `docs/CLIENTS/README.md`. Each requires its own host-side test before status upgrade:

- Hermes Agent: not installed.

## Findings and follow-ups

Earlier M11 setup-writer follow-ups were addressed in follow-up PRs. Remaining live-client coverage:

1. Add live verification for Hermes Agent on a host where the client is installed.

## Public-safety statement

This document and the underlying run do not include:

- API key values, bearer tokens or Authorization headers;
- raw provider responses or fetched page bodies;
- private URLs;
- machine-specific absolute paths for the verifying user;
- chat transcripts or other personal content.

Placeholder paths used: `~/.config/nole/.env`, `~/.local/bin/nole-mcp`, `/absolute/path/to/nole`, `/absolute/path/to/nole-mcp`.
