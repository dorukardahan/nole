# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.6.0] - 2026-06-04

Theme: **`nole setup --grok-build` for xAI's Grok Build TUI** (resolves #64). Additive
minor — a new setup writer + flag; no existing flag/command/behaviour changes.

### Added

- **`nole setup --grok-build`** writes/merges `~/.grok/config.toml` with a TOML
  `[mcp_servers.nole]` table for xAI's **Grok Build TUI** (the Rust `grok` binary,
  e.g. 0.2.20). This is a **different product** from `superagent-ai/grok-cli`, which
  `--grok` targets (JSON `~/.grok/user-settings.json`) — the v1.5.1 verification pass
  found the installed `grok` was xAI's, whose config nole could not previously write
  (issue #64). Both flags now coexist; `--grok` is unchanged. The setup help and the
  "specify at least one agent" message disambiguate the two products.
  - Reuses the same line-based TOML table upsert as the Codex writer: re-running
    rewrites the `[mcp_servers.nole]` launch keys while **preserving sibling MCP
    servers and root keys**, and a user-set `enabled = false` is **preserved**. If the
    nole entry has been hand-customized with content the writer does not manage (a
    `[mcp_servers.nole.*]` sub-table such as `[mcp_servers.nole.env]`, or an extra
    direct key), the writer **refuses to overwrite it** (leaves the file untouched
    with a clear error) rather than silently dropping the user's settings. Annotated
    TOML headers (`[mcp_servers.nole] # note`) and inline-commented `enabled` values
    are handled. Default form is the bare binary (`command`/`args = ["mcp"]`, matching
    the other writers); `--mcp-wrapper` switches to the env-sourcing wrapper.
- **Grok Build TUI is now `verified` (CLI MCP manager).** Verified 2026-06-04 on
  `grok 0.2.20`: `nole setup --grok-build`'s output was read by `grok mcp doctor` —
  `handshake OK (protocol 2025-06-18)`, 6 tools discovered, `healthy`. The
  `superagent-ai/grok-cli` documented under `--grok` stays `repo-tested` (not installed
  on the host). Evidence in `docs/CLIENTS/LIVE-VERIFICATION.md`.

### Notes

- Purely additive: the CLI command set is unchanged (still `setup`), so the
  command-surface lock stays green; setup flags are an additive-only commitment (not
  snapshot-frozen), so no `docs/STABILITY.md` change is needed. Issue **#64 resolved**.

## [1.5.1] - 2026-06-04

Theme: **Gemini CLI + Grok CLI live-verification pass (docs only).** Both real CLIs
were launched on a host; the findings are recorded honestly — neither is upgraded to
`verified`, and one notable Grok-CLI discrepancy is documented + tracked.

### Documentation

- **Gemini CLI (0.40.1):** recorded that the real CLI was launched and `nole setup
  --gemini`'s path + `mcpServers` schema match the client's own `gemini mcp add`
  output. Status stays `repo-tested` (not `verified`): Gemini 0.40.1's `gemini mcp
  list` prints nothing non-interactively (even for its own added servers) and there
  is no `gemini mcp test`/`doctor`, so in-client tool visibility was not observable
  without a model turn.
- **Grok CLI:** documented that the installed `grok` (xAI's "Grok Build TUI" 0.2.20,
  reading `~/.grok/config.toml` with a full `grok mcp add/list/remove/doctor`
  manager) is a **different product** from the `superagent-ai/grok-cli` that `nole
  setup --grok` targets (JSON `~/.grok/user-settings.json`). Nólë's MCP server
  connected to the installed Grok Build TUI cleanly (`grok mcp doctor`: handshake OK,
  6 tools discovered) but via a config Nólë does not write; the documented client was
  not installed, so it stays `repo-tested`. Corrected the now-stale "no `grok mcp add`
  command" claim and clarified the two distinct CLIs. Writer-target decision tracked
  in **issue #64**.
- Recorded the run (isolated throwaway `HOME`; the maintainer's real `~/.gemini` and
  `~/.grok` were untouched) in `docs/CLIENTS/LIVE-VERIFICATION.md`, and confirmed
  Nólë's advertised MCP surface is the expected 6 tools (`search`, `research`,
  `provider_status`, `budget_status`, `extract`, `search_and_extract`).

No code, schema, or surface changes — docs only.

## [1.5.0] - 2026-06-04

Theme: **keyless arXiv academic provider.** A new primary-source scholarly-preprint
search provider reinforces the `academic` route, exactly as the keyless Wikipedia
provider reinforces the encyclopedic routes. Purely additive — no new key, no setup,
no dependency, and no change to any frozen CLI/MCP/REST/task-enum surface.

### Added

- **`arxiv` keyless search provider** (`internal/providers/arxiv`), backed by the
  public arXiv Atom query API (`https://export.arxiv.org/api/query`). No API key,
  no registration, no new Go dependency (stdlib `encoding/xml`). It is routed on the
  `academic` task ONLY, positioned immediately before `wikipedia` (after the keyed
  providers, before the last-resort `ddgs` fallback), so an academic query reaches
  primary-source preprints first, then the encyclopedic overview, then the general
  backstop. It is deliberately NOT a general fallback and is on no other route.
  - Results pass through arXiv's native relevance order; `score` stays unset (arXiv
    exposes no numeric relevance score and Nólë never fabricates one), and
    `published_at` is the paper's first-version submit time, verbatim.
  - The agent's query is passed through verbatim (no field-operator parsing). A
    query arXiv rejects comes back as an error entry that is skipped — an honest
    empty fall-through to Wikipedia/DDGS, never an error.
  - Good-citizen by design: descriptive `User-Agent`, exactly one request per
    search, retries disabled (arXiv's Terms of Use ask for ≤1 request / 3 s on a
    single connection, and its edge 429 carries no `Retry-After`), a circuit breaker
    (it is routed before the DDGS fallback), an SSRF-safe fixed-host request, a
    size-capped response body, and status-only error redaction.
- The route-planner allowlist (`nole route-plan --providers arxiv`) and the
  comprehensive benchmark provider map now include `arxiv`.

### Notes

- This is a route-matrix + provider addition. The route matrix is explicitly **not**
  part of the frozen stability surface (see `docs/STABILITY.md` → "NOT covered"), and
  arXiv adds no new command, flag, MCP tool, env var, or task-enum value — so every
  surface-lock test stays green and `docs/STABILITY.md` is unchanged (consistent with
  the keyless Wikipedia provider added in v1.1.0). See `docs/ROUTE-EVIDENCE.md` for
  the (capability-based, not benchmark-measured) insertion rationale.

## [1.4.0] - 2026-06-04

Theme: **REST (`nole serve`) is now a stable, authenticated surface.** It graduates
from experimental to a frozen 1.x contract: bearer-token auth, a surface-lock, and
an honest 402 mapping. Additive — no CLI/MCP/env removal or rename.

### Added

- **Bearer-token auth for `nole serve` (`NOLE_SERVE_TOKEN`).** When set, every
  endpoint except `/health` requires `Authorization: Bearer <token>`
  (constant-time compared, never logged); `/health` stays open for readiness
  probes. The token is read from the process env (never argv/config).
- **`TestStableRESTSurface`** pins the REST contract: the route set
  (`/health`, `/mcp`, `/api/{search,extract,search_and_extract,research,providers,budget}`),
  the error-envelope field shape, and the status-code mapping. `docs/STABILITY.md`
  moves REST from experimental to **stable**.

### Changed

- **REST exhausted-free-tier now returns HTTP 402** (`NoFreeQuotaError` →
  `402 Payment Required`) instead of `500` — an honest "this would require paid
  usage you have not authorized". Landed together with the surface-lock so it is
  part of the frozen contract.
- **SECURITY (fail closed): a non-loopback `nole serve` bind now REQUIRES
  `NOLE_SERVE_TOKEN`.** Previously a non-loopback bind printed a warning and started
  anyway (serving your BYOK keys + quota to the network unauthenticated — explicitly
  experimental + warned-against). Now `serve` **refuses to start** on a non-loopback
  bind without a token, with a clear message telling you to set `NOLE_SERVE_TOKEN`
  or bind to loopback. The loopback default (`127.0.0.1`) is unchanged (no token
  needed). If you currently run `--listen 0.0.0.0` without a token, set
  `NOLE_SERVE_TOKEN` on upgrade.

### Docs

- `nole serve --help`, `docs/STABILITY.md` (REST stable section + `NOLE_SERVE_TOKEN`
  env entry + the new surface-lock in the enforcement list), README, PACKAGING,
  PUBLIC-RELEASE-CHECKLIST, and the OpenCode client doc updated to reflect the
  authenticated, stable REST surface (no longer "experimental / unauthenticated").

## [1.3.1] - 2026-06-03

Theme: **onboarding messages catch up with always-on keyless extract.** Follow-up
to v1.3.0; message accuracy only, no behaviour change.

### Docs

- The post-setup message (`nole setup`) implied keyless URL extraction required
  `nole setup --local-extract`. Since v1.3.0 the built-in `httpfetch` backstop
  provides keyless URL extraction **out of the box** (best-effort, no JavaScript),
  so the message now states that, and frames `--local-extract` (Scrapling) +
  provider keys as OPTIONAL upgrades to JS-capable / higher-fidelity extraction.
  This message is compiled into the binary, so it always matches the installed
  version's real capabilities.
- The post-install messages (`install.sh`, `install.ps1`) are made version-NEUTRAL:
  the scripts run regardless of which release `NOLE_INSTALL_VERSION` pins, so they
  no longer claim a version-specific capability (a pinned pre-v1.3.0 install has no
  `httpfetch`). They state the always-true keyless-DDGS-search baseline and point to
  `nole doctor` for the installed version's actual search/extract capabilities.

## [1.3.0] - 2026-06-03

Theme: **keyless extract out of the box.** A new pure-Go HTTP-fetch extract
provider closes the gateway's biggest zero-setup gap: with no keys and no Python,
`search` worked but `extract` / `search_and_extract` / research's extract phase
hard-failed (Scrapling was the only keyless extract path, and it needs a venv).
Now extract works with zero keys and zero setup. Additive and keyless — no
breaking change to any committed CLI/MCP/env surface.

### Added

- **`httpfetch` — keyless, pure-Go, dependency-free extract provider**
  (`internal/providers/httpfetch`). GETs a public URL and strips the HTML to
  readable text using only the standard library (`net/http` + `regexp` +
  `html.UnescapeString`); **no new module dependency** (consistent with the
  existing `ddgs`/`wikipedia` HTML helpers). It is the LAST-RESORT keyless backstop
  on the extract route (`scrapling -> firecrawl -> tavily -> httpfetch`) — the
  extract-side analogue of DDGS on the search routes. It runs **no JavaScript**, so
  it is honestly weaker than Scrapling/Firecrawl on SPA/JS-rendered pages, and that
  is an accepted limit for a zero-setup fallback. Registered unbreakered (like the
  other free fallbacks) and seeded `keyless-free` in the quota ledger.
  - SSRF-safe on TWO layers: every redirect hop is re-validated by
    `safenet.ValidateURLContext` before it is fetched (a public URL that
    30x-redirects to a private/metadata host is blocked at the redirecting hop, via
    a manual no-follow redirect walk), AND the resolved IP is re-validated again at
    DIAL time by a transport `Control` hook (new exported `safenet.ValidateIP`),
    closing the DNS-rebinding / split-horizon window between preflight and connect.
    The transport disables proxies so the dial guard always sees the real target IP.
  - Hardened: response body is size-capped (over-cap is fatal, never extracts a
    truncated body); non-text content types are refused, and an EXPLICIT `text/plain`
    response is returned verbatim (not HTML-stripped, so `#include <stdio.h>` and
    other angle-bracketed content survive); transport errors are redacted to drop the
    request URL/query (no token leak on the non-JSON CLI path); errors otherwise carry
    only HTTP status + byte-size metadata. Descriptive `User-Agent` with a contact URL
    (no browser-spoof). Fuzz-tested (`FuzzHTMLToText`: never panics, deterministic,
    preserves UTF-8 validity).

### Changed

- **MCP `extract` / `search_and_extract` are now advertised out of the box.** Tool
  gating moved from an env-key/Scrapling check to a registry-capability check
  (`core.Service.HasExtractCapableProvider`): the tools are advertised whenever the
  registry has an available extract-capable provider. Because the keyless
  `httpfetch` backstop is always registered, that is always true in the default
  service — so agents get `extract`/`search_and_extract` with zero keys and zero
  setup. **This is a backward-compatible surface expansion** (the tools are only
  ever ADDED to the keyless configuration, never removed); the surface-lock tests
  and `docs/STABILITY.md` are updated to record it. A higher-fidelity / JS-capable
  provider (Tavily/Firecrawl key, or local Scrapling) is still preferred when
  configured.
- `nole bench --live --comprehensive` includes httpfetch. (The search route-planner
  `--providers` allowlist intentionally does NOT accept `httpfetch` — like Scrapling
  it is extract-only, and route-plan plans search routes, so `--providers httpfetch`
  correctly errors rather than emitting an unusable search plan.)

### Docs

- `docs/STABILITY.md`, `docs/PROVIDER-KEYS.md` (provider table, cost-class
  examples, the partial-key behaviour section, a dedicated `## httpfetch` section),
  `docs/ROUTE-EVIDENCE.md` (extract route), and `README.md` (provider list, keyless
  enumerations) updated to document the always-on keyless extract backstop and its
  honest no-JavaScript limit.

## [1.2.3] - 2026-06-03

Theme: **security — bump the Go toolchain to 1.25.11 for two standard-library
CVE fixes.** No code or surface change; a toolchain/build-input bump.

### Security

- **Go 1.25.10 → 1.25.11** (`go.mod` directive + `go-version` pins in `ci.yml`
  and `release.yml`, now pinned to the exact patch rather than the floating
  `1.25.x`). The release `govulncheck` gate (pinned `@v1.3.0`, which fetches the
  live vuln DB at scan time) began failing on **GO-2026-5039** (`net/textproto`)
  and **GO-2026-5037** (`crypto/x509`), both *Found in go1.25.10, Fixed in
  go1.25.11*. Verified locally under go1.25.11: `govulncheck ./...` reports **no
  vulnerabilities**, and the full suite builds/tests clean. This unblocks the
  release pipeline (the v1.2.2 docs release run was halted by this gate before
  publishing; its content ships here in v1.2.3). The `1.25.x` pins are tightened
  to `1.25.11` so the build/scan toolchain is deterministic, not whatever the
  runner's setup-go manifest happens to resolve.


## [1.2.2] - 2026-06-03

Theme: **docs — surface the live Scoop channel.** Documentation only; no code,
no committed-surface change.

### Docs

- **README** now documents the **Scoop (Windows)** install channel
  (`scoop bucket add nole https://github.com/dorukardahan/scoop-nole` then
  `scoop install nole`), alongside the existing Homebrew block. The working
  channel was previously invisible to Windows users in the README.
- **`docs/PACKAGING.md`** Scoop section updated from "DORMANT" to **LIVE**: the
  `dorukardahan/scoop-nole` repo and the `SCOOP_BUCKET_TOKEN` secret both exist,
  so `release.yml` auto-rolls the bucket on every stable release; `nole.json` is
  bootstrapped and tracks the latest stable tag; activation issue #54 is closed.
  The maintainer re-setup notes (separate-token requirement, repo-before-secret
  ordering, fail-loud-on-misconfig) are retained.

## [1.2.1] - 2026-06-02

Theme: **REST (`nole serve`) hardening + parity proof** — close the test gaps and
one real defect on the HTTP surface, and document its true status honestly. No
change to any committed (CLI/MCP/env) surface.

### Fixed

- **REST error bodies no longer silently truncate.** `writeHTTPJSONError` is now a
  handler method that routes its encode failure through the same `logEncodeErr`
  path as the success encoders (it previously swallowed the error), so a partial
  error response leaves a server-side log instead of vanishing.

### Changed

- **REST 400 decode-error bodies carry `operation`** (additive) — a strict subset
  of the 500 `cliErrorEnvelope`, so a consumer parses `operation`+`error`
  uniformly across 400 and 500.
- **`docs/STABILITY.md`** now states the REST status honestly: its error envelope,
  `route_trace`/`routing_insight`, task normalization, secret redaction, and
  cost-fail-closed behaviour are **at parity with CLI/MCP** (shared `core` +
  `safeerr` code), but REST stays **experimental** — the route/field shapes are
  not surface-locked and the endpoints are unauthenticated (expose BYOK keys +
  quota). Parity of the contract ≠ a stability freeze; auth + a REST surface-lock
  are prerequisites for declaring it stable (a future PR).

### Tests

- New `internal/cli/http_rest_parity_test.go`: drives the REAL `buildMux` to prove
  the 5-field error-envelope parity, the previously-untested `search_and_extract`
  / `research` happy paths, the new 400 `operation` key on every POST route,
  oversized-body 400s on all POST routes, unknown-path 404, the research
  error-envelope divergence (forced via a cancelled context), and — the key
  gap-closer — that a **provider-originated secret is redacted end-to-end** through
  the live HTTP path (not just envelope shape). `serve_test.go` adds a
  unit-testable `nonLoopbackWarning` (prints the exposure warning, never silenced
  by `NOLE_LOG=off`) and a regression-lock on the deliberate 300s `WriteTimeout`.

## [1.2.0] - 2026-06-02

Theme: **Scoop (Windows package manager)** — a `scoop install` channel, mirroring
the Homebrew tap. Additive packaging only; no binary/behaviour change.

### Added

- **Scoop bucket channel** `dorukardahan/scoop-nole`:
  `scoop bucket add nole https://github.com/dorukardahan/scoop-nole; scoop install nole`.
  A prebuilt-binary manifest (`packaging/scoop/nole.json.tmpl`) points `64bit` +
  `arm64` at the matching `nole-windows-<arch>.exe` release asset and pins each
  asset's `hash` (bare lowercase sha256 from the release `SHA256SUMS` — Scoop's
  integrity check, fail-closed); the `bin` shim exposes the command as `nole`.
- **`Update Scoop bucket`** step in `release.yml` auto-rolls the bucket on each
  stable release — renders the template (version + the two Windows checksums) and
  pushes `nole.json`, authenticating clone+push with a PAT via `GIT_CONFIG_*` Basic
  auth (the same mechanism as the Homebrew tap). It is **gated on a
  `SCOOP_BUCKET_TOKEN` secret and skips cleanly when absent** (and on prereleases),
  so a release never fails for lack of it.
- **`scripts/audit.sh` Scoop check** — renders the manifest with dummy values and
  validates JSON shape with `jq` (required fields, both arch keys, bare-hex hashes,
  windows-`.exe` URLs); skips if `jq` is absent.

### Fixed

- **A hyphenated tag is always treated as a prerelease** in `release.yml`. The
  `prerelease` flag was only inferred from a hyphenated tag on a `push`; a manual
  `workflow_dispatch` for e.g. `v1.2.0-rc.1` with the default `prerelease=false`
  input would have marked it stable and synced the rc asset into the **stable**
  Homebrew tap **and** Scoop bucket. Now a hyphenated tag is a prerelease on both
  push and dispatch; the dispatch input can only *additionally* force a
  non-hyphenated tag to prerelease, never downgrade a hyphenated one to stable.

### Notes

- The Scoop channel is **DORMANT** until the maintainer creates
  `dorukardahan/scoop-nole` and adds a `SCOOP_BUCKET_TOKEN` secret (a PAT with
  `contents: write` on that repo — `HOMEBREW_TAP_TOKEN` is scoped to `homebrew-nole`
  only). **Create the repo before the secret:** the default no-secret state skips
  cleanly (a release never fails for lack of it), but adding the secret before the
  repo exists makes the post-release sync fail loudly (by design — surfaces a
  misconfigured PAT/repo; the Release itself is already published). See
  `docs/PACKAGING.md` and the activation issue. North-star unaffected — packaging
  only.

## [1.1.1] - 2026-06-02

Theme: **install.ps1 functional test** — closes the one real test gap (the
PowerShell installer was previously only `pwsh` parse-checked + diff-reviewed).
No product surface change.

### Added

- **`install_ps1_script_test.go`** — a real functional test for
  `scripts/install.ps1`, mirroring the `install.sh` suite case-for-case (15
  tests): SHA256 verify + mismatch fail-closed, the full three-way attestation
  taxonomy (unusable→soft-skip, signed+reachable mismatch / signed-but-missing /
  malformed-tag → fail-closed, pre-signing / unreachable → soft-skip), and the
  `NOLE_INSTALL_VERIFY=auto|require|off` modes — driven by a fake release server +
  injected fake `gh` (reusing the install.sh harness). It shells out to `pwsh` and
  **skips cleanly when pwsh is absent** (CI ubuntu ships pwsh 7; local macOS without
  it just skips, so the gate never breaks). Validated against real pwsh 7.6.2.

### Fixed

- **`install.ps1` cross-platform guard** — the user-PATH persistence block uses
  the Windows-only `User` `EnvironmentVariableTarget`, which throws on
  pwsh-on-Linux/macOS (under `$ErrorActionPreference='Stop'`, aborting *after* the
  binary is staged). It is now guarded so it runs on Windows (PowerShell 5.1 and
  7+) and is skipped on non-Windows pwsh. No behaviour change on Windows (the only
  platform the installer targets in production); it makes the script
  inspectable/testable cross-platform.

## [1.1.0] - 2026-06-02

Theme: **keyless Wikipedia/MediaWiki provider** — a new zero-setup search
provider that reinforces encyclopedic routes. Additive: no CLI/MCP/env surface
change, so the v1.0.0 stability commitment is unaffected.

### Added

- **Wikipedia/MediaWiki provider** (`internal/providers/wikipedia`) — keyless
  search backed by the official MediaWiki Action API (`list=search` on English
  Wikipedia), with a descriptive `User-Agent` per Wikimedia policy. It is routed
  into the `factcheck`, `people`, and `academic` routes only, positioned before
  the `ddgs` last-resort fallback — so it reinforces encyclopedic/biographical/
  factual queries without becoming a general fallback and without displacing
  DDGS. Cost class `keyless-free` (never triggers paid spend). Search + status
  capabilities only (no extract), so the MCP extract-tool gating and the
  surface-lock tests are unchanged. Snippets are HTML-sanitized; `maxlag`
  backpressure and API error bodies are handled redaction-safely (no query/host
  detail leaks); results — including disambiguation/list pages — pass through
  verbatim for the agent to weigh (Nólë never judges quality). Because it is
  routed before the DDGS fallback, it carries a **circuit breaker** (like the
  other remote providers): a slow/unreachable upstream short-circuits fast in a
  long-lived `serve`/MCP process instead of stalling those routes ahead of DDGS.
  `nole route-plan --providers wikipedia` is accepted for inspection.

### Notes

- Routing-only addition: it changes which providers serve `factcheck`/`people`/
  `academic` (see `docs/ROUTE-EVIDENCE.md`), not the result contract. `ddgs`
  remains the final last-resort entry on every search route. Closes #43.

## [1.0.2] - 2026-06-02

Theme: **fix the Homebrew tap auto-bump** — release-infra bugfix; no product
behaviour, surface, or dependency change.

### Fixed

- **`release.yml` "Update Homebrew tap" now authenticates the `git clone` with
  the correct scheme.** Two latent bugs in the v0.13.0 auto-bump (dormant until a
  real `HOMEBREW_TAP_TOKEN` was configured) surfaced on the v1.0.1 release:
  (1) the PAT was configured only *after* `git clone`, so the clone ran
  unauthenticated and the runner refused it (`could not read Username ... No such
  device or address`); (2) the token was sent as `Authorization: Bearer`, which
  is the REST-API scheme — GitHub's Git-over-HTTPS expects **Basic** auth (token
  as the password), so even an authenticated clone/push would have been rejected.
  Fix: encode `x-access-token:<token>` as a Basic credential (as
  `actions/checkout` does) and supply it via `GIT_CONFIG_*` env so it applies to
  the clone, branch detection, and push alike — with no token in the URL, process
  args, or any persisted `.git/config`. Resolves the auto-bump path tracked in #48.

## [1.0.1] - 2026-06-02

Theme: **docs + CI hygiene** — no behaviour, surface, or dependency change; the
stable contract is untouched. Patch-level cleanup found by an adversarial doc/CI
sweep.

### Fixed

- **README accuracy** — corrected four drifted claims: the MCP tool list now
  matches the frozen contract (`search`, `research`, `provider_status`,
  `budget_status` always; `extract`/`search_and_extract` when extract-capable);
  the `research` description no longer claims Nólë "synthesizes" (it returns
  evidence for the agent to synthesize — the core gateway invariant); `nole
  version` dev-build output is described precisely (`dev` version + `unknown`
  commit/date); the planner line drops the non-existent `--tasks` flag in favour
  of the real `--task` / `--single-intent`.
- **Release-notes pointer** — README pointed at `docs/RELEASE-NOTES-v0.7.1.md` as
  "current"; now points at `CHANGELOG.md` + the GitHub Releases page (the
  per-version notes file convention ended at v0.7.1; this no longer goes stale).

### Changed

- **`govulncheck` pinned to `@v1.3.0` across the board** — `ci.yml` was on
  `@latest`; both `ci.yml` and `release.yml` now pin the current latest
  (`v1.3.0`, was `v1.1.4` on the release path). Reproducible scans + supply-chain
  hygiene; the vuln DB is still fetched fresh at run time, so detection coverage
  is unchanged. `CONTRIBUTING.md` and `docs/ARCHITECTURE.md` no longer say
  `@latest`, so local, CI, and documented scans agree.

## [1.0.0] - 2026-06-02

Theme: **stability commitment** — declare the agent-facing surface stable under
SemVer. No behaviour change; this release freezes the contract so integrations at
1.x keep working across 1.x upgrades.

### Added

- **`docs/STABILITY.md`** — the v1.0.0 stability commitment: which surfaces are
  frozen (CLI commands + primary flags + `--json` fields, MCP tools + params +
  result shapes, the documented `NOLE_*` env vars, the install/integrity contract,
  and the safety invariants) and which are explicitly NOT (HTTP/REST `serve`,
  provider routing order, `route_trace`/`NOLE_LOG` formats, benchmark numbers,
  `internal/...` packages, the ledger file format).
- **Surface-lock tests** — `TestStableCLICommandSurface` (internal/cli) and
  `TestStableMCPToolSurface{WithoutExtract,WithExtract}` (internal/mcpserver) pin
  the exact frozen command + MCP-tool sets. They fail on any silent add/remove/
  rename, forcing a conscious decision + a matching version bump.

### Notes

- SemVer from here: MAJOR = breaking surface change, MINOR = additive, PATCH =
  fixes. The two safety invariants (MCP stdout is JSON-RPC only; secrets are never
  printed/logged) and the cost-fail-closed default are never weakened in 1.x.
- README points first-time integrators at `docs/STABILITY.md`.

## [0.13.0] - 2026-06-02

Theme: **Homebrew** — `brew install` for macOS and Linux.

### Added

- **Homebrew tap** `dorukardahan/homebrew-nole`: `brew install dorukardahan/nole/nole`
  installs the prebuilt release binary per platform (macOS/Linux × amd64/arm64) and
  pins each asset's `sha256` (Homebrew's integrity check, sourced from the release's
  `SHA256SUMS`). The formula's source of truth is `packaging/homebrew/nole.rb.tmpl`
  in this repo; build-provenance verification is surfaced in the formula `caveats`.
- **`Update Homebrew tap`** step in `release.yml` auto-rolls the tap on each stable
  release — renders the template with the version + the four published checksums and
  pushes `Formula/nole.rb`. It is gated on a `HOMEBREW_TAP_TOKEN` secret (a PAT with
  `contents: write` on the tap repo) and **skips cleanly when that secret is absent**
  (the formula is then bumped manually), so a release never fails for lack of it.

### Notes

- Prebuilt-binary formula by design (not build-from-source): the release binary
  already carries the version/commit/date ldflags stamp, so `nole version` and
  `doctor --check-updates` report correctly. README + `docs/PACKAGING.md` updated.

## [0.12.0] - 2026-06-02

Theme: **Windows install** — a first-class PowerShell installer with the same
verification contract as the bash installer, so Windows is no longer "download
the .exe manually".

### Added

- **`scripts/install.ps1`** — PowerShell installer mirroring `scripts/install.sh`
  exactly: detects arch via `RuntimeInformation.OSArchitecture` (correct under
  x64 emulation on Windows-on-ARM), downloads `nole-windows-<arch>.exe` +
  `SHA256SUMS`, verifies SHA256 with `Get-FileHash` (mandatory floor, fail-closed),
  runs the additive `gh attestation verify` gate with the identical three-way
  taxonomy + `SignedSince` floor + malformed-tag guard + `gh >= 2.93.0`
  (CVE-2026-48501) gate + `--hostname` derivation, installs to
  `%LOCALAPPDATA%\Programs\nole` (stage-in-place + atomic `Move-Item`) and adds it
  to the user PATH. Honors the same `NOLE_INSTALL_*` env overrides. `install.sh`'s
  Windows message and the README now point at it.

### Notes

- Native-command error handling is the key porting trap: `gh` is invoked with
  `$PSNativeCommandUseErrorActionPreference` disabled so a non-zero exit does not
  throw (it would skip the soft-skip classification and break the zero-dep floor);
  `$ErrorActionPreference='Stop'` still governs cmdlets so downloads/hashing
  fail-closed. gh's output is classified internally but never surfaced (it can
  carry private URLs). `audit.sh` parse-checks `install.ps1` with `pwsh` when
  available (CI Linux runners have it; local macOS skips).

## [0.11.0] - 2026-06-02

Theme: **self-update** — let an installed nole upgrade itself, with the same
verification contract as `scripts/install.sh`. The natural continuation of
`doctor --check-updates` (which only advises): now nole can apply the update.

### Added

- **`nole self-update`** — downloads the latest published release (or
  `--version <tag>`), verifies it, and atomically replaces the running binary.
  SHA256 is the mandatory integrity floor (verified in-process with
  `crypto/sha256`, fail-closed on mismatch); a GitHub build-provenance
  attestation is an additive, best-effort second gate via `gh attestation
  verify` — the SAME three-way taxonomy and `gh >= 2.93.0` (CVE-2026-48501) gate
  as the installer. Flags: `--check-only` (report only), `--version <tag>`,
  `--verify auto|require|off`. The outbound check is anonymous (no auth header),
  and the command never auto-runs — the user invokes it explicitly. For
  consistency with `install.sh`, it honors the same env vars
  (`NOLE_INSTALL_REPO` / `_API_URL` / `_DOWNLOAD_URL` / `_VERIFY` / `_VERSION`)
  when the matching flag is unset; `NOLE_INSTALL_DIR` does not apply (it replaces
  the running binary in place).

### Notes

- The self-replace is hand-rolled (no self-update library): a NEW file is staged
  in the running binary's own directory and atomically `rename`d into place — it
  never truncates/overwrites the running inode, which keeps it Apple-Silicon
  codesign-safe. The stage uses `os.CreateTemp` (O_EXCL + a random name) so a
  pre-planted symlink in a writable install dir cannot hijack the write. On
  Windows the running `.exe` is renamed aside to `.old` first (it cannot be
  deleted while running; the next self-update removes it). A failure before the
  rename leaves the existing binary untouched.
- Verification deliberately shells out to `gh` rather than vendoring
  `sigstore-go` in-process: the latter would add ~70 transitive modules,
  ~15-26 MB, and force a Go 1.25 toolchain for a feature that runs only at
  upgrade time — exactly the heavy dependency the gateway avoids. SHA256 (the
  mandatory floor) is in-process and needs no external tool.

## [0.10.0] - 2026-06-02

Theme: **supply-chain provenance** — make release artifacts verifiable beyond a
bare checksum, WITHOUT compromising the zero-dependency install path. SHA256
stays the mandatory integrity floor; signature verification is an additive,
best-effort second gate that never blocks a keyless install.

### Added

- **Build-provenance attestations** — `.github/workflows/release.yml` now signs
  every release with keyless Sigstore-backed
  [GitHub artifact attestations](https://docs.github.com/actions/security-for-github-actions/using-artifact-attestations)
  (`actions/attest-build-provenance`, pinned by commit SHA). One attest step with
  a multi-path `subject-path` covers each per-binary artifact (`dist/nole-*`) AND
  the `SHA256SUMS` file itself as subjects, so both the binaries and the checksum
  list are provenance-bound. No repo Secret is added — signing uses the workflow's
  OIDC identity. Attestations live in the GitHub attestation API and are resolved
  by digest at verify time; they are NOT uploaded as release assets.
- **`install.sh` additive verification** — after the mandatory SHA256 check,
  `install.sh` optionally verifies the build-provenance attestation via
  `gh attestation verify`, hardened to the exact release-workflow signer identity
  (`--signer-workflow`). Controlled by `NOLE_INSTALL_VERIFY=auto|require|off`
  (default `auto`).

### Security

- **Three-way fail taxonomy** keeps the zero-dependency floor intact while
  closing the realistic downgrade path: (a) verifier unusable (no `gh`, `gh`
  lacking the subcommand, or `gh` older than 2.93.0 — the CVE-2026-48501 fix that
  stopped `gh attestation` leaking the host token to TUF mirrors) → **soft-skip**;
  (b) attestation cryptographically invalid, OR provably missing on a
  KNOWN-signed release while the API is reachable → **fail closed**; (c) API
  unreachable / anonymous / pre-signing release → **soft-skip**. A `SIGNED_SINCE`
  version floor (v0.10.0) lets the installer distinguish a genuinely-unsigned old
  release from a stripped attestation on a signed one.
- The optional gate runs BEFORE the stage+atomic-rename, so a verification
  failure never disturbs an existing install. `install.sh` passes no token of its
  own; an anonymous host simply soft-skips (cli/cli #11803).

### Notes

- SHA256 remains the only check that gates the zero-dependency path: a machine
  with just `curl`/`wget` + `sha256sum`/`shasum` installs exactly as before. The
  attestation gate is exercised by nine `go test` cases that inject a fake `gh`
  (valid, mismatch→fail-closed, no-attestation old vs signed version, API
  unreachable, CVE-gated old gh, require, off) with no network.

## [0.9.0] - 2026-06-02

Theme: **onboarding** — make Nólë easy to install and keep current, and make the
zero-key story explicit, without changing the gateway's behaviour. Adds the CLI's
first outbound network call (the opt-in update check), isolated and fail-soft.

### Added

- **`scripts/install.sh`** — one-command install of a prebuilt release binary:
  detects OS/arch (Linux/macOS; amd64/arm64), downloads the matching
  `nole-<os>-<arch>` asset + `SHA256SUMS`, **verifies the checksum before
  installing** (fails closed on mismatch), and installs to `~/.local/bin`
  (rm-first — the Apple-Silicon-safe order). Touches no secrets, sends no
  telemetry. Overridable via `NOLE_INSTALL_VERSION` / `NOLE_INSTALL_DIR` /
  `NOLE_INSTALL_REPO`. Windows is directed to the `.exe` asset (the bash installer
  targets Unix). Docs lead with download-then-run; pipe-to-bash as a convenience.
- **keyless-aware setup message** — `nole setup` now states up front that Nólë
  works with ZERO keys (keyless DDGS web search out of the box; `nole setup
  --local-extract` adds keyless local URL extraction) and that provider keys are
  optional, before listing them.
- **`nole doctor --check-updates`** — fail-soft staleness check (new
  `internal/selfupdate` package): compares the running version to the latest
  published release and prints a one-line notice if behind. SILENT when offline or
  on any error, never fails `doctor`, sends no auth header, and makes no network
  call unless the flag is passed. Works in human and `--json` modes (the JSON
  `update` field). Endpoint overridable via `NOLE_RELEASES_API`.

### Notes

- `internal/selfupdate` is the CLI's first outbound network call: anonymous,
  short-timeout, fail-soft, never writes stdout. Version comparison is per-segment
  NUMERIC (so 0.10.0 > 0.9.0) and treats any non-release version (`dev`,
  pre-releases, garbage) as "not behind" (no nag). `install.sh` is syntax-linted in
  `audit.sh` and exercised end-to-end against an httptest server in `go test`
  (install + checksum-mismatch-fails-closed).

## [0.8.0] - 2026-06-01

Theme: **observability** — make the gateway's behaviour and configuration
visible to operators and agents, WITHOUT changing routing, judging quality, or
weakening the two invariants the rest of Nólë depends on (stdout stays
protocol-only; secrets are never printed or logged). Pure visibility: a
structured logger, a config dump, and a machine-readable doctor.

### Added

- **`NOLE_LOG` structured diagnostic logging** (new `internal/nolelog` package).
  `NOLE_LOG=text` (default) keeps human-readable diagnostics on stderr,
  `NOLE_LOG=json` emits one compact JSON object per line, and `NOLE_LOG=off`
  silences them. The logger writes ONLY to the writer it is constructed with
  (always `os.Stderr` in production) and NEVER references `os.Stdout`, so it can
  never corrupt the MCP JSON-RPC stream, a REST body, or a `--json` command's
  output. Plain field values flow through `safeerr.Redact` and are fully redacted
  when the field key names a credential (`api_key`/`token`/`secret`/…); error
  fields flow through `safeerr.Message`. Wired into the service (research step
  failures) and the `serve` HTTP path (encode failures, server lifecycle) via a
  new `core.WithLogger` option, replacing the ad-hoc `fmt.Fprintf(os.Stderr, …)`
  diagnostic sites. Two stderr messages stay RAW and unconditional — never routed
  through the logger, so `NOLE_LOG=off` can never silence them: the top-level
  fatal error in `main.go` (the command's result) and `serve`'s non-loopback bind
  warning (the only runtime notice that unauthenticated endpoints expose keys).
- **`nole config dump [--json]`** — prints the effective configuration: cost
  policy, hard-cap source, log mode, ledger path/state, recognized non-secret
  `NOLE_*` env vars, provider cost classes, and quota floors. Secrets appear as
  set/unset ONLY, never a value; even the non-secret env values are passed
  through a closed allowlist plus `$HOME`-collapse and redaction. Read-only: it
  sources `BudgetStatus()`/`ProviderStatus()`, which never debit or refresh the
  ledger, so inspecting config never spends quota.
- **`nole doctor --json`** — the doctor report as one machine-readable JSON
  document (providers, secret presence, paid-mode, budget, and the optional
  `--mcp` smoke block). On `--json --mcp` smoke failure it still emits the report
  and returns the same non-zero exit as the human path. The human `doctor`
  output is unchanged.

### Notes

- No new MCP tools, no routing change, no schema change to existing surfaces.
  `nolelog` and the new commands are gated in `audit.sh` (`config dump`,
  `config dump --json`, `doctor --json`) alongside the existing
  `doctor --mcp`/`providers --json` smokes, and the `doctor --mcp` stdout-purity
  smoke is re-confirmed green under `NOLE_LOG=json`.

## [0.7.1] - 2026-06-01

Theme: **honest-quota data correction** — a follow-up to v0.7.0's trust pillar
that re-verifies every BYOK provider's free tier against current (June 2026)
published pricing and fixes a credit-vs-call unit mismatch. Data, a one-line
upgrade-path ledger clamp, and docs; no schema or signature change, and no new
MCP tools.

### Changed

- **Tavily floor lowered 1000 → 500, Firecrawl 1000 → 250.** Their free tiers
  grant 1000 *credits*/month, but the ledger debits 1 per *call* — and a call can
  cost more than 1 credit. The floor is now `credits ÷ the priciest call Nólë can
  issue`: Tavily an advanced search/extract is 2 credits → 1000 ÷ 2 = 500;
  Firecrawl search is 2 credits per 10 results, so a 20-result search (Service
  permits up to `maxSearchLimit=20`) is 4 credits → 1000 ÷ 4 = 250. The old
  `FreeQuota=1000` over-read remaining headroom up to 4× — the dashboard could hit
  zero while Nólë still reported room. Undercounting is the safe direction; the
  drift signal catches the rest. (Brave stays 1000: its $5 credit meters a uniform
  $0.005/query, so 1 call = 1 query = 1000-query floor.)
- **Brave metadata corrected to the Feb-2026 model.** Brave eliminated its flat
  free tier (2000, briefly 5000 queries/month) on 12 Feb 2026; the false "legacy
  accounts keep 2000/month" grandfathering claim is removed. The note now states
  the $5/month auto-renewing credit (~1000 queries at $0.005/query), the **50
  req/sec** Search-plan rate cap (was wrongly 1 req/sec — that was the eliminated
  legacy tier), the required public attribution, and the overage behaviour (past
  the $5 credit the card is billed unless you set a usage limit in the Brave
  dashboard — the biggest surprise-bill vector, and how you cap it).
- **Firecrawl "monthly vs one-time" hedge resolved.** Verified as 1000
  credits/month, reset monthly with no rollover — the prior "in flux" wording is
  gone.

### Fixed

- **Existing ledgers are corrected on first load, not next month.** When a
  persisted current-month entry was sized for the old 1000 floor,
  `mergeLedgerEntries` now re-bases its `free_remaining` on calls already consumed
  against the new (lower) floor, instead of inheriting the stale counter until the
  next monthly rollover. Without it, an upgrading user would keep over-reading
  their Tavily/Firecrawl headroom for the rest of the month — the exact over-read
  this release exists to eliminate. The re-base fires on same-cost-class loads
  AND across a `NOLE_<PROVIDER>_PAID` toggle (so disabling paid mode after upgrade
  cannot inherit the stale counter), is persisted on first load so the on-disk
  ledger self-heals, is idempotent, and only ever lowers the counter. Caught by
  Codex review.

### Notes

- Verified against official sources (brave.com/search/api, api-dashboard pricing,
  docs.tavily.com, firecrawl.dev/pricing) via a grounding workflow with adversarial
  cross-check. DDGS and Scrapling were re-verified too: Nólë's DDGS provider is
  pure-Go (POSTs `html.duckduckgo.com` directly, already handles HTTP 202), so the
  upstream `duckduckgo_search`→`ddgs` PyPI rename does not affect it; Nólë's
  Scrapling subprocess script is written defensively (no removed `css_first`/
  `xpath_first`, `getattr` fallbacks, fails closed on `follow_redirects`) and is
  compatible with Scrapling v0.4.x. No code change needed for either.

## [0.7.0] - 2026-06-01

Theme: **make the center trustworthy** — every number Nólë reports about money
and health is now labelled true, estimated, or unknown. No new MCP tools; this
release adds fields to the existing `provider_status`/`budget_status` envelopes
and turns `/health` into a real readiness check.

### Added

- **Drift signal.** When a provider rejects a call as over-quota (HTTP 429)
  while Nólë's local free-tier counter still shows room, `budget_status` now
  reports it (`has_drift`, `drift_signals[]`) and the affected provider carries a
  `drift_warning` in `provider_status`. It is mechanical observability — Nólë
  never debits on it, never reorders routes from it, and never judges provider
  health. Signals persist across restarts (union-merged under the ledger file
  lock) and age out of output after 24h. It is a best-effort EARLY signal: once
  repeated 429s trip the circuit breaker, calls short-circuit and drift stops.
- **Circuit-breaker state in `provider_status`.** Breakered providers now report
  `breaker_state` (`closed`/`open`/`half-open`), `breaker_consec_fails`, and
  `breaker_opened_at` (RFC3339, while open) — raw signals for the agent to reason
  about, with no Nólë-computed recovery ETA. A provider that is currently
  short-circuiting also reports `available: false` with `reason: circuit_open`.
- **Honest per-provider quota metadata.** Each BYOK entry now carries a
  `metering_model` (`credit-based` for all three today) and `budget_status`
  states up front that `free_remaining` is Nólë's own issued-request estimate,
  not a live provider-dashboard balance (`estimate_note`). Per-provider notes
  spell out the real metering caveats (Brave's Feb-2026 credit/metered-billing
  change + 1 req/sec cap; Tavily's per-credit search/extract cost; Firecrawl's
  monthly-vs-one-time ambiguity — verify your dashboard).
- **Cost-cap clarity.** `budget_status` exposes `hard_cap_source`
  (`explicit`/`unset`), and `nole doctor` now says loudly when `cost-capped` is
  set without `NOLE_HARD_CAP_CENTS` — premium providers are blocked until you set
  it. Nólë never authorizes an unrequested default spend (it stays fail-closed).

### Changed

- **`/health` is now a real readiness check.** It returns `200 {"status":"ready",
  ...}` iff at least one search-capable provider is available and allowed by the
  cost policy, else `503 {"status":"not_ready", "reason": ...}`. The body shape
  changed from `{"status":"ok"}` to `{status, timestamp, reason?,
  available_providers}`. Because keyless DDGS is always available, a zero-key
  deployment is correctly "ready". Readiness is orthogonal to budget — a hard-cap
  hit is a `/api/budget` concern, not a health one.

### Notes

- No MCP tool was added, renamed, or removed; the tool set is unchanged
  (`search, extract, search_and_extract, provider_status, budget_status,
  research`). Existing free-tier quota numbers are unchanged (1000/month per BYOK
  provider) — verified against current provider pricing as the honest fail-safe
  floor; the honesty work is metadata + the drift signal, not new integers.

## [0.6.0] - 2026-06-01

### Added

- `search_and_extract` — a combined primitive (MCP tool + `POST
  /api/search_and_extract`) that searches, then extracts the top result(s) in a
  single call, collapsing the common search-then-read round-trip. `extract_top`
  (default 1, max 3) controls how many top results are read; a per-URL extract
  failure is non-fatal and recorded in `extract_errors`.
- `research` is now reachable by agents — an MCP `research` tool and `POST
  /api/research` expose the multi-step search→extract pass, returning the
  deduplicated sources and extracted content (the same pipeline `nole research`
  uses).
- `include_trace` (default false) on the MCP/REST `search` and `extract`
  surfaces to opt back into the full per-attempt `route_trace`.

### Changed

- **Semi-breaking:** the MCP/REST `search` and `extract` success responses now
  OMIT `route_trace` by default (the compact `routing_insight` is still always
  present). Opt back in with `include_trace: true`. The CLI is unchanged —
  `--insight off|compact|verbose` still governs it there.
- **Semi-breaking:** `research` (CLI, MCP, and REST) no longer returns a composed
  `summary`/answer. It returns evidence — sources + extracts — for the calling
  agent to synthesize; the `synthesizeSummary` string-concatenation was removed.

### Notes

- The research pipeline moved into the core package so the MCP and REST surfaces
  can share it. A degraded research sub-step now logs to the `nole serve` /
  `nole mcp` server's stderr (it previously logged from the CLI process).

## [0.5.0] - 2026-06-01

### Added

- Task-fit routing now fires on every search. Previously the deterministic
  multi-intent planner ran only in the `classify` / `route-plan` inspection
  commands; real searches defaulted to the generic provider route. Now
  `Service.Search` auto-classifies the query when no task is supplied (an
  explicit `--task` / `task` argument always wins), so the news/docs/code/
  academic/pricing routes actually apply. The response reports a `task_source`
  (`supplied` / `detected` / `default`) so an agent can see whether it drove the
  route or Nólë inferred it. The planner remains pure deterministic keyword
  matching — no LLM, no quality judgment.
- Provider relevance and recency signals are passed through to the agent.
  `SearchResult` now carries optional `score` (provider-native relevance, e.g.
  Tavily's) and `published_at` (publication date) where the provider supplies
  them — verbatim, never computed or fabricated, omitted when absent. For
  `news` / `factcheck`, results are stably ordered newest-first using those
  dates: a pass-through of the provider's freshness signal, not a quality
  judgment (`score` is never sorted or filtered on).
- Task-aware request shaping: `news` / `factcheck` searches send a conservative
  last-month freshness window to providers that support it (Brave `freshness`,
  Tavily `topic`/`time_range`, Firecrawl `tbs`); every other task sends an
  unchanged request.
- The MCP `search` tool's `task` parameter is now a documented enum with an
  "auto-detected if omitted" note; unknown or aliased values (e.g. `community`
  → social) are normalized server-side (on MCP and REST) rather than erroring.

### Changed

- `nole research` classifies the question to drive its multi-step fan-out
  instead of a fixed `[general, research, docs]` list (whose routes overlapped
  and self-deduped).

### Notes

- Freshness-coverage caveat: Firecrawl leads the `news` / `factcheck` routes,
  but its web-source results carry no per-result date in this release, so the
  newest-first ordering is a no-op until the chain falls through to Brave
  (`page_age`) or Tavily (`published_date`). Per-result news dates from Firecrawl
  are a planned follow-up.

## [0.4.0] - 2026-05-31

### Added

- Per-provider circuit breaker for the remote API providers (Brave, Tavily,
  Firecrawl). After a configurable number of consecutive failures a provider's
  breaker opens and calls short-circuit immediately (no burned timeout, no quota
  debit), then admit one half-open probe per cooldown to recover. It uses a
  generation/epoch model so a slow call admitted in a previous regime can never
  be mis-attributed to the recovery probe; it trips on 5xx/429/408, transport
  errors, and client timeouts (a hung upstream), and never on 4xx or caller
  cancellation. In-memory and per-process (benefits the long-lived `nole serve`
  / MCP server). The keyless DDGS fallback and the local Scrapling extractor are
  intentionally left unbreakered so the free last-resort path is never
  short-circuited. Tunable via `NOLE_BREAKER_THRESHOLD` (default 5) and
  `NOLE_BREAKER_COOLDOWN_MS` (default 30000).
- Native fuzz targets for the SSRF preflight (`FuzzValidateURL`), the DDGS HTML
  sanitizer (`FuzzCleanHTML`), and the bounded body readers
  (`FuzzDecodeJSONLimited`); their seed corpora run as part of the normal test
  gate. Direct REST-handler tests (`buildMux`) and a quota persist-failure
  rollback regression test were also added.

### Changed

- Ctrl-C / SIGTERM now cancels in-flight work for `search`, `extract`, and
  `research` instead of hard-killing mid-request: a signal-aware root context is
  threaded into the providers, the DNS preflight resolves on that context (so a
  slow/wedged resolver is interruptible), and a second interrupt force-exits
  during a slow shutdown. `nole mcp` and `nole serve` use the same root context
  (no nested signal handlers).
- Cache eviction is now deterministic when entries share a timestamp: a
  monotonic insertion sequence breaks ties instead of relying on map iteration
  order.
- The Firecrawl search adapter clamps the result limit to [1,20] like Brave and
  Tavily (defense-in-depth for direct construction).
- The `research` report's `providers_used` list is sorted for stable output.

### Fixed

- `nole research` now surfaces a cancellation instead of swallowing it into a
  partial report with a success exit code.

### Security

- The SSRF preflight now decodes IPv4 addresses embedded in IPv6 transitional
  forms — IPv4-compatible (`::a.b.c.d`) and 6to4 (`2002::/16`) — and re-validates
  the embedded address, closing bypasses where a private/metadata IPv4 was
  smuggled in a v6 literal past `net.IP`'s classifiers. NAT64 keeps its wholesale
  `64:ff9b::/96` block; network-specific-prefix NAT64 is left to best-effort to
  avoid over-blocking legitimate public translations.

## [0.3.2] - 2026-05-31

### Security

- Local Scrapling extract no longer follows HTTP redirects past the SSRF
  preflight. The fetcher is invoked with redirect-following disabled; each
  redirect target is re-validated by `safenet.ValidateURL` (with a final-URL
  backstop for builds that ignore the no-follow request) before the next hop is
  fetched, and the walk is bounded to 5 hops. This closes the redirect-based
  SSRF-to-metadata / internal-host vector on the opt-in local-extract path. The
  redirect-disabled fetch + `status`/`Location` contract is verified live
  against Scrapling 0.4.8; the Go redirect-validation loop is unit-tested.

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

[Unreleased]: https://github.com/dorukardahan/nole/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/dorukardahan/nole/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dorukardahan/nole/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/dorukardahan/nole/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/dorukardahan/nole/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/dorukardahan/nole/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/dorukardahan/nole/compare/v0.2.4...v0.3.0
[0.2.4]: https://github.com/dorukardahan/nole/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/dorukardahan/nole/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/dorukardahan/nole/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dorukardahan/nole/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dorukardahan/nole/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dorukardahan/nole/releases/tag/v0.1.0
