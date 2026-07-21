# Live client verification evidence (M11)

Scope: M11 live client verification, plus 2026-05-20 Cursor, OpenClaw and Hermes Agent follow-up runs, a 2026-05-28 OpenClaw compatibility re-check and Hermes v2026.5.28 source compatibility review, a 2026-06-04 Gemini CLI + Grok CLI follow-up run, and a 2026-07-21 Hermes Agent v0.19 real-client re-check.
Run kind: local maintainer run, real clients launched.
Run dates: 2026-05-19 (M11); 2026-05-20 (Cursor follow-up); 2026-05-20 (OpenClaw follow-up); 2026-05-20 (Hermes Agent follow-up); 2026-05-28 (OpenClaw 2026.5.27 compatibility re-check); 2026-05-28 (Hermes Agent v2026.5.28 source compatibility review); 2026-06-04 (Gemini CLI 0.40.1 + Grok CLI follow-up run); 2026-07-21 (Hermes Agent v0.19 real-client re-check).
Host description: macOS arm64 workstation with Go toolchain installed for M11/Cursor, and Ubuntu x86_64 VPS hosts with OpenClaw or Hermes Agent installed for the follow-up runs.
Cost policy: free-first (default; no policy change during the run).
Live provider calls: low-limit keyless smoke searches via DDGS only; each follow-up run records its own single search where applicable.
Provider keys: presence-only via `nole doctor`; key values never printed, logged or committed.
Network required: yes (low-limit smoke searches only).
Secrets required: presence only; values not surfaced.

This document records real-client verification for installable agents/CLIs that could be exercised on the verification hosts. It is intentionally a separate artifact from `docs/INTEGRATION-VERIFICATION.md`, which remains an offline/CI integration evidence document.

## Method

1. Provider keys are read from a local-only `~/.config/nole/.env` file; values are not surfaced. `nole doctor` reports presence only.
2. A local env-sourcing MCP wrapper at `~/.local/bin/nole-mcp` loads `~/.config/nole/.env` and execs `nole mcp`, so client configs do not have to embed key values or shell snippets.
3. For each available client, the MCP server entry is registered with the client's first-class CLI (e.g. `claude mcp add`, `codex mcp` via `nole setup --codex`, `kimi mcp add`, `hermes mcp add`) or written directly into the client's documented config file. The MCP command points at the wrapper, at a wrapper-equivalent env-sourcing shell line, or at a direct absolute Nólë binary path when the verified client/profile already owns the safe launch environment.
4. The client's own MCP management surface is then used to confirm the MCP server connects and exposes the tool surface expected for that Nólë version (four tools in the early M11 receipts; six in current releases).
5. For each live-search smoke recorded here, the run uses `limit=1`, `free-first`, and DDGS/keyless routing, then records only the compact `routing_insight` plus result URL.

The verification is conservative. A client is only labeled `verified` when its real MCP client path reported or invoked a connected Nólë MCP server and that version's expected tools were observable.

## Versions and binaries

- For the 2026-05-19 M11 run, Nólë was built from the then-current M11 branch; `nole doctor --mcp` reported `mcp: ok`, `stdout: startup-clean (0 bytes before protocol input)`, `protocol: initialize/tools/list (… non-json stdout lines: 0)`, `tools: [budget_status extract provider_status search]`.
- Clients available on the verification hosts (installed and exercised): Claude Code, Codex CLI, OpenCode, Kimi (M11 run); Cursor (2026-05-20 follow-up run); OpenClaw 2026.5.18 (2026-05-20 follow-up run); Hermes Agent v0.14.0 (2026-05-20 follow-up run); OpenClaw 2026.5.27 (2026-05-28 compatibility re-check); Hermes Agent v0.19.0 / v2026.7.20 (2026-07-21 real-client re-check).
- Clients absent on the verification hosts (not exercised): generic MCP clients beyond the named clients above.

## Wrapper and launch patterns used by verified clients

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

A shared search smoke is recorded at the Nólë binary/wrapper layer because the wrapper-based MCP clients route to the same Nólë `search` code path. The shared smoke ran in no-key/free-first conditions; keyed providers were not used, and DDGS was the keyless fallback, so the smoke incurred no paid spend. Follow-up client runs that performed their own live search record those details in the client-specific sections below.

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
  - `provider_status` returned the same 4-provider table that `nole doctor --mcp` and the CLI `providers --json` produce — brave / ddgs / firecrawl / tavily, each with availability, capabilities, cost class, and a `free-first` policy reason. For that Cursor smoke, DDGS was the only route allowed by the configured `free-first` policy; the other three providers were correctly reported as `premium_blocked_free_first`.
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

### OpenClaw (2026-05-28 compatibility re-check)

- Status: compatible (OpenClaw wrapper-backed MCP registry path).
- Client version: OpenClaw 2026.5.27 (`27ae826`).
- Config schema used by OpenClaw: `mcp.servers.nole`.
- MCP entry shape: server name `nole`, wrapper command `nole-mcp`, 0 args, no provider key values in OpenClaw config.
- `nole doctor --mcp` passed with `NOLE_MCP_SMOKE_BINARY` pointed at the OpenClaw wrapper: startup stdout clean, `initialize` and `tools/list` ok, tools `[budget_status extract provider_status search]`.
- Provider surface through the wrapper: Brave, DDGS, Firecrawl, Scrapling and Tavily available; local Scrapling reported Python package `0.4.8`.
- Cost policy: `free-first`; paid spend: none.
- Secret-safety: no key values, bearer tokens, auth headers, raw provider payloads, private URLs or machine-specific absolute paths are recorded. Provider secrets remained in the local wrapper/env file, not in OpenClaw config.

### Hermes Agent (2026-05-20 follow-up run)

- Status: verified (Hermes Agent MCP profile path + chat-agent tool dispatch).
- Client version: Hermes Agent v0.14.0.
- Config surface verified from the installed CLI: `hermes profile create`, `hermes -p <temporary-profile> mcp add`, `hermes -p <temporary-profile> mcp list`, `hermes -p <temporary-profile> mcp test nole`, and a fresh `hermes -p <temporary-profile> chat` turn.
- Verification profile: disposable cloned Hermes profile; its gateway was stopped. The active default gateway profile was not modified and continued to show no configured MCP servers.
- Setup command shape:
  - `hermes -p <temporary-profile> mcp add nole --command /absolute/path/to/nole --args mcp`
- Resulting Nólë MCP entry shape (sanitized): direct absolute Nólë binary path with args `["mcp"]`; no key values embedded in Hermes config.
- `hermes -p <temporary-profile> mcp test nole` connected over stdio and discovered 4 tools.
- A fresh Hermes Agent chat turn loaded the Nólë MCP server and dispatched Nólë tools by name:
  - `mcp_nole_provider_status` succeeded.
  - `mcp_nole_search` succeeded once with `limit=1`.
- MCP tools observed through the Hermes Agent runtime: `search`, `extract`, `provider_status`, `budget_status` (exposed to Hermes as `mcp_nole_search`, `mcp_nole_extract`, `mcp_nole_provider_status`, `mcp_nole_budget_status`).
- Smoke search through Hermes Agent (sanitized):
  - Query: `Go net/http Client Timeout documentation`
  - Task: `docs`
  - Limit: `1`
  - Provider used: `ddgs`
  - Compact routing insight: `Nólë: search docs via ddgs (free-first, 4/5 attempts, 1 result)`
  - Route interpretation: Brave, Firecrawl and Tavily were unavailable to the Nólë MCP subprocess as disabled/no-key providers; DDGS was the keyless-free fallback under `free-first`. This is not a claim that DDGS is the benchmark-primary docs provider.
  - Result URL: `https://pkg.go.dev/net/http`
- Secret-safety: no provider key values, bearer tokens, auth headers, raw provider payloads, private URLs, machine-specific absolute paths or chat transcripts are recorded. Local runtime logs/transcripts were not committed.
- Paid spend: none.

### Hermes Agent v2026.5.28 source compatibility review

- Status: source-compatible review only; not a live client verification.
- Reviewed upstream release: Hermes Agent v2026.5.28 / v0.15.0, published 2026-05-28.
- Relevant upstream MCP surfaces: `mcp_servers` remains the config root; stdio servers still use `command` and `args`; `timeout`, `connect_timeout`, `tools.resources`, `tools.prompts`, `enabled` and `supports_parallel_tool_calls` are documented config keys.
- Nólë setup impact: `nole setup --hermes` remains compatible and now writes `tools.resources=false` and `tools.prompts=false` for new Nólë entries so Hermes does not add resource/prompt utility wrappers to Nólë's web-search tool surface.
- Environment impact: Hermes v0.15 documents stdio MCP subprocess credential filtering. Prefer `nole setup --hermes --local-extract` or `--mcp-wrapper` so Nólë gets `~/.config/nole/.env` through the env-sourcing wrapper instead of putting provider key values in Hermes config.
- Historical outcome: a v0.15 live-client run was not performed in this 2026-05-28 review. The 2026-07-21 v0.19 real-client receipt below supersedes that evidence gap for current support.
- Secret-safety: this review records only public source/config shapes and no provider key values, bearer tokens, auth headers, raw provider payloads, private URLs, private paths or chat transcripts.

### Hermes Agent v0.19.0 real-client re-check (2026-07-21)

- Status: verified (real Hermes MCP client path).
- Exact client version read-back: Hermes Agent v0.19.0 (v2026.7.20).
- Exact installed Nólë version line read back on the verification host: `nole v1.7.0`. This shell-selected binary check is recorded as compatibility context, not as proof of the image held by an already-running stdio child: that release did not expose its version through the callable `provider_status` result.
- `hermes mcp test nole` connected successfully and exposed six native Nólë tools: `search`, `extract`, `search_and_extract`, `research`, `provider_status`, and `budget_status`.
- Read-only client-path dispatch succeeded for `provider_status`, `budget_status`, and sanitized public-document search/extract calls.
- No configuration was written and no binary was replaced. The run did not invoke `/reload-mcp`, start a fresh cutover session, restart a gateway/service, or make any production change.
- Reconnect semantics: a stdio child keeps the binary image it started with. After a future binary or config change, Hermes `/reload-mcp` closes the old connection/child, rereads config, reconnects, and refreshes the active tool snapshot; a fresh session is a fallback. A file replacement alone does not update an already-running child.
- Drift observability: the post-v1.9.0 code adds an additive MCP-only `provider_status.server_version`, sourced from the same build version as the initialize handshake. Once such a build is installed and the MCP subprocess reconnects, the agent can identify the loaded server directly; unstamped source builds report `dev`. This verification did not perform that installation or reconnect.
- Secret-safety: the receipt contains no provider key values, bearer tokens, auth headers, raw provider payloads, private URLs, local absolute paths, profile names, or chat transcripts.

### Gemini CLI (2026-06-04 follow-up run)

- Status: **repo-tested (not upgraded)** — real CLI launched and the setup-writer output confirmed against the client's own MCP manager, but in-client tool visibility was **not observable non-interactively** in this CLI version.
- Client version: Gemini CLI `0.40.1` (`google-gemini/gemini-cli`).
- Run isolation: an isolated throwaway `HOME` was used so the maintainer's real `~/.gemini` was never modified; the temp profile was deleted after the run.
- Writer parity (confirmed live): `nole setup --gemini` writes `~/.gemini/settings.json` with a top-level `mcpServers` object keyed by name. Running the client's **own** `gemini mcp add --scope user <name> …` against the same profile wrote into the **same file with the same `mcpServers` object shape** (both the Nólë-written `nole` entry and the client-written probe entry coexisted in one valid file). So the writer's path + schema are correct against the installed Gemini CLI.
- Why not upgraded to `verified`: in `0.40.1`, `gemini mcp list` prints **nothing** to stdout or stderr (exit 0) even for the client's own freshly-added server, and there is no `gemini mcp test`/`doctor` connectivity probe. So the client enumerating Nólë's tools could not be observed without a model turn (which needs Gemini auth + would consume the user's quota). Tool visibility was therefore not observed *in-client*. Nólë's advertised tool set is independently confirmed below (`nole doctor --mcp` + the Grok `mcp doctor` handshake): 6 tools — `search`, `research`, `provider_status`, `budget_status`, `extract`, `search_and_extract`.
- Smoke search: not run through Gemini (a tool-dispatch turn needs model auth; not exercised to avoid using the user's credentials/quota).
- Secret-safety: the written `~/.gemini/settings.json` `nole` entry contains only `command` + `args:["mcp"]` — no key values, tokens, or headers. No secrets were printed by the client or Nólë.

### Grok CLI (2026-06-04 follow-up run)

- Status: **repo-tested (not upgraded)** for the documented `superagent-ai/grok-cli`; that client was **not installed** on this host. The `grok` that *is* installed is a **different product** (see below), which Nólë's MCP server connected to cleanly.
- Important finding — two distinct `grok` CLIs (tracked in issue **#64**):
  - `nole setup --grok` targets **`superagent-ai/grok-cli`** (`@vibe-kit/grok-cli`), which reads `~/.grok/user-settings.json` as `{ "mcp": { "servers": [ {id,…} ] } }` (a JSON array). That CLI was not present here, so the documented client stays `repo-tested`.
  - The installed `grok` is **xAI's "Grok Build TUI" `0.2.20`** (`~/.grok/bin/grok`), which reads **`~/.grok/config.toml`** (`[mcp_servers.<name>]`, TOML) and has a full MCP manager: `grok mcp add/list/remove/doctor`. It does **not** read the JSON file Nólë's `--grok` writer produces — `grok mcp list` reported `No MCP servers configured` after `nole setup --grok`.
- Nólë MCP works with the installed xAI Grok Build TUI (via its own config): configured through `grok mcp add nole --command /absolute/path/to/nole --args mcp` (writes `~/.grok/config.toml`), `grok mcp doctor nole --json` reported all checks passing — `command found`, `server started`, `handshake OK (protocol 2025-06-18)`, **`6 tools discovered`**, `healthy: true`. Its config-source scan also reads `~/.claude.json` and project `.mcp.json`.
- Run isolation: an isolated throwaway `HOME` was used; the maintainer's real `~/.grok` was never modified; the temp profile was deleted after the run.
- Why the documented client is not upgraded: the `superagent-ai/grok-cli` Nólë's writer targets is not installed here, so its in-client tool visibility was not observed. The installed xAI tool is a different product Nólë does not currently write config for (issue #64 tracks whether to add a `config.toml` writer).
- Smoke search: not run through Grok (a tool-dispatch turn needs Grok login — `grok mcp doctor` reported `grok.com: not logged in` for the throwaway profile — and would consume the user's account; the `mcp doctor` handshake + 6-tool discovery is the recorded in-client evidence for the installed tool).
- Secret-safety: the written `~/.grok/config.toml` `nole` entry contains only `command` + `args=["mcp"]` + `enabled=true` — no key values, tokens, or headers. No secrets were printed.

### Grok Build TUI — `nole setup --grok-build` writer verified (2026-06-04, v1.6.0)

- Status: **verified (CLI MCP manager)** for xAI's Grok Build TUI. Following issue #64, v1.6.0 adds `nole setup --grok-build`, which writes the TOML `~/.grok/config.toml` the Grok Build TUI reads (distinct from `--grok`, which writes the superagent JSON). This entry verifies the WRITER's output (not the TUI's own `grok mcp add`).
- Method: in an isolated throwaway `HOME`, `nole setup --grok-build` wrote `~/.grok/config.toml` with a flat `[mcp_servers.nole]` table (`command`, `args = ["mcp"]`, `enabled = true`). Then `grok mcp doctor nole --json` (Grok Build TUI `0.2.20`) read that file (`source ~/.grok/config.toml → found, server_count 1`) and reported every check passing: `command found`, `server started`, `handshake OK (protocol 2025-06-18)`, **`6 tools discovered`**, `healthy: true`.
- Smoke search: not run through Grok (a tool-dispatch turn needs Grok login; the `mcp doctor` handshake + 6-tool discovery is the recorded in-client evidence, mirroring the `kimi mcp test` path).
- Secret-safety: the writer's `config.toml` entry carries only `command` + `args` + `enabled` — no key values. The maintainer's real `~/.grok` was untouched (isolated `HOME`, deleted after the run).

### Nólë MCP tool surface confirmed in this run

`nole doctor --mcp` on the released v1.5.0 binary reported `protocol: initialize/tools/list` and `tools: [budget_status extract provider_status research search search_and_extract]` (6 tools) — matching the "6 tools discovered" the Grok `mcp doctor` handshake reported. This is the surface expanded from the 4 tools recorded in the M11 run, after the v0.6.0 (`research`, `search_and_extract`) and v1.3.0 (always-on keyless `extract`) additions. The same `nole doctor --mcp` also listed the keyless `arxiv` search provider added in v1.5.0 (`[search, status]`, `keyless-free`).

## Pending clients on these hosts

These clients remain `generic/unverified` per the matrix in `docs/CLIENTS/README.md`. Each requires its own host-side test before status upgrade:

- Generic MCP clients beyond the named clients above.

## Findings and follow-ups

Earlier M11 setup-writer follow-ups were addressed in follow-up PRs. Priority named-client live coverage is now recorded for Claude Code, Codex CLI, OpenCode, Kimi, Cursor, OpenClaw and Hermes Agent. Generic MCP clients remain template-only until a specific client/runtime is named and tested.

The 2026-06-04 Gemini CLI + Grok CLI run launched both real CLIs but did not upgrade either to `verified`: Gemini `0.40.1` confirmed Nólë's writer matches the client's own `gemini mcp add` output (same path + schema) but offers no non-interactive way to observe in-client tool visibility (`gemini mcp list` is silent; no `mcp test`/`doctor`); and the installed `grok` turned out to be xAI's "Grok Build TUI" `0.2.20` (TOML `~/.grok/config.toml`), a different product from the `superagent-ai/grok-cli` that `nole setup --grok` targets (JSON `~/.grok/user-settings.json`). Nólë's MCP server connected cleanly to the installed Grok Build TUI (`grok mcp doctor`: handshake OK, 6 tools), but via a config Nólë does not write. Follow-up: issue **#64** tracks whether to add a `config.toml` writer for xAI's Grok Build TUI.

## Public-safety statement

This document and the underlying run do not include:

- API key values, bearer tokens or Authorization headers;
- raw provider responses or fetched page bodies;
- private URLs;
- machine-specific absolute paths for the verifying user;
- chat transcripts or other personal content.

Placeholder paths used: `~/.config/nole/.env`, `~/.local/bin/nole-mcp`, `/absolute/path/to/nole`, `/absolute/path/to/nole-mcp`.
