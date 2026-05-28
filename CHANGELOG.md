# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- OpenClaw client docs now record the 2026-05-28 compatibility re-check on
  OpenClaw 2026.5.27 with the wrapper-backed MCP registry and local Scrapling
  available through the wrapper.

## [0.2.2] - 2026-05-26

### Changed

- `nole setup --local-extract` now auto-detects stable versioned Python
  interpreters (`python3.13`, `python3.12`, `python3.11`, `python3.10`)
  before falling back to generic `python3`/`python`. This avoids bleeding-edge
  Python runtimes when a more compatible supported interpreter is available.
- Local extract setup now prints a short "preparing runtime" line before the
  first-run Python dependency install, so agent installers do not look idle
  during the slow part of setup.

## [0.2.1] - 2026-05-26

### Added

- `nole setup --local-extract`, which creates an isolated Scrapling Python
  runtime, installs `scrapling[fetchers]`, writes
  `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env` and generates an
  env-sourcing MCP wrapper at `~/.local/bin/nole-mcp`.
- Automatic loading of `~/.config/nole/.env` by Nólë commands. Values that
  are already present in the process environment still take precedence.

### Changed

- Agent install docs now treat local Scrapling setup as part of the standard
  GitHub-link install flow, so AI agents should not ask users to hand-create
  `NOLE_SCRAPLING_PYTHON` in normal installs.

## [0.2.0] - 2026-05-26

### Added

- Tag-triggered GitHub Release workflow that runs the audit gate, builds
  cross-platform binaries, generates `SHA256SUMS`, prepends the standard
  release safety notes and publishes GitHub-generated release notes.
- GitHub generated-release-notes configuration and standard release preamble.
- `LICENSE` at repo root (Apache-2.0).
- `SECURITY.md` at repo root (mirrors `docs/SECURITY.md`) so GitHub's
  "Report a vulnerability" link discovers it.
- `CONTRIBUTING.md` covering build/test loop, local audit gate, PR
  expectations, issue filing pointer, and license note.
- `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1).
- `.github/ISSUE_TEMPLATE/bug_report.md` and
  `.github/ISSUE_TEMPLATE/feature_request.md` for structured issue
  intake.
- `.github/PULL_REQUEST_TEMPLATE.md` enforcing motivation, changes,
  test plan, and audit-gate status sections.
- HTTP server hardening in `internal/cli/http.go`:
  `http.MaxBytesReader` (1 MiB) on the `/api/search`, `/api/extract`,
  and `/mcp` POST handlers; `ReadHeaderTimeout`, `ReadTimeout`,
  `WriteTimeout`, and `IdleTimeout` on `http.Server`.
- Optional local Scrapling extraction fallback via `NOLE_SCRAPLING_PYTHON`.
  Nólë invokes a user-supplied Python runtime with `scrapling[fetchers]`
  installed; it does not vendor or redistribute Scrapling code.
- `nole setup --hermes` support for writing Hermes Agent
  `~/.hermes/config.yaml` MCP config while preserving unrelated config,
  comments, existing Nólë policy fields, and user-tuned timeout values.

### Changed

- Bumped Go toolchain from `1.23.0` to `1.25.10` in `go.mod`; added
  `toolchain go1.25.10` directive. CI workflow `go-version` pins also
  bumped to `1.25.x`. This closes the seven reachable Go stdlib
  vulnerabilities reported by `govulncheck` against the 1.23.0
  toolchain.
- `internal/providers/tavily/tavily.go`: `api_key` moved from JSON
  request body to `Authorization: Bearer` header on both Search and
  Extract calls. Matches the shape already used by the Firecrawl
  adapter; reduces the risk of body-logging exposure.
- `internal/providers/firecrawl/firecrawl.go`: request bodies typed
  with `firecrawlSearchRequest` and `firecrawlScrapeRequest` structs
  instead of `map[string]interface{}`; matches the response-side
  pattern.
- `internal/providers/brave/brave.go`: helper `maxInt` renamed to
  `clampMin` for clarity at its single call site.
- `internal/cli/serve.go`: `--mcp` error message now points to
  `docs/CLIENTS/README.md` for setup guidance.
- `internal/cli/doctor.go`: per-provider `free_remaining` line annotated
  with `(local quota counter; resets monthly)` so new users do not
  confuse local counter state with provider-dashboard balance.
- `README.md`: Prerequisites lifted to `Go 1.25+` to match new
  toolchain pin; client setup example extended with a pointer to
  `nole setup --help` for the full client roster.
- Release-facing docs now describe the repository as public and the GitHub
  Release path as tag-triggered automation.
- `README.md` and release notes now mention the native Hermes setup writer
  instead of only the manual `hermes mcp add` path.
- `docs/PROVIDER-KEYS.md`: explicit note that Brave's official free
  tier is 2,000/month and Nólë's local anchor is 1,000 (conservative
  safety margin).

### Security

- Closed 7 reachable Go stdlib vulnerabilities by bumping the
  toolchain (see `govulncheck` output cited in the launch-readiness
  audit at `/tmp/nole-launch-audit.md`): GO-2026-4971 (net),
  GO-2026-4947/4946 (crypto/x509), GO-2026-4918 (net/http HTTP/2),
  GO-2026-4870/4337 (crypto/tls), GO-2026-4601 (net/url IPv6).
- Added HTTP body-size limits and slowloris-class timeouts on
  `nole serve --mcp`.

## [0.1.0] — Unreleased (draft)

Initial v0.1 release-prep readiness. See
`docs/RELEASE-NOTES-v0.1-DRAFT.md` for the full draft summary.

### Added

- Go single-binary CLI with MCP stdio support.
- Stable MCP tools: `search`, `extract`, `provider_status`,
  `budget_status`.
- CLI commands: `search`, `extract`, `classify`, `route-plan`,
  `providers`, `doctor`, `bench`.
- LLM-free multi-intent classifier and route planner.
- `free-first` default policy; `cost-capped` and `quality-first`
  modes for explicit premium-capable provider usage.
- Provider adapters: Brave (search), Tavily (search+extract),
  Firecrawl (search+extract), DDGS (keyless search fallback),
  Scrapling (optional local extract fallback).
- In-process TTL cache and optional file-backed local quota/cost
  ledger.
- Sanitized `route_trace` and compact `routing_insight`.
- `doctor --mcp` subprocess smoke for protocol cleanliness and tool
  visibility.
- Deterministic offline benchmark harness and public-safe route
  evidence summary.
- Agent install docs and client-specific setup writers/checklists for
  Claude Code, Codex CLI, OpenCode, Kimi, Cursor, OpenClaw, Hermes
  Agent.
- Private-prep CI: tests, vet, doctor, bench, public-safety secret
  scan, cross-platform build/checksum validation.
- Partial-keys graceful degradation across the MCP surface
  (conditional `extract` tool registration; `setup_suggestions` in
  `provider_status`; one-time `setup_tip` per session on `search`).
- SSRF preflight (`internal/safenet/url.go`) on every extract URL.
- Bench claims guard rejecting superlative or unsupported
  quantitative phrasing in `docs/BENCHMARKS.md` and
  `docs/ROUTE-EVIDENCE.md`.

[Unreleased]: https://github.com/dorukardahan/nole/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/dorukardahan/nole/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dorukardahan/nole/releases/tag/v0.1.0
