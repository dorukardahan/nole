# AGENTS.md — installing and contributing to Nólë

Nólë is a free, local web search router for AI agents and coding CLI tools. If a user gives you this repository link and asks you to install Nólë, your goal is to build a local binary, configure the user's chosen agent to launch `nole mcp`, verify tool visibility, and never expose credentials.

## Non-negotiable safety rules

- Never print API keys, bearer tokens, auth headers, private provider payloads or private URLs.
- Only report credential presence/absence.
- Keep Nólë local/BYOK. Do not create a hosted proxy or SaaS deployment unless the user explicitly asks.
- Do not enable hidden paid usage. Default stance is free-tier/BYOK-safe.
- Preserve existing client config files. Merge unknown fields, keep backups and do not widen file permissions.
- MCP stdout must be JSON-RPC protocol only. Startup messages and logs go to stderr.
- Do not claim an agent integration is verified unless you actually tested the client config and tool visibility.

## Build from source

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go vet ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

If `go` is missing, install Go 1.23+ using the user's normal package manager or a user-local toolchain. Do not commit downloaded toolchains or build artifacts.

## Install binary

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
nole doctor
```

On a VPS, use the same steps over SSH. Keep the binary on that VPS; do not route traffic through a hosted service unless the user explicitly requests deployment.

## Provider keys

Nólë reads provider keys from environment variables:

```bash
export BRAVE_API_KEY="..."          # or BRAVE_SEARCH_API_KEY
export TAVILY_API_KEY="..."
export JINA_API_KEY="..."
export FIRECRAWL_API_KEY="..."
```

Do not ask the user to paste real keys into chat. Tell the user to create keys in provider dashboards and set them locally. If a GUI app does not inherit shell env, use a local env file such as `~/.config/nole/.env` with mode `0600` and configure the client launcher to source it.

For details and overage cautions, read `docs/PROVIDER-KEYS.md`.

## Verify core commands

Run these before declaring success:

```bash
nole doctor
nole doctor --mcp
nole providers --json
nole bench --json
nole bench --evidence-md
nole classify "OpenAI API docs pricing and latest changelog" --json
nole route-plan "OpenAI API docs pricing and latest changelog" --json
nole search "Go net/http Client Timeout documentation" --task docs --json
nole extract "https://go.dev/doc/" --json
```

Search, extract, classify and route-plan JSON responses should include a compact `routing_insight` by default; search, extract and route-plan preserve full `route_trace` for debugging where available. In normal user answers, include at most the compact Nólë insight and result URLs; do not dump full traces unless the user is troubleshooting. If live provider calls could incur cost or use quota, keep them low-limit and explicit. `nole bench --json` and `nole bench --evidence-md` are deterministic/offline and safe.

## Configure clients

When available, use built-in setup writers:

```bash
nole setup --claude
nole setup --codex
nole setup --opencode
```

Generic MCP command:

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

Client docs:

- `docs/CLIENTS/claude-code.md`
- `docs/CLIENTS/codex.md`
- `docs/CLIENTS/opencode.md`
- `docs/CLIENTS/openclaw.md`
- `docs/CLIENTS/hermes.md`
- `docs/CLIENTS/cursor.md`
- `docs/CLIENTS/kimi.md`
- `docs/CLIENTS/generic-mcp.md`

## MCP smoke test expectations

`nole doctor --mcp` should confirm:

- startup stdout is clean before protocol input;
- JSON-RPC initialize succeeds;
- `tools/list` succeeds;
- tools include `search`, `extract`, `provider_status`, `budget_status`;
- stderr does not leak secrets.

## Contribution workflow

Use small PRs. For behavior changes, write tests first and watch them fail before implementing.

Recommended local gate:

```bash
gofmt -w $(git diff --name-only -- '*.go')
go test ./...
go vet ./...
go run . doctor
go run . doctor --mcp
go run . bench --json
go run . bench --evidence-md
git diff --check
./scripts/check-docs-framing.sh
./scripts/check-benchmark-claims.sh
```

For each PR:

1. Branch from `main`.
2. Keep the route matrix unchanged unless sanitized evidence supports a change.
3. Run the relevant verification gate.
4. Scan for secrets in changed files.
5. Push, open a PR, and request review.
6. Fix review feedback before merging.

## Product framing to preserve

Primary one-liner:

Nólë is a free, local web search router for AI agents and coding CLI tools.

Use this framing in user-facing docs. MCP is an entrypoint, not the whole product. Nólë improves an existing agent's web-search layer; it does not replace the agent.
