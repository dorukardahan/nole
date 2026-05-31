# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.1] - 2026-05-31

### Added

- Response-body size caps across all providers: `providerhttp.ReadAllLimited`
  / `DecodeJSONLimited` wrap every HTTP response read (16 MiB search,
  64 MiB extract), and the Scrapling subprocess stdout/stderr are bounded
  (64 MiB / 64 KiB), so a hostile or misconfigured endpoint can no longer OOM
  the process with an unbounded body.
- CI now runs `go test -race ./...` (separate `race` job) so the
  concurrency-heavy cache, quota ledger, MCP session tracker and the new
  request-coalescing path are race-checked on every change.
- `govulncheck` now runs in the tag-triggered release workflow (not only on
  PR/push), so the exact published commit is vulnerability-scanned.

### Changed

- Concurrent identical search/extract requests are now coalesced
  (`golang.org/x/sync/singleflight`, keyed by the cache key): N simultaneous
  identical queries collapse to one upstream fetch and one quota debit instead
  of N, so a burst on the `serve` surface can no longer multiply free-tier
  debits. Distinct queries still run fully in parallel.
- The provider fallback loop now short-circuits on a cancelled/disconnected
  request (`ctx.Err()`), surfacing `context.Canceled` immediately instead of
  probing every remaining provider and returning a misleading provider error.
- Search `limit` is clamped centrally to `[1,20]` (and Brave/Tavily request
  counts to their API maximum of 20), so an over-large limit no longer forces
  a guaranteed provider `422` and a non-positive limit no longer leaks through
  to a provider as "no limit".
- Provider retry backoff now adds equal jitter (`math/rand/v2`) to the
  exponential delay, spreading synchronized retry waves; `Retry-After` handling
  is unchanged and stays exact.
- `nole serve` now shuts down gracefully on SIGINT/SIGTERM (drains in-flight
  requests via `http.Server.Shutdown`) instead of hard-killing them, warns on
  a non-loopback bind (the endpoints are unauthenticated and expose BYOK keys
  and quota), and its `--mcp` help/error text now accurately describes the REST
  API at `/api/*` (which has always started alongside `/mcp`) instead of
  "coming soon".
- The read-only HTTP endpoints (`/health`, `/api/providers`, `/api/budget`)
  are now gated to GET/HEAD and log JSON-encode errors, consistent with the
  POST handlers.

### Fixed

- `internal/cli/research.go`: extract content is truncated on a rune boundary
  (`unicode/utf8`) instead of a byte boundary, so a non-ASCII page can no
  longer be cut mid-UTF-8 into mojibake (the class v0.3.0 fixed for provider
  snippets).
- `internal/providers/ddgs`: result `href` values now have `&amp;` decoded
  (previously only title/snippet were decoded), so result URLs are usable
  downstream.
- `internal/safeerr`: `Set-Cookie`/`Cookie` session tokens and userinfo
  credentials in non-`http(s)` scheme URLs (e.g. `ftp://user:pass@host`) are
  now redacted.
- `AGENTS.md` now states the Go 1.25+ toolchain requirement (matching
  `go.mod`), and `CHANGELOG.md` records the previously-missing `[0.2.3]`
  release section.

### Security

- SSRF preflight (`internal/safenet`) now blocks reserved ranges that Go's
  `net.IP.IsPrivate()` misses — CGNAT `100.64.0.0/10`, `0.0.0.0/8`,
  `192.0.0.0/24`, benchmark `198.18.0.0/15`, and IPv6 `64:ff9b::/96` /
  `2001:db8::/32` — and rejects ambiguous all-numeric/octal/hex hostnames
  (e.g. `0177.0.0.1`) that pass Go's resolver but resolve to loopback under a
  libc/`inet_aton` backend (parser-differential SSRF).
- Response-body and subprocess-output size caps (see Added) close an
  unbounded-read OOM/DoS vector across every provider.
- `github.com/buger/jsonparser` bumped `v1.1.1 -> v1.1.2`, clearing the
  DoS advisory `GO-2026-4514` it carried as an indirect dependency.

## [0.3.0] - 2026-05-30

### Added

- `nole setup --gemini` writer for Gemini CLI (`google-gemini/gemini-cli`):
  merges a `nole` entry into `~/.gemini/settings.json`'s `mcpServers` object
  (keyed by server name), preserving unknown root keys and sibling servers,
  with `.bak` backup and preserved permissions. Config shape verified from
  primary source; status is `repo-tested` (the real client was not launched in
  this environment). See `docs/CLIENTS/gemini.md`.
- `nole setup --grok` writer for Grok CLI (`superagent-ai/grok-cli`): upserts a
  `nole` entry into the `mcp.servers` array (keyed by an `id` field) in
  `~/.grok/user-settings.json`, preserving other servers, unknown per-entry
  fields, user `label`/`enabled`, and unknown root keys. Config shape verified
  from primary source; status is `repo-tested`. See `docs/CLIENTS/grok.md`.
- `nole version` command, which prints the binary's version, commit, and build
  date. Release builds now stamp `Commit` and `Date` via `ldflags` (alongside
  the existing `Version`); a development build reports `unknown` for the
  unstamped fields. See `scripts/check-release-builds.sh`.
- `NOLE_CACHE_MAX_ENTRIES` to cap the per-map size of the in-process
  search/extract cache (default `1024`). Documented in `AGENTS.md`.
- `docs/ARCHITECTURE.md` (file:line-anchored codebase + dependency map) and
  `docs/RESEARCH-FINDINGS.md` (adversarially-verified improvement findings).

### Changed

- `internal/providers/providerhttp`: transport-level request failures
  (connection reset, DNS blip, dropped keep-alive) are now retried while
  attempts remain instead of returning on the first failure (which defeated
  `MaxAttempts`); a dead context (cancel/deadline) is still not retried. HTTP
  `408 Request Timeout` is now treated as a transient status and retried,
  aligning the retry policy with its `statusCategory` label and RFC 9110.
- The in-process search/extract cache is now bounded: once a map exceeds its
  entry cap it evicts the oldest entry (FIFO by insertion time), so a
  long-lived MCP server issuing many distinct queries no longer grows the
  cache without limit. Default cap is `1024`; override with
  `NOLE_CACHE_MAX_ENTRIES`.
- Provider snippet and extracted-content truncation now uses
  `core.TruncateRunes`, which truncates on a rune boundary instead of
  byte-slicing, so non-ASCII text can no longer be split mid-UTF-8-sequence
  into mojibake. Applied across the tavily, firecrawl, ddgs and scrapling
  providers and the `research` summary synthesis.

### Fixed

- `internal/providers/ddgs`: result snippets could be attached to the wrong
  result. The parser zipped two independently-collected match slices with a
  counter that only advanced on kept links, so a skipped ad row (which can
  carry its own `result__snippet`) shifted every subsequent organic snippet
  onto the wrong result. Snippets are now paired to the link that physically
  precedes them in the HTML by byte offset.
- `internal/core/quota.go`: a future-dated `PeriodStart` (clock skew, or a
  ledger copied from a host whose clock was ahead) was treated as the current
  period and left a provider stranded as permanently exhausted. The refresh
  guard now refills any period that is not exactly the current one,
  self-healing a future-dated entry by resetting it to the current period.

## [0.2.4] - 2026-05-28

### Added

- Hermes Agent v2026.5.28 / v0.15.0 release-impact notes now document the
  unchanged `mcp_servers` config shape, stricter MCP subprocess environment
  filtering and the wrapper-first setup recommendation for provider keys and
  local Scrapling.

### Changed

- `nole setup --hermes` now writes an explicit Hermes MCP tool policy for new
  Nólë server entries (`tools.resources=false`, `tools.prompts=false`) so the
  v0.15 utility-tool surface stays limited to Nólë's native search/extract
  tools unless the user deliberately changes it.
- OpenClaw client docs now record the 2026-05-28 compatibility re-check on
  OpenClaw 2026.5.27 with the wrapper-backed MCP registry and local Scrapling
  available through the wrapper.

## [0.2.3] - 2026-05-27

### Changed

- Default route matrix refreshed from a 2026-05-26 local live task-provider
  benchmark (39 public cases, 150 provider measurements). Search routes are now
  task-specific rather than one broad ordering: general leads with Brave; docs,
  news, fact-check, pricing, people, social and research lead with Firecrawl;
  code, academic and semantic lead with Tavily. A configured local Scrapling now
  leads the default `extract` route, falling back to Firecrawl then Tavily when
  the local runtime is unavailable. DDGS stays available as a keyless search
  fallback but moves to the end of default search routes after the live sample
  observed repeated rate limits. See `docs/ROUTE-EVIDENCE.md`.

### Fixed

- `nole bench --live --comprehensive` now loads `~/.config/nole/.env` before
  constructing providers, matching normal CLI/MCP startup, and comprehensive
  runs now include the configured local Scrapling provider.
- The doctor free-tier BYOK test now uses an isolated quota ledger so local
  developer state cannot change expected test output.

### Security

- Route evidence stays summary-only: no raw provider payloads, key values, auth
  headers, private URLs or private queries. The route matrix is a local
  evidence-backed default, not a provider SLA or global provider ranking.

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

[Unreleased]: https://github.com/dorukardahan/nole/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/dorukardahan/nole/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/dorukardahan/nole/compare/v0.2.4...v0.3.0
[0.2.4]: https://github.com/dorukardahan/nole/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/dorukardahan/nole/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/dorukardahan/nole/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dorukardahan/nole/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dorukardahan/nole/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dorukardahan/nole/releases/tag/v0.1.0
