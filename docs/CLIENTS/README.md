# Client docs index

Status: generic/unverified index.

Nólë is a free, local web search router for AI agents and coding CLI tools. Client support is documented conservatively: a client is `verified` only after the real client was configured, Nólë MCP tools were visible, one low-limit search worked, and no credentials appeared in config, logs or chat.

## Client support matrix

| Client | Current status | Setup path | Notes |
| --- | --- | --- | --- |
| Claude Code | repo-tested | `nole setup --claude` or manual MCP JSON | Config merge/upsert covered by repo tests; live Claude Code verification pending. |
| Codex CLI | repo-tested | `nole setup --codex` or TOML block | Setup sources `~/.config/nole/.env` if present; live Codex CLI tool visibility pending. |
| OpenCode | repo-tested | `nole setup --opencode` or manual JSON | Config merge path covered; live OpenCode verification pending. |
| OpenClaw | generic/unverified | generic MCP stdio template | Keep generic until real client config path/schema and tool visibility are recorded. |
| Hermes Agent | generic/unverified | generic MCP stdio template | Priority target, but no verified Hermes config test is recorded in this repo yet. |
| Cursor | repo-tested | `nole setup --cursor` where available, otherwise generic MCP JSON | Setup flag/shared MCP JSON merge helper coverage exists; live Cursor verification pending. |
| Kimi | generic/unverified | generic MCP stdio template if client supports MCP | No repo setup writer or live verification yet. |
| Generic MCP clients | generic/unverified | command `/absolute/path/to/nole`, args `["mcp"]` | Use for clients not listed above. |

## Status labels

- `verified`: real client tested, config path/schema recorded, tools visible, one low-limit search worked, and no key/auth/header/raw payload leakage was observed.
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

## Environment and cost safety

Provider keys should come from the process environment or a local-only wrapper/env file. Do not write key values into shared project config.

Recommended local-only env file pattern:

```bash
mkdir -p ~/.config/nole
chmod 700 ~/.config/nole
$EDITOR ~/.config/nole/.env
chmod 600 ~/.config/nole/.env
```

The file may contain provider variable names such as `BRAVE_API_KEY`, `TAVILY_API_KEY`, `JINA_API_KEY` and `FIRECRAWL_API_KEY` when the user owns those accounts. Do not paste real values into docs, PRs, issues or chat.

Default cost policy is `free-first`. Explicit `cost-capped` or `quality-first` settings can allow premium-capable providers, so client docs must not make absolute “no paid requests” claims.

## Verification checklist for any client

Before calling a client verified, record:

1. Client name and version.
2. OS/environment and whether the client is a CLI, GUI, gateway or service process.
3. Config file path and schema, with secrets omitted.
4. Exact Nólë MCP entry or setup command used.
5. `nole doctor` result summarized without key values.
6. `nole doctor --mcp` result.
7. Tool visibility for `search`, `extract`, `provider_status`, `budget_status`.
8. One low-limit docs search with compact `routing_insight` and result URLs.
9. Confirmation that logs/config/chat contain no provider key values, bearer tokens, auth headers, raw provider payloads, private paths or private URLs.
10. Troubleshooting notes for PATH/env inheritance.

See also `docs/AGENT-INSTALL.md` and `docs/PROVIDER-KEYS.md`.
