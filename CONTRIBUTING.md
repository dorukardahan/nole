# Contributing to Nólë

Thanks for your interest in Nólë. This guide covers the build/test loop, the
local audit gate that mirrors CI, and what to include in pull requests.

## Prerequisites

- Go 1.25+ (the project pins `go 1.25.10` in `go.mod`; older toolchains will
  fail to build).
- Optional provider API keys for `Brave`, `Tavily`, `Firecrawl`. None are
  required to build, test or run the deterministic offline benchmark; `DDGS`
  is a keyless search fallback.
- For live client/integration smokes only: `gh` CLI for the GitHub side and
  any client (Claude Code, Codex, OpenCode, Kimi, Cursor, OpenClaw, Hermes)
  you want to verify against.

## Build and test loop

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

Quick smokes for the CLI surface:

```bash
./nole providers --json
./nole bench --json
./nole classify "OpenAI API docs pricing and latest changelog" --json
./nole route-plan "OpenAI API docs pricing and latest changelog" --json
./nole search "Go net/http Client Timeout documentation" --task docs --json
```

`./nole extract <url>` requires a Tavily or Firecrawl key (the MCP server
will not register the `extract` tool when neither is configured — that is
intentional, see `docs/PROVIDER-KEYS.md`).

## Local audit gate (matches CI)

Before opening a PR, run the same gate that CI runs:

```bash
./scripts/audit.sh
```

This wraps:

- `gofmt -l` (must be empty)
- `./scripts/check-docs-framing.sh`
- `./scripts/check-benchmark-claims.sh`
- `./scripts/check-integration-evidence.sh`
- `go test ./...`
- `go vet ./...`
- `go run . doctor` / `doctor --mcp`
- `go run . bench --json` / `bench --evidence-md`
- `go run . providers --json`
- A subprocess integration-evidence smoke (`scripts/verify-integration-evidence.sh`)

Public-safety secret scan and cross-platform build/checksum jobs run in CI on
top of this. To preview them locally:

```bash
./scripts/secret-scan.sh
./scripts/check-release-builds.sh
```

A vulnerability scan via [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln)
is recommended on any change that touches networking, TLS, parsing, or
provider HTTP code:

```bash
GOBIN=/tmp go install golang.org/x/vuln/cmd/govulncheck@v1.3.0
/tmp/govulncheck ./...
```

Stdlib findings should be zero on a current toolchain.

## Pull request expectations

- Small focused changes. One PR per concern. If a change touches more than
  three packages, consider splitting unless the diff is genuinely coupled
  (e.g. interface change rippling outward).
- All CI gates green. `private-prep gates` (`verify`, `public-safety`,
  `cross-platform-build`) must pass before review.
- No secrets in diffs. The repo's `scripts/secret-scan.sh` is strict; do not
  commit `.env` files, real API keys, auth headers, private hostnames, or
  absolute home paths (anything under `/Users/USER/` or `/home/user/`).
- No marketing or superlative claims about providers, routing, benchmarks, or
  speed. The bench claims guard (`internal/bench/claims_guard_test.go`)
  rejects them in CI.
- Test new code paths. Provider adapters in `internal/providers/<name>/`
  follow the pattern: typed request/response structs, central
  `providerhttp.DoWithRetry`, `providerhttp.NewHTTPStatusError` for non-200
  redaction. Match the pattern; add unit tests next to the file.
- Keep MCP stdout protocol-clean. All logs go to stderr; stdout is reserved
  for the JSON-RPC stream. The MCP error wrappers in
  `internal/mcpserver/errors.go` are the source of truth.

## Filing an issue

Use the issue templates under `.github/ISSUE_TEMPLATE/`. Include:

- nole version (`./nole --version` or commit hash)
- OS + Go version
- Whether the issue reproduces on the deterministic offline bench or only
  on live providers
- For provider issues: provider name, redacted error message, route trace
  (`--json` output)

Do not paste real API keys, authorization headers, or live provider response
bodies. Nólë's own error redaction strips them; copy-pasting around it
defeats the purpose.

## Security disclosures

See [`SECURITY.md`](SECURITY.md). For sensitive disclosures, prefer the
GitHub private vulnerability reporting flow over public issues.

## License

By contributing to Nólë you agree that your contributions will be licensed
under the Apache License 2.0 (`LICENSE`).
