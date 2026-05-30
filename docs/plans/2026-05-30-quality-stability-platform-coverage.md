# Plan: quality, stability, latency & platform-coverage improvements (2026-05-30)

Nólë is a free, local web search router for AI agents and coding CLI tools. This plan turns the
adversarially-verified findings in `docs/RESEARCH-FINDINGS.md` (Phase 2) into scoped, reversible,
test-backed changes. It adds first-class `nole setup` support for **Gemini** and **Grok** and lands
a set of correctness/stability/coverage wins, while keeping the route matrix frozen and the local
gate green after every change.

- **Branch:** `feat/quality-stability-platform-coverage` (off `main`).
- **Baseline:** local gate green at start (`go test ./...`, `go vet`, `gofmt -l` empty,
  `doctor`, `doctor --mcp`, `bench --json/--evidence-md`, `git diff --check`,
  `check-docs-framing.sh`, `check-benchmark-claims.sh` all pass).
- **Discipline:** behavior changes are TDD (write the failing test first, watch it fail, implement).
  One logical change per commit, conventional-commit messages. Run the full gate after each commit.
  Pushing/PRs are out of scope for this run.

## Non-negotiable invariants (must not regress)

- **Money-safety:** quota is debited only on a successful provider response (`service.go:112,205`).
- **Route matrix frozen** (`router.go:5`): no reorder without sanitized evidence.
- **MCP stdout is JSON-RPC only**; logs/warnings go to stderr.
- **Secret hygiene:** never print keys/tokens/headers/raw payloads or private paths; the persisted
  `LedgerWarning` must stay free of OS-error text and paths (`file_quota_test.go:146-158`).
- **Config writers** preserve unknown fields, keep `.bak` backups, never widen permissions.

---

## Phase A — Platform coverage (Gemini + Grok) — headline deliverable

### A1. `nole setup --gemini` writer
- **Rationale:** Gemini CLI reads `~/.gemini/settings.json` with a top-level `mcpServers` **object**
  keyed by server name — structurally identical to Cursor. (Source: `RESEARCH-FINDINGS.md §1`.)
- **Change:** add `writeGeminiConfig`/`writeGeminiConfigPath` reusing `writeMCPJSONConfig`
  (target `~/.gemini/settings.json`); add `--gemini` flag, `--all` wiring, description + error strings.
- **Files:** `internal/cli/setup.go`.
- **Risk:** low (reuses the audited JSON merge path).
- **Verification (TDD):** new tests in `setup_writers_test.go` — bare + wrapper entry shape, preserve
  unknown root + sibling servers, `.bak` + perms, idempotency (extend `TestSetupWritersIdempotent`),
  and `TestSetupGeminiFlagWritesUserScopeFile`. `go test ./internal/cli/...`.

### A2. `nole setup --grok` writer (new array-upsert-by-id pattern)
- **Rationale:** Grok CLI reads `~/.grok/user-settings.json` with `mcp.servers` as an **array** of
  objects keyed by an `id` field (Source: `RESEARCH-FINDINGS.md §1`) — a shape no existing writer handles.
- **Change:** add `writeGrokConfig`/`writeGrokConfigPath` that reads the JSON root (preserving unknown
  root keys), reads/creates `mcp` → `servers` array, upserts the element with `id=="nole"` in place
  (preserving unknown fields on that element) or appends, then writes atomically with backup/perms.
  Entry: `{id,label,enabled:true,transport:"stdio",command,args}` (wrapper mode: `command=wrapper,args:[]`).
  Add `--grok` flag, `--all` wiring, description + error strings.
- **Files:** `internal/cli/setup.go`.
- **Risk:** medium (new merge logic) → mitigated by thorough tests.
- **Verification (TDD):** `setup_writers_test.go` — array upsert (update existing `nole` in place),
  append when absent, preserve other array entries + unknown per-entry fields + unknown root keys,
  `.bak` + perms, wrapper mode, idempotency; `TestSetupGrokFlagWritesUserScopeFile`. `go test ./internal/cli/...`.

### A3. Client docs, support matrix, framing guard, AGENTS, CHANGELOG
- **Rationale:** mirror existing platforms; keep status truthful (`repo-tested`, since the real
  clients are not launched here).
- **Change:** add `docs/CLIENTS/gemini.md` and `docs/CLIENTS/grok.md` (each carrying
  `Status: repo-tested`, config path, exact entry, verification checklist, precise limitation that
  live tool-visibility was not observed in this environment); add Gemini + Grok rows to
  `docs/CLIENTS/README.md` support matrix following its status-label rules; add `gemini.md`/`grok.md`
  (and the sibling `LIVE-VERIFICATION.md`, finding #25) to the `required=()` array in
  `scripts/check-docs-framing.sh`; extend the AGENTS.md client/setup lists; add a CHANGELOG entry.
- **Files:** `docs/CLIENTS/gemini.md`, `docs/CLIENTS/grok.md`, `docs/CLIENTS/README.md`,
  `scripts/check-docs-framing.sh`, `AGENTS.md`, `CHANGELOG.md`.
- **Risk:** low. **Verification:** `./scripts/check-docs-framing.sh` (status-label loop covers the new
  docs) + `./scripts/check-benchmark-claims.sh` stay green.

## Phase B — Correctness & resilience (P2 + adjacent, all TDD)

### B1. Retry transport errors + align 408 (findings #1, #5)
- **Rationale:** `DoWithRetry` returns on the first transport error (connection reset/DNS blip),
  never retrying despite `MaxAttempts>1`; and `408` is labelled `transient` but not retried — the
  label and policy disagree. Both reduce resilience of the fallback router.
- **Change:** on transport error, retry while `ctx` is live and `attempt<MaxAttempts` (backoff via
  `retryDelay(http.Header{},…)`), else return; add `http.StatusRequestTimeout` to `isTransientStatus`.
- **Files:** `internal/providers/providerhttp/retry.go` (+ `retry_test.go`).
- **Risk:** low (quota debits only on `StatusOK`, so retrying cannot double-charge; replay-safe via
  `req.GetBody`; `MaxDelay` caps latency).
- **Verification (TDD):** `TestDoWithRetryRetriesTransportErrorThenSucceeds`,
  `TestDoWithRetryTransportErrorRespectsMaxAttempts`/`ctx`, `TestDoWithRetryRetriesRequestTimeout`.

### B2. DDGS snippet/ad-link misalignment (finding #2)
- **Rationale:** snippets are zipped to links by a counter that only advances on kept links; a
  skipped ad row can shift every subsequent snippet onto the wrong result.
- **Change:** index-based matching — pair each kept link with the first `result__snippet` whose match
  offset falls between this link and the next link.
- **Files:** `internal/providers/ddgs/ddgs.go` (+ `ddgs_test.go`).
- **Risk:** low (single provider; regex-scrape contract unchanged otherwise).
- **Verification (TDD):** `TestDDGSAdSnippetAlignment` with a leading `result--ad` block + two organic
  results; existing ddgs tests stay green.

### B3. Rune-safe snippet truncation (findings #3, #27-partial)
- **Rationale:** `snippet[:300]` byte-slices in `tavily.go`, `firecrawl.go`, `ddgs.go`, `research.go`;
  a cut mid-rune yields invalid UTF-8 (mojibake) on the non-ASCII content common in web results.
- **Change:** add `core.TruncateRunes(s string, max int) string` (rune-boundary safe) and replace the
  four byte-slice sites; this also de-duplicates the copy-pasted block.
- **Files:** `internal/core/types.go` (+ `types_test.go`), `tavily.go`, `firecrawl.go`, `ddgs.go`,
  `internal/cli/research.go`.
- **Risk:** low (output changes only when content was previously cut mid-rune).
- **Verification (TDD):** `TestTruncateRunes` (ASCII >max, multibyte stays `utf8.ValidString`, <=max
  unchanged); existing provider snippet assertions stay green.

## Phase C — Quality, observability & coverage (low-risk, additive)

### C1. `nole version` command + stamp Commit/Date (finding #6)
- **Rationale:** `version.Commit`/`Date` are declared but never consumed or stamped (dead), and there
  is no CLI way to query the binary version. A `version` command fixes both at once.
- **Change:** add `newVersionCommand()` printing `Version`/`Commit`/`Date`; register at `root.go`;
  extend `scripts/check-release-builds.sh` ldflags to stamp `Commit` (`git rev-parse --short HEAD`) and
  `Date` (UTC). Honest output: shows `unknown` until a stamped build runs.
- **Files:** `internal/cli/version.go` (new), `internal/cli/root.go`, `scripts/check-release-builds.sh`.
- **Risk:** low (additive). **Verification (TDD):** `TestVersionCommandPrintsBuildInfo`.

### C2. `markUnavailableLocked` uses its `err` for observability (finding #7)
- **Rationale:** the function ignores its `err` argument; callers pass real I/O errors that vanish.
- **Change:** use `err` for a sanitized, non-persisted diagnostic seam **without** folding raw OS-error
  text or paths into the persisted `LedgerWarning` (preserving `file_quota_test.go:146-158`).
- **Files:** `internal/core/quota.go` (+ `file_quota_test.go`).
- **Risk:** low. **Verification (TDD):** `TestMarkUnavailableDoesNotLeakErrorIntoWarning`.

### C3. Test coverage: `safeerr.Message`, IPv6 SSRF, multi-provider quota (findings #8, #4, #9)
- **Rationale:** `safeerr.Message` (8 call sites) has no direct test; SSRF tests are IPv4-only;
  no test asserts that after an empty-results provider only the *successful* provider's quota debits.
- **Change:** add tests only (zero behavior change): `safeerr_test.go` (nil-guard +
  `HTTPStatusError` bypass-Redact branch + redaction), `url_test.go` (`::1`, `fc00::/7`, `fe80::/10`,
  `[::ffff:169.254.169.254]` blocked), `service_test.go` (empty-then-success quota accounting).
- **Files:** `internal/safeerr/safeerr_test.go`, `internal/safenet/url_test.go`,
  `internal/core/service_test.go`.
- **Risk:** none (additive tests; raises coverage). **Verification:** `go test ./...`.

---

## Proposed / deferred (documented, not executed this run)

Real but lower-value, environment-blocked, or needing wider design. Listed so they are not lost and
not silently dropped (see `docs/RESEARCH-FINDINGS.md §2` for anchors + rationale): #10 second
provider-status pass, #11 cache max-entries bound, #12 surface `registry.Register` errors, #13 planner
rules for `semantic`/`extract` (behavior change), #14/#15/#16 quota internals, #17/#18 `research`
timeout & URL filter, #19/#20 Windows local-extract (cannot exercise here), #21 bench score docs,
#22 comprehensive `incomplete` flag, #23 orphaned Python runner, #24 verify-evidence tool detection,
#26 secret-scan prefix override, #27 Search/Extract loop de-duplication.

## Gate (run after every change)

```
gofmt -w $(git diff --name-only -- '*.go'); go test ./...; go vet ./...;
go run . doctor; go run . doctor --mcp; go run . bench --json; go run . bench --evidence-md;
git diff --check; ./scripts/check-docs-framing.sh; ./scripts/check-benchmark-claims.sh
```

A red gate blocks further progress until fixed. Coverage must not decrease; every behavioral change
ships with a regression test.
