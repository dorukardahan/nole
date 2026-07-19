# Antigravity CLI client

Status: repo-tested. The `nole setup --antigravity` writer and its config-merge behavior are covered by repo tests. A local unauthenticated Antigravity CLI 1.1.1 binary was used only for `--version`/`--help` evidence; authenticated tool visibility was not observed here. Keep this status at `repo-tested` until an authenticated Antigravity `/mcp` or `agy -p` smoke confirms Nólë's tools are visible.

Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools. Antigravity CLI is Google's native Go CLI binary (`agy`) and can use local stdio MCP servers by launching `nole mcp` or an env-sourcing wrapper around it.

## What is repo-tested

- `nole setup --antigravity` writes Antigravity's global MCP config at `~/.gemini/config/mcp_config.json`.
- For an existing local `mcpServers.nole`, the writer upserts only the launch-critical `command` and `args` fields. It preserves existing Nólë options (for example `env`, `cwd`, `disabled`, `disabledTools`, and timeouts), sibling servers (including their remote `serverUrl` fields), unknown root keys, file permissions, and `.bak` backups (`internal/cli/setup_antigravity_test.go`).
- If `mcpServers.nole` itself already has a known Antigravity/Gemini remote transport key (`serverUrl`, `serverURL`, `url`, or legacy `httpUrl`), setup stops without writing or backing up the file. Rename or remove that entry explicitly before asking Nólë to configure the same name as local stdio; setup will not create an ambiguous mixed transport or silently discard remote connection fields. Unrelated URL-valued metadata does not trigger this check.
- The Nólë entry is local stdio only: `{ "command": "/absolute/path/to/nole", "args": ["mcp"] }`. No provider credentials are embedded.
- Wrapper mode writes `{ "command": "/absolute/path/to/nole-mcp", "args": [] }` so GUI/service-like launches can source `~/.config/nole/.env` through the wrapper.
- Official Antigravity docs identify `agy` as the native CLI and document MCP configuration/verification flows. See `https://antigravity.google/docs/cli/install`, `https://antigravity.google/docs/cli/gcli-migration`, and `https://antigravity.google/docs/mcp`.

## What is NOT verified here

- Authenticated Antigravity tool visibility was not tested. Do not claim Nólë is `verified` for Antigravity until `/mcp` inside Antigravity or an authenticated `agy -p` smoke shows Nólë's tools.
- Remote MCP (`serverUrl`) is not written by Nólë's setup command. This writer configures the local stdio entry only.
- Workspace-scoped `.agents/mcp_config.json` is not written by `nole setup --antigravity`; the built-in writer targets the global config path.

## Install Antigravity CLI

Follow the official installer for your platform from:

```text
https://antigravity.google/docs/cli/install
```

Then verify the CLI is present:

```bash
agy --version
agy --help
```

Antigravity is the current consumer successor for Gemini CLI users. Keep using `nole setup --gemini` only when you intentionally use the Gemini CLI Standard/Enterprise/Cloud/paid API-key path.

## Setup Nólë

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

Then run the Antigravity writer:

```bash
nole setup --antigravity
```

If Antigravity does not inherit the shell environment that holds provider keys, point it at the env-sourcing wrapper instead:

```bash
nole setup --local-extract                 # writes ~/.local/bin/nole-mcp (chmod 700)
nole setup --antigravity --mcp-wrapper /absolute/path/to/nole-mcp
```

## Manual config shape

Antigravity's global MCP config path:

```text
~/.gemini/config/mcp_config.json
```

Local stdio Nólë entry:

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

Remote MCP entries may use `serverUrl`/`serverURL` or Gemini migration forms `url`/`httpUrl`, but Nólë's writer does not create a remote entry. It preserves remote sibling servers while upserting the local `nole` server. If the existing `nole` entry itself has one of those transport keys, setup refuses the conversion and leaves the file unchanged.

Workspace-scoped Antigravity MCP config can also be represented at:

```text
.agents/mcp_config.json
```

Nólë's built-in setup command intentionally writes only the global user config.

## Verification checklist (to upgrade to `verified`)

On an authenticated Antigravity install, record all of the following before changing the status label:

- `nole doctor` passes with key presence only;
- `nole doctor --mcp` passes;
- Antigravity `/mcp` or an authenticated `agy -p` smoke shows `nole` connected with `search`, `provider_status`, `budget_status`, `extract`, `search_and_extract`, and `research` as available tools;
- one low-limit docs search works through Antigravity;
- config/log/chat evidence contains no provider key values, auth tokens, auth headers, raw provider payloads, private paths, or private URLs.

Record the evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` before marking Antigravity `verified`.

## Troubleshooting

- If tools do not appear, run `nole doctor --mcp` directly, then restart the Antigravity session.
- If provider keys are missing inside Antigravity, use `nole setup --local-extract` and re-run `nole setup --antigravity --mcp-wrapper /absolute/path/to/nole-mcp`.
- Keep `nole mcp` on stdio. Do not point Antigravity at a hosted proxy unless the user explicitly requested a hosted deployment.
