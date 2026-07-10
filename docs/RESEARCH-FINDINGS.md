# Nólë improvement research findings (Phase 2)

Nólë is a free, local web search router for AI agents and coding CLI tools. MCP is an
entrypoint, not the whole product. This document records the evidence-cited research that
drives the Phase 3 plan and Phase 4 execution.

> **Method.** A dynamic verification workflow (106 sub-agents) took every improvement seed
> from `docs/ARCHITECTURE.md` (Phase 1) and ran each through a two-stage pipeline:
> **(1) verify** — an agent re-read the actual code at the anchor and judged whether the claim
> is true and *actionable within this effort's framing* (quality / stability / latency /
> correctness / coverage / platform — **not** risky taste refactors), proposing a minimal
> fix + verification method; **(2) adversarial refute** — a second agent tried to refute the
> verdict (is the bug real? would the fix break a test or invariant? is the code intentional?).
> Platform research (Gemini, Grok, Antigravity) was pinned to **primary sources only** (official docs +
> repository source via `gh api`), then independently re-derived by a skeptic. Cheat sheets and
> blog posts were explicitly rejected as sole evidence.
>
> **Honesty rules respected:** no fabricated platform support or benchmark numbers; the real
> Gemini/Grok clients were **not** launched in this environment; Antigravity was checked only with unauthenticated `agy --version/--help`, so their honest status is
> `repo-tested`, never `verified`. Verified facts and proposed/deferred work are separated below.

---

## 1. Platform coverage — Gemini, Grok, and Antigravity (verified config formats)

The Gemini and Grok schemas were confirmed at high confidence by an independent adversarial re-derivation
(researcher and skeptic agree). Antigravity facts were added later from Google's official Antigravity docs and an unauthenticated local `agy 1.1.1 --version/--help` check. Authenticated tool visibility was not observed here, so the honest support status is **`repo-tested`** (setup writer + config merge covered by Go tests).

### Antigravity CLI (`agy`, native Go CLI)

- **Global MCP config path:** `~/.gemini/config/mcp_config.json`
- **Workspace MCP config path:** `.agents/mcp_config.json`
- **Structure:** top-level **`mcpServers` object keyed by server name** (`$.mcpServers.<name>`).
- **Local stdio entry Nólë must write:**
  ```json
  { "mcpServers": { "nole": { "command": "/absolute/path/to/nole", "args": ["mcp"] } } }
  ```
- **Remote MCP entries:** require `serverUrl`; Nólë's setup writer is intentionally local stdio only and must preserve existing remote sibling entries.
- **Verification:** install via the official `agy` installer, then verify with Antigravity `/mcp` or an authenticated `agy -p` smoke. This run did not authenticate and did not observe tool visibility.
- **Implication:** the root schema is structurally compatible with the shared MCP JSON path, but Antigravity uses a specialized merge so existing per-server policy/options survive while only `command` and `args` are updated. It also needs its own flag and target path so Gemini CLI support is preserved separately.
- **Primary sources:** official Antigravity docs `https://antigravity.google/docs/cli/gcli-migration`, `https://antigravity.google/docs/cli/install`, `https://antigravity.google/docs/mcp`; local unauthenticated `agy 1.1.1` reported `--version` and `--help` successfully.

### Gemini CLI (`@google/gemini-cli`, repo `google-gemini/gemini-cli`)

- **User config path:** `~/.gemini/settings.json`
- **Structure:** top-level **`mcpServers` object keyed by server name** (`$.mcpServers.<name>`),
  shallow-merged. **Not an array.**
- **stdio entry Nólë must write:**
  ```json
  { "mcpServers": { "nole": { "command": "/absolute/path/to/nole", "args": ["mcp"] } } }
  ```
- **First-class MCP CLI:** yes — `gemini mcp add --scope user nole <bin> mcp`.
- **Implication:** structurally identical to Cursor/Windsurf — Nólë can reuse the existing
  `writeMCPJSONConfig` JSON writer with a new target path; sibling servers and unknown root keys
  are preserved by the same RawMessage merge.
- **Primary sources:** `packages/cli/src/config/settingsSchema.ts:161` (`mcpServers` type `object`,
  `default {} as Record<string,MCPServerConfig>`, `SHALLOW_MERGE`); `packages/cli/src/commands/mcp/add.ts`
  (`mcpServers[name]=newServer`); `packages/core/src/config/config.ts:477` (`MCPServerConfig`:
  `command?/args?/env?/cwd?` for stdio); `packages/core/src/config/storage.ts:54-79` +
  `packages/core/src/utils/paths.ts:13` (`GEMINI_DIR='.gemini'` → `~/.gemini/settings.json`);
  official `docs/cli/tutorials/mcp-setup.md`.

### Grok CLI (`@vibe-kit/grok-cli` / `grok-dev`, repo `superagent-ai/grok-cli`)

- **User config path:** `~/.grok/user-settings.json`
- **Structure:** top-level **`mcp` object** whose **`servers` is an ARRAY** of objects, each
  **keyed by an `id` field** (`$.mcp.servers[]`). This differs from every other Nólë writer.
- **stdio entry Nólë must write (upsert by `id`):**
  ```json
  { "mcp": { "servers": [ { "id": "nole", "label": "nole", "enabled": true,
    "transport": "stdio", "command": "/absolute/path/to/nole", "args": ["mcp"] } ] } }
  ```
- **First-class MCP CLI:** none observed (config via the `/mcps` TUI or direct JSON edit).
- **Implication:** needs a **new merge pattern** — read/create `mcp.servers`, find the element
  with `id == "nole"` and update in place (preserving unknown fields on that element), else append;
  preserve all other array entries and all other root keys.
- **Primary sources:** `src/utils/settings.ts:92-104` (`McpServerConfig{ id, label, enabled,
  transport:"http"|"sse"|"stdio", url?, headers?, command?, args?, env?, cwd? }`), `:106`
  (`McpSettings.servers?: McpServerConfig[]`), `:185-186` (`USER_DIR=join(homedir(),".grok")`,
  `USER_SETTINGS_PATH=join(USER_DIR,"user-settings.json")`), `:651` (`saveMcpServers` writes
  `{ mcp: { servers } }`); repo default branch `main`, pushed 2026-05-26.

> **Why cheat sheets were rejected:** secondary sources described Grok's MCP block as a flat
> `mcpServers` object (Gemini-style). The repository source proves it is an `mcp.servers` array
> keyed by `id`. Copying the blog shape would have produced a config the real client ignores.

---

## 2. Verified actionable findings (survived adversarial review)

27 of 59 seeds remained actionable after the refute pass. Priority/risk are the post-adversarial
values. "This run" marks items scheduled for Phase 4 execution; "Proposed" items are documented
with rationale but deferred (they are real but lower-value, environment-blocked, or need wider
design — never shipped untested).

| # | Anchor | Area | Pri | Risk | Verdict | Disposition |
|---|--------|------|-----|------|---------|-------------|
| 1 | `internal/providers/providerhttp/retry.go:52` | stability | P2 | low | confirmed | **This run** — retry transport errors (respect ctx + MaxAttempts) |
| 2 | `internal/providers/ddgs/ddgs.go:100` | correctness | P2 | low | confirmed | **This run** — pair snippet to following link, fix ad-skip misalignment |
| 3 | `internal/providers/tavily/tavily.go:123` (+ firecrawl/ddgs/research) | correctness | P2 | low | confirmed | **This run** — rune-safe truncation helper, replace 4 byte-slices |
| 4 | `internal/safenet/url_test.go:76` | coverage | P2 | low | confirmed | **This run** — add IPv6 SSRF block tests |
| 5 | `internal/providers/providerhttp/retry.go:121` | correctness | P3 | low | partial | **This run** — retry 408 (align with `transient` category) |
| 6 | `internal/version/version.go:5` + `root.go` | quality | P3 | low | confirmed | **This run** — `nole version` command consumes Commit/Date + stamps ldflags |
| 7 | `internal/core/quota.go:645` | quality | P3 | low | partial | **This run** — use `err` for observability without leaking into the persisted warning |
| 8 | `internal/safeerr/safeerr_test.go:8` | testing | P3 | low | confirmed | **This run** — direct `Message()` tests (nil-guard + HTTPStatusError branch) |
| 9 | `internal/core/service_test.go:257` | coverage | P3 | low | confirmed | **This run** — multi-provider quota-after-empty regression test |
| 10 | `internal/mcpserver/tools.go:172` | latency | P2 | medium | partial | Proposed — second provider-status pass on first search; needs care re: SetupTip contract |
| 11 | `internal/core/cache.go:22` | stability | P3 | low | confirmed | Proposed — add a max-entries bound to the in-process cache |
| 12 | `internal/cli/app.go:38` | stability | P3 | low | partial | Proposed — surface `registry.Register` errors to `doctor`/stderr |
| 13 | `internal/core/planner.go:71` | coverage | P3 | low | partial | Proposed — classification has no rules for `semantic`/`extract` (behavior change) |
| 14 | `internal/core/quota.go:598` | correctness | P3 | low | partial | Proposed — guard `PeriodStart > now` (future-dated/clock-skew ledger) |
| 15 | `internal/core/quota.go:449` | stability | P3 | low | partial | Proposed — document `(ledger, nil)`-but-degraded constructor contract |
| 16 | `internal/core/quota.go:188` | correctness | P3 | low | partial | Proposed — characterization test for cross-process refresh edge |
| 17 | `internal/cli/research.go:138` | latency | P3 | low | partial | Proposed — per-URL extract timeout in `research` |
| 18 | `internal/cli/research.go:132` | correctness | P3 | low | partial | Proposed — case-insensitive / query-aware extractable-URL filter |
| 19 | `internal/cli/setup_local_extract_test.go:63` | platform | P3 | low | partial | Proposed — Windows local-extract test (cannot exercise here) |
| 20 | `internal/cli/setup_local_extract.go:205` | platform | P3 | low | partial | Proposed — Windows wrapper variant (cannot verify here) |
| 21 | `internal/cli/bench.go:209` | quality | P3 | low | partial | Proposed — document the three score scales |
| 22 | `internal/bench/comprehensive.go:85` | stability | P3 | low | partial | Proposed — `incomplete` flag on truncated comprehensive runs |
| 23 | `cmd/bench/main.py:232` | coverage | P3 | low | partial | Proposed — wire or retire the orphaned Python runner |
| 24 | `scripts/verify-integration-evidence.sh:39` | stability | P3 | low | partial | Proposed — presence-based MCP tool detection |
| 25 | `scripts/check-docs-framing.sh:9` | docs | P3 | low | partial | **This run (folded into platform docs)** — add missing siblings to `required` |
| 26 | `scripts/secret-scan.sh:56` | security | P3 | low | partial | Proposed — high-confidence credential-prefix override of the `<12` whitelist |
| 27 | `internal/core/service.go:57` | quality | P3 | low | partial | Proposed — de-duplicate Search/Extract loop (large surface; defer) |

## 3. Dropped or corrected claims (verified-vs-proposed honesty)

The adversarial pass **rejected or de-scoped** the following Phase-1 seeds. Recording them
prevents re-litigating settled points and documents one outright-wrong seed.

- **`comprehensive.go:193` — 202→rate_limited is CORRECT, not a bug.** HTTP `202` is DuckDuckGo's
  documented rate-limit signal (`ddgs.go:55,68`); mapping it to `rate_limited` is intentional and
  test-pinned. The Phase-1 rationale ("202 Accepted is not a rate-limit signal") was wrong for this
  codebase. **No change.**
- **`app.go:120` — redundant ledger branch:** real but pure style; `NewFileQuotaLedgerWithPolicy`
  always returns a non-nil ledger, so the branch is harmless dead code with no functional benefit.
  Not worth churn. **No change.**
- **`router.go` / `service.go` — `Router.Select` dead in production / `routeFor` duplication:**
  originally deferred as a taste refactor; later resolved by adding `Router.Route` and
  `Router.Candidate`, then making `Service.Search`/`Extract` consume the shared lazy
  candidate path while keeping `Router.Select` as a compatibility wrapper. **Resolved after this scan.**
- **`brave.go:140` `clampMin` misnomer, `bench.go:104` hand-rolled sort, `service.go:57` Search/Extract
  duplication, `research.go:151` magic numbers, `http.go:100` ignored encode errors, `search.go:26`
  insight ordering, `file_lock_windows.go:21` "non-blocking" lock, `safeerr.go:14/22` Redact scope:**
  each verified as either behavior-preserving cosmetics, intentional design, or unreachable in
  practice. **No change** (some retained as Proposed notes only).

---

*Generated by the Phase 2 verification workflow. All anchors are file:line at time of writing;
verify before relying on stale anchors. No provider keys, tokens, or raw payloads appear in this
document.*
