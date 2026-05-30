# Nole architecture and dependency map

> Phase 1 mapping. Anchors are file:line at time of writing; verify before relying on stale anchors.

Nole is a free, local web search router for AI agents and coding CLI tools. It is BYOK and free-tier-first: it routes a search/extract request to the best available provider under a spend-safe quota policy, without making any hidden paid calls. The router core is deterministic and LLM-free. MCP is one entrypoint (the `mcp` stdio server and `serve --mcp` HTTP server expose the same engine to coding agents), not the whole product — the same `core.Service` is reachable from the plain CLI subcommands (`search`, `extract`, `research`, etc.), an HTTP REST surface, and the bench harness.

---

## Binary and command tree

The process entrypoint is `main.go`, which builds the Cobra command tree and renders redacted errors to stderr (stdout stays clean on failure).

| Element | Anchor | Notes |
|---|---|---|
| `main` (process entry) | `main.go:11` | Builds root via `cli.NewRootCommand()`, executes, on error prints `safeerr.Message(err)` to stderr then `os.Exit(1)`. |
| `NewRootCommand` (cobra root) | `internal/cli/root.go:7` | `Use="nole"`, `SilenceUsage+SilenceErrors=true`. Registers all 11 subcommands. |

Subcommands (registered `internal/cli/root.go:14-24`):

| Subcommand | Constructor | Anchor | Role |
|---|---|---|---|
| `search` | `newSearchCommand` | `internal/cli/search.go:11` | Routed web search via `Service.Search`; text or `--json`, `--task`, `--limit`, `--insight`. |
| `classify` | `newClassifyCommand` | `internal/cli/plan.go:11` | JSON-only deterministic intent classification (`core.ClassifyQuery`), no provider calls. |
| `route-plan` | `newRoutePlanCommand` | `internal/cli/plan.go:44` | JSON-only deterministic route plan (`core.BuildRoutePlan`), no provider calls. |
| `extract` | `newExtractCommand` | `internal/cli/extract.go:11` | URL extraction via `Service.Extract` (SSRF-gated). |
| `research` | `newResearchCommand` | `internal/cli/research.go:15` | Multi-step Search+Extract pipeline, markdown synthesis, no LLM. |
| `bench` | `newBenchCommand` | `internal/cli/bench.go:21` | Offline/live/comprehensive eval + sanitized route-evidence. |
| `providers` | `newProvidersCommand` | `internal/cli/providers.go:10` | Provider status table / JSON via `Service.ProviderStatus`. |
| `doctor` | `newDoctorCommand` | `internal/cli/doctor.go:50` | Health + secret-presence + budget + optional `--mcp` smoke checks. |
| `mcp` | `newMCPCommand` | `internal/cli/mcp.go:9` | stdio MCP transport: `server.ServeStdio(mcpserver.New(...))`. |
| `serve` | `newServeCommand` | `internal/cli/serve.go:9` | HTTP server (requires `--mcp`); exposes `/mcp` JSON-RPC + REST + `/health`. |
| `setup` | `newSetupCommand` | `internal/cli/setup.go:39` | Writes MCP client configs for 7 agents + optional local Scrapling bootstrap. |

There is no `version` subcommand and no cobra `Version` field, even though `internal/version` exists and is build-stamped (improvement seed below).

---

## Package map

### Area: entry-cli — CLI entrypoint + composition root (`main`, `cli/app`, `cli/root`, `version`)

**Purpose.** Process entrypoint and dependency-composition root. `main.go` runs the Cobra tree and renders redacted errors to stderr. `cli/root.go` assembles the 11 subcommands. `cli/app.go` is the wiring layer that reads environment variables and constructs a fully-configured `core.Service` (provider registry, quota ledger, response cache, route matrix) plus shared JSON/insight/task-parsing helpers used by every subcommand. `internal/version` holds build-stamped identity vars consumed by the MCP server.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `main` | func | `main.go:11` | Entrypoint; builds root, executes, prints redacted error to stderr + `os.Exit(1)`. stdout clean on failure. |
| `NewRootCommand` | func | `internal/cli/root.go:7` | Root cobra.Command; registers all 11 subcommands. |
| `defaultService` | func | `internal/cli/app.go:24` | Central composition root: loads `.env`, builds `core.Registry`, registers real-or-mock providers + keyless ddgs/scrapling, assembles quota ledger + optional cache, returns `core.NewService(...)`. Discards every `registry.Register` error via `_ =`. |
| `defaultQuotaLedger` | func | `internal/cli/app.go:83` | Selects ledger backend: memory/off/none → in-memory; else file-backed at `NOLE_QUOTA_LEDGER_PATH` or default path, with memory fallback. Has a redundant error branch (lines 120-125). |
| `defaultQuotaLedgerPath` | func | `internal/cli/app.go:144` | Resolves on-disk ledger location; honors `XDG_STATE_HOME` only when absolute, else `~/.local/state/nole/quota-ledger.json`; "" when no home. |
| `providerQuotaEntry` | func | `internal/cli/app.go:205` | Maps provider+key-presence to a `core.QuotaEntry` cost class (DisabledNoKey / PremiumCapable / FreeTierBYOK). |
| `defaultCacheTTL` | func | `internal/cli/app.go:162` | Parses cache TTL from `NOLE_CACHE_TTL` or `NOLE_CACHE_TTL_SECONDS`; returns 0 (disabled) on parse failure / non-positive. |
| `parseTaskStrict` / `parseTask` | func | `internal/cli/app.go:357` | Strict (validation, plan.go) vs lenient (forgiving fallback to TaskGeneral, search.go) task-string parsing. `parseTask` at 349. |
| `buildCLIErrorWithInsightMode` | func | `internal/cli/app.go:268` | Builds `cliErrorEnvelope` JSON (operation, redacted error, defensively-copied route + route_trace, routing_insight). `buildCLIError` at 264. |
| `cliErrorEnvelope` | type | `internal/cli/app.go:256` | JSON error contract: operation, error, optional route, routing_insight, route_trace. |
| `writeJSON` / `writeJSONTo` | func | `internal/cli/app.go:246` | Shared 2-space-indented JSON encoders (stdout / any `io.Writer`). |
| `runSearch` | func | `internal/cli/app.go:388` | Thin search facade; rejects empty query, calls `defaultService().Search` with `context.Background()`. Builds a new Service per call. |
| `version` vars | var | `internal/version/version.go:3` | Build-stamped `Version`/`Commit`/`Date`. Only `Version` is read (`mcpserver/server.go:10`) and only `Version` is ldflag-stamped (`scripts/check-release-builds.sh:44`); Commit/Date are dead. |

**Data flow.** `os/shell` → `main.main` (`main.go:11`) → `cli.NewRootCommand().Execute()`. Cobra dispatches to one of 11 subcommands. Each calls `defaultService()` (`app.go:24`) → `loadDefaultNoleEnvFile()` reads provider keys → `core.NewRegistry()` + `registry.Register(real-or-mock)` → `defaultQuotaEntries` (`app.go:73`) → per-provider `providerQuotaEntry` (`app.go:205`) → `defaultQuotaLedger` (`app.go:83`) → `defaultResponseCacheFromEnv`/`defaultCacheTTL` (`app.go:155/162`) → `core.NewService(registry, ledger, core.DefaultRouteMatrix(), opts)`. Output flows through `writeJSON` or `applyXxxInsightMode` + `writeHumanRoutingInsight` (`app.go:290-347`). Errors bubble to `main`, get redacted by `safeerr.Message` (`safeerr.go:18`), print to stderr. `version.Version` flows separately into `mcpserver.NewMCPServer`.

### Area: core-routing — `internal/core` routing engine (route matrix, registry, deterministic planner, shared types, insight formatting)

**Purpose.** Deterministic, LLM-free decision core. Defines the task taxonomy and provider contracts (`types.go`), the evidence-derived task→provider route matrix + a quota-aware single-provider selector (`router.go`), a name-keyed provider registry (`registry.go`), a rule-based query classifier + route planner that produces routes without provider calls (`planner.go`), and the human-readable "Nole:" routing-insight string builders (`insight.go`). Separates "what provider should handle this" from actual provider I/O (which lives in `service.go`).

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `RouteMatrix` | type | `internal/core/router.go:3` | `map[TaskType][]string` — ordered fallback chain per task. |
| `DefaultRouteMatrix` | func | `internal/core/router.go:5` | Canonical per-task provider ordering (routing prior from benchmark evidence). Extract route excludes brave/ddgs, leads with local scrapling. |
| `Router` | type | `internal/core/router.go:31` | Holds registry + QuotaLedger + matrix; the decision unit. |
| `Router.Select` | method | `internal/core/router.go:41` | Walks the task route, skipping unregistered/incapable/disallowed providers; returns first allowed + full route, else `NoFreeQuotaError`. Only exercised by tests — `service.go` reimplements this loop. |
| `Registry` | type | `internal/core/registry.go:8` | `map[string]Provider` store. |
| `Registry.Register` | method | `internal/core/registry.go:16` | Rejects nil provider, empty name, duplicate names. |
| `Registry.List` | method | `internal/core/registry.go:36` | Providers sorted by name (deterministic). |
| `Provider` | interface | `internal/core/types.go:136` | Name, Capabilities, Search, Extract, Status. |
| `TaskType` / Task* consts | const | `internal/core/types.go:5` | 12 task types (general, news, docs, academic, factcheck, semantic, code, social, people, pricing, extract, research). |
| `RouteAttempt` | type | `internal/core/types.go:69` | Per-provider trace row (status, reason, cost policy/class, cache status, latency, count, cents) — observability backbone. |
| `HasCapability` | func | `internal/core/types.go:144` | Linear membership check over capability slice. |
| `TaskDescription` | func | `internal/core/types.go:163` | One-line description per task; covers all 12. |
| `plannerRules` | var | `internal/core/planner.go:71` | Weighted-phrase rule table. Covers 8 tasks; semantic and extract have NO rules. |
| `ClassifyQuery` | func | `internal/core/planner.go:102` | Deterministic classifier: honors override, else normalizes+scores rules, flags ambiguity on top-score tie, falls back to general. |
| `BuildRoutePlan` | func | `internal/core/planner.go:144` | Classifies then maps each intent to a route via matrix/override; emits `PlannedRoute` list + 'planned' RouteTrace, no provider calls. |
| `scoreIntents` | func | `internal/core/planner.go:173` | Sums weighted phrase hits, stable-sorts by score desc then `taskPriority` asc. |
| `normalizeQuery` / `containsPhrase` | func | `internal/core/planner.go:204` | Lowercase, strip non-alphanumeric to spaces, space-pad for whole-token matching. |
| `taskPriority` | func | `internal/core/planner.go:261` | Tie-break ordering over 10 tasks; TaskSemantic/TaskExtract absent → default 100. |
| `buildRuntimeRoutingInsight` | func | `internal/core/insight.go:117` | Central runtime search/extract insight formatter; derives provider/policy/cache/attempt summary from the trace. |
| `BuildRoutePlanRoutingInsight` | func | `internal/core/insight.go:68` | Summarizes a RoutePlan into a 'route-plan planned ...' line. |
| `ParseInsightMode` | func | `internal/core/insight.go:16` | Parses compact/off/verbose (empty → compact); ok=false on unknown. |

**Data flow.** Two surfaces. (1) Planning (no I/O): `BuildRoutePlan` (`planner.go:144`) → `ClassifyQuery` (`planner.go:102`) → `normalizeQuery`+`scoreIntents` over `plannerRules` → per-intent `routeForPlan` (`planner.go:234`) reads `RouteMatrix`/override → `PlannedRoute[]` + 'planned' RouteTrace → `BuildRoutePlanRoutingInsight`. (2) Selection: `Router.Select` (`router.go:41`) reads `matrix[task]` (TaskGeneral fallback), per provider calls `Registry.Get`, `HasCapability`, `ledger.Decide(name).Allowed` — first allowed Provider + route copy, else `NoFreeQuotaError`. At runtime `service.go` does NOT call `Router.Select`; it reads `s.router.matrix` directly (`service.go:260`) and reimplements the Decide/Record/trace loop, then feeds the accumulated RouteTrace into `BuildSearchRoutingInsight`/`BuildExtractRoutingInsight` (`insight.go:29,33`) → `buildRuntimeRoutingInsight`.

### Area: core-state — `internal/core` cost/quota ledger, response cache, BYOK metadata, setup hints, file locking, typed errors

**Purpose.** Implements the "no hidden paid spend" guarantee. The quota ledger classifies every provider into a cost class and decides, under a configurable cost policy (free-first / cost-capped / quality-first), whether a call is allowed, persisting free-tier counters and paid spend to a crash-safe, file-locked JSON ledger. Supporting pieces: TTL response cache, authoritative BYOK metadata slice, setup-hint builders, typed no-free-quota error. Survives corrupt ledgers, schema migration, multi-process races, month boundaries.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `MemoryQuotaLedger` | type | `internal/core/quota.go:112` | Core ledger struct (also backs file ledger). Mutex-guarded; `*Locked` methods assume the lock held. |
| `QuotaLedger` | interface | `internal/core/quota.go:74` | Public contract: Allow, Decide, Record, Get, Entries, BudgetStatus. |
| `decideLocked` | method | `internal/core/quota.go:193` | Policy engine; maps (CostClass × CostPolicy) → Allowed + Reason; honors `failClosedReason`. Default branch (285) coerces unknown classes. |
| `Record` | method | `internal/core/quota.go:297` | Atomic charge: file lock, re-read disk (defeats stale instances), decrement FreeRemaining / add SpentCents, persist. On reload failure marks unavailable + fails closed. |
| `recordLocked` | method | `internal/core/quota.go:312` | Refreshes expired monthly quotas, re-decides, mutates per cost class, persists with rollback (348). |
| `reloadFromDiskLocked` | method | `internal/core/quota.go:432` | Reads + validates JSON ledger (schema 1..2), merges, refreshes, migrates v1→v2, persists when changed. Missing file = fresh OK ledger. |
| `mergeLedgerEntries` | func | `internal/core/quota.go:519` | Merges disk onto seeds; drops orphans; carries counters across cost-class transitions; takes `max(SpentCents)`. |
| `refreshExpiredEntriesLocked` | method | `internal/core/quota.go:589` | Refills FreeRemaining for monthly entries whose PeriodStart < current YYYY-MM. |
| `persistLocked` | method | `internal/core/quota.go:609` | Crash-safe write: mkdir 0700, write .tmp 0600, rename, chmod 0600. Schema v2. |
| `withFileLockLocked` | method | `internal/core/quota.go:489` | Wraps fn in exclusive advisory lock on `<path>.lock`; no-op for in-memory. |
| `recoverCorruptLedgerLocked` | method | `internal/core/quota.go:472` | On parse error / bad schema: backup, RecoveredCorrupt state + `ledger_corrupt_fail_closed`, pathless warning, persist. |
| `normalizeQuotaEntry` | func | `internal/core/quota.go:673` | Infers CostClass, syncs flags, clamps negatives to 0. |
| `premiumWithinCap` | func | `internal/core/quota.go:702` | Cost-capped gate: blocks if no cap / unknown cost / total+estimated > HardCapCents. |
| `MemoryResponseCache` | type | `internal/core/cache.go:22` | Mutex-guarded TTL cache w/ injectable clock; lazy eviction on read; nil/ttl<=0 safe. |
| `searchCacheKey` | func | `internal/core/cache.go:112` | NUL-joined key from task + normalized query + limit; empty task → TaskGeneral. |
| `cloneSearchResponse` | func | `internal/core/cache.go:145` | Defensive deep-ish copy (Results/Route/RouteTrace) so callers can't mutate stored entries. |
| `lockLedgerFile` (unix) | func | `internal/core/file_lock_unix.go:10` | Blocking exclusive `flock(LOCK_EX)`; build-tag `!windows`. |
| `lockLedgerFile` (windows) | func | `internal/core/file_lock_windows.go:19` | `LockFileEx` `LOCKFILE_EXCLUSIVE_LOCK` (0x2) over 1 byte; build-tag `windows`. |
| `byokProviders` | var | `internal/core/byok_metadata.go:22` | Authoritative BYOK metadata (brave, tavily, firecrawl): free quotas, refresh windows, capabilities, signup URLs, env examples. Never mutate after init. |
| `BuildSetupSuggestions` | func | `internal/core/setup_hints.go:15` | One suggestion per missing BYOK key, classified high/medium/low, sorted. |
| `BuildSetupTip` | func | `internal/core/setup_hints.go:103` | Once-per-session upgrade nag from high/medium suggestions; nil when nothing missing or only low-impact. |
| `NoFreeQuotaError` | type | `internal/core/errors.go:5` | Typed no-free-provider error; `IsNoFreeQuota` handles value + pointer. |

**Data flow.** Wiring: `app.go:119` → `NewFileQuotaLedgerWithPolicy(path, policy, seeds)`; seeds built from `LookupBYOK` (`app.go:216`). Construction (`quota.go:408`) seeds map, runs `reloadFromDiskLocked` under lock. Hot path: `Decide(provider)` → mutex → `refreshExpiredEntriesLocked` → `decideLocked` (CostClass × policy) → QuotaDecision. On committed call: `Record(provider)` → file lock → `reloadFromDiskLocked` (re-read disk) → `recordLocked` decrements/adds → `persistLocked` (tmp+rename+chmod). Corruption → `recoverCorruptLedgerLocked` → backup + fail-closed (only keyless-free passes). Response cache sits in front in `service.go`: `GetSearch`/`GetExtract` short-circuit; `SetSearch`/`SetExtract` store clones after success; errors never cached. Setup hints read `BYOKProviders()` vs configured map → suggestions/tips surfaced via `ProviderStatusResponse` and first `SearchResponse`.

### Area: core-service — `internal/core` Service orchestration (Search/Extract/ProviderStatus/BudgetStatus)

**Purpose.** Central orchestrator turning a Search/Extract request into a routed, quota-aware, cache-aware provider call. Walks the per-task route applying capability checks, cost-policy quota decisions, and availability gates; falls back across providers on failure/empty; emits a per-attempt RouteTrace + human-readable RoutingInsight. Owns the money-safety invariant: quota is debited only on a successful response.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `Service` | type | `internal/core/service.go:12` | Orchestrator: registry, ledger, router, optional cache. |
| `NewService` | func | `internal/core/service.go:27` | Builds Service + internal Router; applies variadic nil-guarded `ServiceOption`. |
| `WithResponseCache` | func | `internal/core/service.go:21` | Option injecting a ResponseCache (otherwise nil/skipped). |
| `Service.Search` | method | `internal/core/service.go:37` | Defaults Task→TaskGeneral, checks cache, iterates route w/ registration/capability/quota/availability gates, calls `provider.Search`, falls back on error/empty, records quota only on success, returns response+trace or `NoFreeQuotaError`. |
| `Service.Extract` | method | `internal/core/service.go:138` | Mirrors Search: trims URL, defaults Format→markdown, runs `safenet.ValidateURL` (SSRF guard) before routing, same pipeline on TaskExtract route. |
| `Service.ProviderStatus` | method | `internal/core/service.go:231` | Lists providers, calls Status each, merges quota decision, computes BYOK suggestions. |
| `Service.BudgetStatus` | method | `internal/core/service.go:255` | Thin delegate to `ledger.BudgetStatus()`. |
| `Service.routeFor` | method | `internal/core/service.go:259` | Resolves route by reaching into `s.router.matrix` directly, TaskGeneral fallback. |
| `attemptWithDecision` | func | `internal/core/service.go:291` | Core RouteAttempt builder folding QuotaDecision + latency + count. |
| `cacheHitAttempt` | func | `internal/core/service.go:275` | Builds cache_hit attempt; provider name defaults to 'cache'. |
| `mergeProviderCostStatus` | func | `internal/core/service.go:304` | Copies QuotaDecision cost/policy fields onto a ProviderStatus. |
| `contentResultCount` | func | `internal/core/service.go:315` | 1 for non-blank extract content, else 0. |

**Data flow.** Caller (CLI/MCP) → `Service.Search`/`Extract`. Both: (1) normalize request (Task default / URL trim + Format default + `safenet.ValidateURL` for Extract), (2) `routeFor` → `s.router.matrix[task]` w/ TaskGeneral fallback, (3) if cache present consult `cache.GetSearch`/`GetExtract`; hit → rebuild Route/RouteTrace via `cacheHitAttempt` + insight and return; miss → append cacheMiss. (4) Per provider: `registry.Get` → `skippedAttempt(not_registered)`; `HasCapability` → `missing_*_capability`; `ledger.Decide` → skipped w/ `premium_blocked_free_first`/`free_quota_exhausted`; `provider.Status(ctx)` → skipped if unavailable. (5) Call provider, measure latency; err → `provider_error`, continue; empty → `empty_results`/`empty_content`, continue. (6) On success ONLY: `ledger.Record(name)`; if Record errors, re-Decide and set `success_<reason>` but still return response. (7) Build trace + insight, optionally cache.Set, return. (8) Exhausted: `NoFreeQuotaError` or `lastErr`, both with `BuildErrorRoutingInsight`.

### Area: cli-commands — `internal/cli` command surface (search, extract, classify/route-plan, providers, doctor, research, HTTP serve, .env loader)

**Purpose.** Cobra command layer. Each file builds command constructors translating flags/args into `core.Service` calls or pure `core` planners, then renders text or JSON. Hosts the `serve` HTTP handler (REST + MCP JSON-RPC bridge), the `doctor` health/MCP-smoke diagnostic, the `research` pipeline, and the shell-style `.env` loader that primes provider keys before the service is constructed.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `newSearchCommand` | func | `internal/cli/search.go:11` | `search <query>`; `--task/--limit/--json/--insight`; renders text or JSON. |
| `taskHelpText` | func | `internal/cli/search.go:50` | Builds `--task` help from `core.TaskTypes()`, skipping TaskExtract. |
| `newExtractCommand` | func | `internal/cli/extract.go:11` | `extract <url>`; `Service.Extract` w/ `--format`; mirrors search error/JSON handling. |
| `newClassifyCommand` | func | `internal/cli/plan.go:11` | `classify <query>`; JSON-only `core.ClassifyQuery`. |
| `newRoutePlanCommand` | func | `internal/cli/plan.go:44` | `route-plan <query>`; JSON-only `core.BuildRoutePlan(DefaultRouteMatrix())`, no provider calls. |
| `planOptionsFromFlags` | func | `internal/cli/plan.go:79` | `--task/--providers/--single-intent` → `core.PlanOptions`; rejects TaskExtract override + invalid providers. |
| `validPlannerProvider` | func | `internal/cli/plan.go:122` | Allowlist: brave, tavily, firecrawl, ddgs. |
| `newProvidersCommand` | func | `internal/cli/providers.go:10` | `providers`; tab rows or JSON from `ProviderStatus`. |
| `newDoctorCommand` | func | `internal/cli/doctor.go:50` | `doctor`; binary/stdio notes, provider table, secret presence, paid warnings, budget, optional `--mcp`. |
| `writePaidModeWarnings` | func | `internal/cli/doctor.go:31` | Warnings for `NOLE_<P>_PAID` mode + Brave subscription/CC note. |
| `checkMCPStdioSmoke` | func | `internal/cli/doctor.go:165` | Redirects os.Stdout to pipe, builds `mcpserver.New`, asserts 0 stdout bytes at startup. |
| `checkMCPProtocolSmoke` | func | `internal/cli/doctor.go:229` | Spawns `<binary> mcp`, runs initialize + tools/list handshake (10s timeout), validates tools + JSON-only stdout. |
| `rpcIDMatches` | func | `internal/cli/doctor.go:421` | Matches a JSON-RPC id (number or numeric string) against wanted int. |
| `newResearchCommand` | func | `internal/cli/research.go:15` | `research <question>`; runs `researchPipeline` w/ `--max-steps` (default 3). |
| `researchPipeline` | func | `internal/cli/research.go:79` | Searches [general,research,docs], dedupes URLs, extracts up to 5 non-pdf/non-reddit sources (truncate 2000), synthesizes summary. |
| `synthesizeSummary` | func | `internal/cli/research.go:175` | Pure markdown builder (first paragraph, header-strip, 300-char cap); no LLM. |
| `newHTTPHandler` | func | `internal/cli/http.go:26` | Wraps `core.Service` + `mcpserver.New` via `buildMCPServer`. |
| `httpHandler.start` | method | `internal/cli/http.go:36` | Registers `/health`, `/mcp`, `/api/providers`, `/api/budget`, `/api/search`, `/api/extract` w/ 1MiB caps + slowloris timeouts. |
| `httpHandler.handleMCP` | method | `internal/cli/http.go:158` | POST-only JSON-RPC bridge → `mcp.HandleMessage`; `-32700`/`-32603` envelopes on failure. |
| `httpBuildContext` | func | `internal/cli/http.go:229` | Injects `InProcessSession` if `Mcp-Session-Id` present, else sets `mcpserver.EphemeralCtxKey`. |
| `httpSessionForRequest` | func | `internal/cli/http.go:247` | Legacy helper retained only for tests (deprecated path). |
| `loadDefaultNoleEnvFile` | func | `internal/cli/env_file.go:8` | Loads default `.env` (unless `NOLE_DISABLE_ENV_FILE`); called from `app.go:25` and `bench.go:118`. |
| `parseShellEnvAssignment` | func | `internal/cli/env_file.go:45` | Parses `KEY=VALUE` (supports `export `, comments, quotes); existing env wins. |
| `parseDoubleQuotedShellValue` | func | `internal/cli/env_file.go:121` | Double-quoted shell values w/ escapes + `os.ExpandEnv`, preserving `\$`. |

**Data flow.** Entry is `root.go:14-24`. Before any service call, `app.go:25` calls `loadDefaultNoleEnvFile()`. `search.go`/`extract.go` parse `--insight` (`app.go:282`), call `runSearch`→`Service.Search`/`Service.Extract`, apply `applyXxxInsightMode`, then `writeJSONTo` or `writeHumanRoutingInsight`. On error with `--json` they emit `buildCLIErrorWithInsightMode`. `plan.go` classify/route-plan call pure core planners (provider-free). `research.go` fans `Service.Search` across 3 task types, dedupes, `Service.Extract` on ≤5 URLs, `synthesizeSummary` (no model). `http.go` re-exposes Service over REST + MCP bridge. `doctor.go --mcp` runs `checkMCPStdioSmoke` (0 stdout) + `checkMCPProtocolSmoke` (subprocess handshake).

### Area: cli-setup — `nole setup` MCP client config writers + local-extract Scrapling bootstrap

**Purpose.** Implements `nole setup`, registering Nole as an MCP server across seven AI coding agents (Claude, Cursor, Codex, OpenCode, Kimi, Windsurf, Hermes) and optionally bootstrapping an isolated local Scrapling runtime. Each agent has a bespoke writer that merges Nole's entry into the agent's native user-scope config (JSON/TOML/YAML) preserving unknown fields, file permissions, a `.bak` backup, and writing atomically. `--local-extract` provisions a Python venv, installs `scrapling[fetchers]`, persists `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env`, and emits an env-sourcing shell wrapper.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `newSetupCommand` | func | `internal/cli/setup.go:39` | Builds `setup`; parses agent flags, resolves binary path, builds launchSpec, runs local-extract first, dispatches per-agent writers, prints count + key hints. |
| `launchSpec` | type | `internal/cli/setup.go:20` | `{Binary, Wrapper}`; `command()`/`args()` (25,32) return wrapper alone or bare binary + `[mcp]`. |
| `printClaudeInstructions` | func | `internal/cli/setup.go:204` | Claude is instruction-only: NO file; prints `claude mcp add nole -s user -- ...`. |
| `writeHermesConfigPath` | func | `internal/cli/setup.go:253` | YAML-node writer; backs up, preserves comments, upserts `mcp_servers.nole`, atomic-write preserving mode. |
| `upsertHermesNoleServer` | func | `internal/cli/setup.go:314` | Sets command+args, adds default timeout=120/connect_timeout=60 if missing, ensures tools policy. |
| `yamlMappingUpsert` | func | `internal/cli/setup.go:369` | Replace/append key in yaml.Node mapping, carrying Head/Line/Foot comments — comment-preserving idempotency core. |
| `writeMCPJSONConfig` | func | `internal/cli/setup.go:438` | Shared Cursor/Windsurf writer; preserves unknown server fields via RawMessage map, replaces only `nole`. |
| `writeCodexConfigPath` | func | `internal/cli/setup.go:472` | TOML writer via `upsertCodexTomlTable`; block by `codexMCPServerBlock` (489) embedding `/bin/sh -lc 'set -a; . .env; exec nole mcp'` unless wrapper set. |
| `upsertCodexTomlTable` | func | `internal/cli/setup.go:508` | Hand-rolled TOML table replacement; strips preceding comment/blank lines so marker doesn't accumulate. |
| `writeOpenCodeConfigPath` | func | `internal/cli/setup.go:566` | Merges into `mcp` JSON key with `{type:local, command:[bin,mcp], enabled, environment}` (591). |
| `writeKimiConfigPath` | func | `internal/cli/setup.go:639` | Merges into `mcpServers`; `{command}` in wrapper mode or `{command,args}` bare (663). |
| `atomicWriteFile` | func | `internal/cli/setup.go:768` | Durability primitive: temp file same dir, chmod, sync, close, rename, re-chmod; default mode 0600. |
| `configWriteMode` | func | `internal/cli/setup.go:759` | Preserves existing perms on rewrite, else 0600 (no widening secret configs). |
| `readExistingFileWithMode` | func | `internal/cli/setup.go:737` | Stat+read returning (bytes, exists, perm, err); ENOENT → exists=false. |
| `setupLocalExtract` | func | `internal/cli/setup_local_extract.go:26` | Bootstraps Scrapling runtime: resolve venv+python, gate >=3.10, create venv, verify-or-pip-install, persist `NOLE_SCRAPLING_PYTHON`. |
| `resolveSetupPython` | func | `internal/cli/setup_local_extract.go:92` | `--python` value or LookPath-probes `python3.13..3.10, python3, python`. |
| `upsertShellEnvAssignment` | func | `internal/cli/setup_local_extract.go:171` | Idempotent `KEY=value` upsert into `.env`; values shell-quoted. |
| `writeMCPWrapper` | func | `internal/cli/setup_local_extract.go:195` | Emits 0700 POSIX `/bin/sh` wrapper sourcing `.env` then exec'ing the binary (exit 127 if none). |
| `shellQuote` | func | `internal/cli/setup_local_extract.go:229` | Single-quote shell escaping for env values + wrapper binary path. |
| `venvPythonPath` | func | `internal/cli/setup_local_extract.go:133` | `Scripts/python.exe` on Windows, `bin/python` elsewhere. |

**Data flow.** `root.go` → `AddCommand(newSetupCommand)`. RunE (`setup.go:60`): `--all` sets all bools; reject if none + no `--local-extract`; `os.Executable` → `filepath.Abs` → `launchSpec`; validate wrapper absolute (78). If `--local-extract`: `setupLocalExtract` → resolve venv/python/version → venv create → verify-or-install Scrapling → `writeNoleEnvValue(NOLE_SCRAPLING_PYTHON)`; then default wrapper to `~/.local/bin/nole-mcp` + `writeMCPWrapper` (97-107). Per-agent dispatch: claude→`printClaudeInstructions` (no file, not counted); cursor/windsurf→`writeMCPJSONConfig`; codex→`writeCodexConfigPath`; opencode→`writeOpenCodeConfigPath`; kimi→`writeKimiConfigPath`; hermes→`writeHermesConfigPath`. Each: resolve home → native path → read existing → backup → merge nole entry preserving unknowns/comments → `atomicWriteFile` w/ `configWriteMode`. Writer errors go to stderr and skip the `configured++` increment (119-166).

### Area: mcp — MCP server surface (stdio + HTTP) and tool registration (`internal/cli/{mcp.go,serve.go}` + `internal/mcpserver/*`)

**Purpose.** Exposes the engine as a Model Context Protocol server over two transports: stdio (`nole mcp`) for local single-process agent usage, and Streamable-HTTP (`nole serve --mcp`) for team/remote usage. Constructs an `mcp-go` server, registers four tools (search, extract, provider_status, budget_status) wrapping `core.Service`, serializes responses/errors as JSON. Central concern: once-per-session emission of a setup_tip with transport-aware semantics (stdio one-per-process, HTTP persistent-per-session, HTTP ephemeral-always) and a bounded dedup map.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `New` | func | `internal/mcpserver/server.go:9` | Builds mcp-go MCPServer "nole" at `version.Version`, tool caps disabled, then `RegisterTools`. Shared by both transports. |
| `RegisterTools` | func | `internal/mcpserver/tools.go:66` | Registers four tools, binds handler closures over `*core.Service`; owns the per-server `tipState` dedup map (closure-captured). |
| `HasExtractCapableConfigured` | func | `internal/mcpserver/tools.go:49` | Reports any extract-capable provider configured locally; gates extract-tool advertisement (184). Exported so doctor mirrors it. |
| `hashSessionID` | func | `internal/mcpserver/tools.go:38` | SHA-256→hex of client session ID; used only as dedup-map key (anti-memory-inflation). |
| `tipStateMaxEntries` | const | `internal/mcpserver/tools.go:32` | Caps tip-dedup map at 1000; beyond cap new IDs fail-open without recording. |
| `EphemeralCtxKey` | type | `internal/mcpserver/session.go:19` | Empty-struct context key signaling a stateless HTTP request. Set in `http.go:233`, read in `tools.go:120`. |
| `toolErrorJSON` | func | `internal/mcpserver/errors.go:18` | Sanitized JSON error envelope (operation, `safeerr.Message`, route, insight, trace); falls back to hand-built JSON on marshal failure. |
| `toolErrorEnvelope` | type | `internal/mcpserver/errors.go:10` | Struct of the sanitized tool-error payload w/ omitempty. |
| `buildTaskDescription` | func | `internal/mcpserver/tools.go:229` | Generates the search tool's `task` description from `core.TaskTypes()`, skipping TaskExtract. |
| `newMCPCommand` | func | `internal/cli/mcp.go:9` | `nole mcp`; runs `server.ServeStdio(mcpserver.New(defaultService()))`. |
| `newServeCommand` | func | `internal/cli/serve.go:9` | `nole serve`; requires `--mcp`, builds `httpHandler`, starts it, prints `/mcp` + `/health` URLs. |
| `searchToolDescription` | const | `internal/mcpserver/tools.go:19` | Long NL description for the search tool (paired with `extractToolDescription` at 20). |

**Data flow.** Two transports converge on `mcpserver.New`. (1) stdio: `newMCPCommand` (`mcp.go:14`) → `mcpserver.New(defaultService())` → `RegisterTools` → `ServeStdio` reads JSON-RPC from stdin. (2) HTTP: `newServeCommand` (`serve.go:22`) → `newHTTPHandler(svc)` (`cli/http.go:26`) → `handler.start(addr)`; each request's `httpBuildContext` injects in-process session (header present) or sets `EphemeralCtxKey=true`, then `HandleMessage` dispatches to the tool closure. Search handler (`tools.go:104`): `RequireString(query)` → `svc.Search`; on error → `NewToolResultError(toolErrorJSON("search", err, resp.Route, resp.RouteTrace))`. On success: tip decision — ephemeral→always emit; else session key from `ClientSessionFromContext().SessionID()` or `"stdio-default"`, hashed, checked/recorded under `tipState` mutex (fail-open beyond cap). If shouldEmit → `svc.ProviderStatus` → `core.BuildSetupTip`. Response marshaled via `json.MarshalIndent` → `NewToolResultText`. extract registered only when `HasExtractCapableConfigured()`.

### Area: providers — web search/extract provider adapters + shared HTTP retry/error layer

**Purpose.** Concrete adapters satisfying `core.Provider`, translating Nole's neutral request/response types into each upstream API and back. Four real network providers (brave, tavily, firecrawl, ddgs), one local subprocess provider (scrapling, via Python), a deterministic mock placeholder, and a shared `providerhttp` package supplying retry-with-backoff and a secret-safe HTTP status error type. Keeps provider-specific quirks (auth headers, decoding, rate-limit signaling, redaction) isolated per adapter.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `providerhttp.DoWithRetry` | func | `internal/providers/providerhttp/retry.go:40` | Core retry loop: clones request per attempt, calls `client.Do`, returns on transport error or non-transient status, else drains+sleeps backoff. All four network providers route through it. |
| `providerhttp.RetryOptions` | type | `internal/providers/providerhttp/retry.go:13` | Tunable config (MaxAttempts, BaseDelay, MaxDelay, injectable Sleep). |
| `providerhttp.DefaultRetryOptions` | func | `internal/providers/providerhttp/retry.go:20` | From env (`NOLE_RETRY_MAX_ATTEMPTS` clamped 1..5, `NOLE_RETRY_BASE_DELAY_MS`), 5s MaxDelay. |
| `providerhttp.retryDelay` | func | `internal/providers/providerhttp/retry.go:92` | Honors Retry-After (seconds or HTTP date), else exponential `base*2^(attempt-1)` capped. |
| `providerhttp.isTransientStatus` | func | `internal/providers/providerhttp/retry.go:121` | Retryable: 429, 502, 503, 504. Excludes 408 and 202. |
| `providerhttp.cloneRequestForAttempt` | func | `internal/providers/providerhttp/retry.go:72` | Replays body via GetBody; 'not replayable' error if body w/o GetBody on attempt>1. |
| `providerhttp.HTTPStatusError` | type | `internal/providers/providerhttp/errors.go:9` | Public-safe non-2xx error: stores only provider/operation/status/body-size/category, never raw body. |
| `providerhttp.NewHTTPStatusError` | func | `internal/providers/providerhttp/errors.go:17` | Records `len(body)` + derived category; returned by brave/tavily/firecrawl/ddgs on non-200. |
| `providerhttp.statusCategory` | func | `internal/providers/providerhttp/errors.go:38` | Classifies status into auth/transient/server/client/unexpected. |
| `brave.Provider.Search` | method | `internal/providers/brave/brave.go:69` | GET w/ `X-Subscription-Token`; clamps count >=1; maps `web.results`. |
| `brave.clampMin` | func | `internal/providers/brave/brave.go:140` | Misleadingly named `max(a,b)` used to floor count at 1. |
| `brave.New` | func | `internal/providers/brave/brave.go:28` | Functional-options; falls back to `BRAVE_API_KEY` then `BRAVE_SEARCH_API_KEY`. |
| `tavily.Provider.Search` | method | `internal/providers/tavily/tavily.go:69` | POST JSON; advanced depth for TaskResearch else basic; default limit 5; snippet trunc 300. |
| `tavily.Provider.Extract` | method | `internal/providers/tavily/tavily.go:156` | POST single URL to `/extract`; returns first result's `raw_content`. |
| `firecrawl.Provider.Search` | method | `internal/providers/firecrawl/firecrawl.go:76` | POST `{baseURL}/search`; snippet Description→Markdown then trunc 300. |
| `firecrawl.Provider.Extract` | method | `internal/providers/firecrawl/firecrawl.go:154` | POST `/scrape`; markdown content + string-only metadata. |
| `firecrawl.WithBaseURL` | func | `internal/providers/firecrawl/firecrawl.go:29` | Overrides default `https://api.firecrawl.dev/v2`; used by tests. |
| `ddgs.Provider.Search` | method | `internal/providers/ddgs/ddgs.go:40` | Scrapes DDG no-JS HTML via POST w/ browser headers; regex-parses links/snippets; skips ad redirects; special-cases 202 as rate-limit. |
| `ddgs.cleanHTML` | func | `internal/providers/ddgs/ddgs.go:150` | Strips tags + decodes a hand-picked entity set. |
| `ddgs` regex set | var | `internal/providers/ddgs/ddgs.go:33` | Package-level compiled regexes driving the brittle scrape. |
| `scrapling.Provider.Extract` | method | `internal/providers/scrapling/scrapling.go:60` | Runs embedded Python via `exec.CommandContext`, pipes JSON on stdin, decodes `{content,metadata}`; distinguishes timeout, sanitizes stderr. |
| `scrapling.extractScript` | const | `internal/providers/scrapling/scrapling.go:141` | Embedded Python using `scrapling.fetchers.Fetcher` + HTMLParser fallback. |
| `scrapling.sanitizeError` | func | `internal/providers/scrapling/scrapling.go:133` | Trims + caps subprocess stderr at 500 chars. |
| `mock.Provider` | type | `internal/providers/mock/mock.go:10` | Deterministic placeholder; `New`/`NewUnavailable` toggle availability; used when keys absent. |

**Data flow.** Wiring: `app.go:38-61` constructs each provider (real w/ `WithAPIKey` if env key present, else `mock.NewUnavailable` for brave/tavily/firecrawl; ddgs + scrapling always registered). `bench.go:128-132` builds a parallel map. `core.Router`/`Service` select by Task+Capability then call Search/Extract. Each network adapter: guard missing key → build request (GET brave, POST+JSON tavily/firecrawl, POST+form ddgs) → `DoWithRetry(ctx, client, req, DefaultRetryOptions())` → non-200 → `NewHTTPStatusError` (body discarded, size kept) → `json.Decode` → map to `core.SearchResult`/`ExtractResult` w/ Provider tag + 300-char snippet trunc. ddgs diverges: reads HTML, regex-extracts, treats 202 as a locally-built sanitized rate-limit error (documented at `ddgs.go:69-78` because `safeerr.Message` unwraps to `HTTPStatusError.Error()`). scrapling diverges: no HTTP, shells out to Python with per-call timeout. Errors render through `safeerr.Message` for redaction.

### Area: safety — `internal/safeerr` + `internal/safenet` (error redaction + SSRF URL preflight)

**Purpose.** Two sibling security-hardening packages on the boundary between Nole's internals and the outside world. `safeerr` sanitizes errors before any user-facing surface (CLI stderr, HTTP/MCP JSON, bench tracer), preferring a pre-structured public-safe error type when present. `safenet` performs a best-effort local SSRF preflight on URLs before a provider fetch, blocking non-http(s) schemes, local hostnames, and private/loopback/link-local/multicast/unspecified/cloud-metadata IPs.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `Message` | func | `internal/safeerr/safeerr.go:18` | nil → ""; if err unwraps to `*providerhttp.HTTPStatusError` returns its already-safe `Error()` verbatim (dropping wrapping context), else runs `err.Error()` through `Redact`. |
| `Redact` | func | `internal/safeerr/safeerr.go:29` | Trims then replaces every `sensitivePatterns` match with `[REDACTED]`. |
| `sensitivePatterns` | var | `internal/safeerr/safeerr.go:11` | 4 compiled regexps: Authorization bearer header, bare bearer token, api_key/token/secret/password pairs, any http(s) URL. |
| `ValidateURL` | func | `internal/safenet/url.go:18` | SSRF preflight: rejects non-http(s)/empty host, `IsBlockedHost` names, validates literal IPs without DNS, else resolves via `lookupIP` requiring every IP to pass `validateIP`. DNS failure is fail-closed. |
| `validateIP` | func | `internal/safenet/url.go:59` | Per-IP deny: loopback, RFC1918, link-local uni/multicast, multicast, unspecified, `169.254.169.254`. |
| `IsBlockedHost` | func | `internal/safenet/url.go:83` | DNS-free denylist: `{localhost, localhost.localdomain}`. |
| `lookupIP` | var | `internal/safenet/url.go:10` | Indirection var defaulting to `net.LookupIP`; swappable in tests. |
| `HTTPStatusError` | type | `internal/providers/providerhttp/errors.go:9` | The pre-sanitized provider error type `safeerr.Message` special-cases; `Error()` never echoes raw body. |

**Data flow.** safeerr: callers (`main.go:13`, `research.go:101/144`, `bench.go:236`, `app.go:275`, `http.go:84/115`, `mcpserver/errors.go:21`) pass a raw error to `Message`. `errors.As` detects `*HTTPStatusError`; if matched returns `statusErr.Error()` (body-free) and never `Redact`; else regex sweep → `[REDACTED]`. ddgs (`ddgs.go:71`) deliberately does NOT wrap with `HTTPStatusError` (would erase its 202 classification) and hand-builds a sanitized error. safenet: `service.go:143` (`Service.Extract`) calls `ValidateURL` before routing, wrapping errors as `url validation: %w`. Flow: `url.Parse` → scheme/host gate → `IsBlockedHost` → literal-IP fast path (`validateIP`) OR `lookupIP` fan-out (every resolved IP must pass). Fail-closed: parse errors, unsupported schemes, empty host, denylisted host, blocked IP, and DNS failure all return non-nil.

### Area: bench — Benchmark / route-evidence subsystem (`internal/bench` + `internal/cli/bench.go` + `cmd/bench` Python runner)

**Purpose.** Benchmark/eval harness and sanitized "route-evidence" reporting. Three modes: default deterministic offline eval (scores a versioned fixture set against the route matrix with in-code observations, no network/keys), optional low-limit live smoke run through the real service/router, and a comprehensive live mode that forces every provider to run every capability-permitting fixture (bypassing router/policy/quota) for per-provider latency/success. Heavy anti-overclaim emphasis: evidence metadata documents what each mode does/does-not measure, all Markdown is sanitized of URLs/paths/secrets, and a shell "claims guard" blocks unsupported ranking/speed language.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `newBenchCommand` | func | `internal/cli/bench.go:21` | Cobra `bench`; flags (`--json/--evidence-md/--live/--max-live-cases/--comprehensive/--max-comprehensive-cases`); `--comprehensive` requires `--live`. |
| `runComprehensiveBench` | func | `internal/cli/bench.go:117` | CLI adapter; loads env, builds provider map, calls `bench.RunComprehensiveLive` w/ NetworkContext + cost policy from env. |
| `comprehensiveBenchProviders` | func | `internal/cli/bench.go:126` | Fixed provider map `{brave, ddgs, firecrawl, scrapling, tavily}` (router/policy bypassed). |
| `runLiveBench` | func | `internal/cli/bench.go:136` | Live smoke; clamps maxCases (0,10]→3, truncates fixtures, runs through real `core.Service`. |
| `runLiveBenchCase` | func | `internal/cli/bench.go:172` | One live fixture via `svc.Extract`/`svc.Search`; records route, sanitized attempts, coarse `liveScore`. |
| `liveScore` | func | `internal/cli/bench.go:209` | Coarse 0-100: 40 base + up to 30 for count + latency bucket bonus. |
| `sanitizedBenchError` | func | `internal/cli/bench.go:232` | Wraps `safeerr.Message`, truncates to 160 chars. |
| `sortedKeys` | func | `internal/cli/bench.go:104` | Generic map-key sorter via hand-rolled insertion sort. |
| `Report` | type | `internal/bench/bench.go:111` | Top-level JSON artifact: schema, mode, fixture version, evidence metadata, summary, results, matrix, comprehensive-only fields (nil in legacy). |
| `EvidenceMetadata` | type | `internal/bench/bench.go:27` | Self-describing 'measures / does not measure / how to reproduce' block — core anti-overclaim contract. |
| `DefaultFixtureSet` | func | `internal/bench/bench.go:473` | Versioned fixtures (`2026-05-17.offline.v1`): 17 fixtures (16 search + 1 extract), en/tr/es/de. |
| `RunOffline` / `RunOfflineWithObservations` | func | `internal/bench/bench.go:498` | Deterministic offline eval; iterates fixtures, evaluates against matrix + observation table. |
| `evalOfflineCase` | func | `internal/bench/bench.go:530` | Walks route (TaskGeneral fallback), simulates per-provider attempts, marks first success, scores. |
| `defaultOfflineObservations` | func | `internal/bench/bench.go:646` | Hardcoded per-(provider,task) Observation table — synthetic 'fixture data'. |
| `scoreMetrics` / `latencyScore` / `metricsFromObservation` | func | `internal/bench/bench.go:620` | Offline scoring: averages 9 normalized metrics × 100; latency buckets; Kind-derived defaults. |
| `MarkdownEvidenceSummary` | func | `internal/bench/bench.go:226` | Sanitized offline/live Markdown evidence summary. |
| `sanitizeMarkdownCell` | func | `internal/bench/bench.go:452` | Escapes pipes, redacts Authorization/Bearer/SECRET/TOKEN/api_key, replaces URLs + abs paths. |
| `publicReason` | func | `internal/bench/bench.go:399` | Allowlist mapping known reasons to themselves, else `sanitized_error`. |
| `RunComprehensiveLive` | func | `internal/bench/comprehensive.go:48` | Per-provider goroutines run fixtures serially (inter-call spacing) while providers run concurrently; capability-filters; flattens alphabetically. |
| `runComprehensiveOne` | func | `internal/bench/comprehensive.go:126` | Single (provider, fixture) probe w/ per-call timeout; records Success/ResultCount/LatencyMS/ErrorClass. |
| `classifyComprehensiveError` | func | `internal/bench/comprehensive.go:173` | Reduces errors to a sanitized vocabulary; `HTTPStatusError` first, then string match; 202 treated as 429. |
| `summarizeMeasurements` | func | `internal/bench/comprehensive.go:222` | Aggregates per-provider: calls/successes/failures, avg/p50/p95 latency (successful only), error histogram. |
| `MarkdownComprehensiveSummary` | func | `internal/bench/comprehensive.go:296` | Sanitized comprehensive Markdown: aggregate + per-(provider,task) tables. |
| `check-benchmark-claims.sh` | file | `scripts/check-benchmark-claims.sh:40` | CI shell guard: requires 4 benchmark docs + mandated disclaimers, greps for unsupported ranking/speed regexes. |
| `cmd/bench/main.py:main` | func | `cmd/bench/main.py:232` | Standalone Python provider-benchmark runner across 12 categories; parallel/legacy to Go harness, not invoked by Go/CI. |

**Data flow.** `nole bench` → `newBenchCommand.RunE` (`bench.go:37`). default → `bench.RunOffline(DefaultFixtureSet, DefaultRouteMatrix)` → `evalOfflineCase` per fixture (route → `observationFor` → `metricsFromObservation` → `scoreMetrics`). `--live` → `runLiveBench` → `defaultService()` → `runLiveBenchCase` → `svc.Search`/`svc.Extract` → `attemptsFromTrace` + `liveScore` + `sanitizedBenchError`. `--live --comprehensive` → `runComprehensiveBench` → env + `comprehensiveBenchProviders()` → `RunComprehensiveLive` (goroutine-per-provider, `runComprehensiveOne` over capability-matching fixtures w/ per-call timeout + spacing, buffered channel) → flatten alphabetically → `summarizeMeasurements`. Output: `--evidence-md` → `MarkdownComprehensiveSummary`/`MarkdownEvidenceSummary`; `--json` → indented encode; else `printBenchReport`. All Markdown → `sanitizeMarkdownCell`; reasons → `publicReason`/`classifyComprehensiveError`. CI `scripts/audit.sh --ci` runs `check-benchmark-claims.sh` + `go run . bench`. `cmd/bench/main.py` is a self-contained side path exercised only by `cmd/bench/test_main.py`.

### Area: harness — CI/CD pipelines and release-gate verification scripts

**Purpose.** Truth-preservation and release-safety layer. Guard scripts and workflows enforce doc framing, anti-overclaim language, offline integration evidence, secret hygiene, reproducible cross-platform builds, and Go test/vet/vuln gates. `audit.sh` runs in CI and at tag-publish so a release cannot bypass PR checks.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `audit.sh` unified gate orchestrator | file | `scripts/audit.sh:33` | Runs guards, `go test`/`vet`, smoke commands, generates+verifies integration evidence, git diff check. `--ci` from `ci.yml:36` and `release.yml:85`; non-CI dirty-tree diff at line 59. |
| `check-docs-framing.sh` required doc list | var | `scripts/check-docs-framing.sh:9` | 24 required docs (line 35); ~100 literal grep assertions enforce README framing + client Status labels (loop line 145). |
| `secret-scan.sh` Python scanner | file | `scripts/secret-scan.sh:9` | Python3 scanner over `git ls-files`; `safe_value` (line 50) treats values under 12 chars as safe (line 56). |
| `verify-integration-evidence.sh` generator | file | `scripts/verify-integration-evidence.sh:11` | Offline build with keys unset, placeholder `TAVILY_API_KEY` (28); tool grep line 39 coupled to `doctor.go:136/452`. `release.yml:37`, `ci.yml:15`. |

**Data flow.** `ci.yml` runs verify (`audit.sh --ci`), public-safety (`secret-scan.sh`), cross-platform-build (`check-release-builds.sh`), `govulncheck`. `audit.sh` fans out to `check-docs-framing.sh` (which re-invokes `check-benchmark-claims.sh` + `check-integration-evidence.sh`), `go test`/`vet`, smoke commands, then `verify-integration-evidence.sh` pipes generated Markdown into `check-integration-evidence.sh` (`audit.sh:44-47`). `release.yml` re-runs the same gates and build then `gh release create` uploads dist assets. Coupling chain: `doctor.go` sorted tool slice → `verify-integration-evidence.sh:39` grep → `check-integration-evidence.sh:29` assertion.

---

## Request lifecycle

Tracing a search request end to end (extract follows the same shape with an extra `safenet.ValidateURL` preflight):

1. **Entry.** Either `nole search <query>` → `newSearchCommand` (`search.go:11`) → `runSearch` (`app.go:388`); or an MCP `search` tool call → `RegisterTools` search handler closure (`tools.go:104`) over `*core.Service`; or HTTP `POST /api/search` / `POST /mcp` (`http.go:36/158`).
2. **Composition.** CLI path builds a fresh Service per call via `defaultService()` (`app.go:24`) after `loadDefaultNoleEnvFile()` (`env_file.go:8`). MCP/HTTP path holds the Service built once at server construction (`mcpserver.New` / `newHTTPHandler`).
3. **Service entry.** `Service.Search` (`service.go:37`): default `Task→TaskGeneral`.
4. **Route resolution.** `routeFor` (`service.go:259`) reads `s.router.matrix[task]` directly, TaskGeneral fallback. (Note: `Router.Select` at `router.go:41` is NOT on this path — service reimplements the loop.)
5. **Cache.** If `cache != nil` (`WithResponseCache`, `service.go:21`), `cache.GetSearch` keyed via `searchCacheKey` (`cache.go:112`); hit → rebuild Route/RouteTrace via `cacheHitAttempt` (`service.go:275`) + `BuildSearchRoutingInsight` (`insight.go:29`) and return before any quota check; miss → append cache-miss attempt.
6. **Per-provider gate loop.** For each provider name in route: `registry.Get` (`registry.go:31`) → skip `not_registered`; `HasCapability` (`types.go:144`) → skip `missing_search_capability`; `ledger.Decide(name)` (`quota.go:178`→`decideLocked` `quota.go:193`) under cost policy → skip with `premium_blocked_free_first` / `free_quota_exhausted` if not Allowed; `provider.Status(ctx)` → skip if unavailable. Each outcome produces a typed `RouteAttempt` (`types.go:69`) via `attemptWithDecision` (`service.go:291`).
7. **Provider call.** `provider.Search(ctx, req)` (e.g. `brave.go:69`), routed through `providerhttp.DoWithRetry` (`retry.go:40`) for network providers; latency via `time.Since`. Error → `provider_error` trace, continue; empty results → `empty_results` trace, continue (free tier NOT debited).
8. **Quota debit (money-safety invariant).** On success ONLY: `ledger.Record(name)` (`quota.go:297`) takes the file lock, re-reads disk, decrements `FreeRemaining` or adds `SpentCents`, persists (`persistLocked` `quota.go:609`). If Record errors, re-Decide and set trace reason `success_<reason>` but still return the response (`service_test.go:185` guards this).
9. **Cache write.** On success `cache.SetSearch` stores a clone (`cloneSearchResponse` `cache.go:145`).
10. **Insight + return.** `BuildSearchRoutingInsight` (`insight.go:29`) → `buildRuntimeRoutingInsight` (`insight.go:117`) reconstructs the winning provider/policy/cache summary from the trace. Response (results + Route + RouteTrace + RoutingInsight + optional SetupTip) returns to the caller.
11. **Output / error rendering.** CLI: `writeJSON` (`app.go:246`) or `writeHumanRoutingInsight`. MCP: `json.MarshalIndent` → `NewToolResultText`, or `toolErrorJSON` (`errors.go:18`) on failure. Exhausted route → `NoFreeQuotaError` (`errors.go:5`) or `lastErr`, both with `BuildErrorRoutingInsight`. All error text passes `safeerr.Message` (`safeerr.go:18`) for redaction before reaching stderr/JSON.

---

## Setup-writer subsystem

`nole setup` (`setup.go:39`) registers Nole as an MCP server across seven agents. Each writer merges only the `nole` entry into the agent's native user-scope config, preserving unknown sibling fields/comments, writing atomically via `atomicWriteFile` (`setup.go:768`) with a `.bak` backup and `configWriteMode` (`setup.go:759`) permission preservation.

| Platform | Config path family | Serialization | Writer anchor | Notes |
|---|---|---|---|---|
| Claude | Managed by Claude Code CLI (no file written) | n/a (CLI command printed) | `printClaudeInstructions` `internal/cli/setup.go:204` | Instruction-only; prints `claude mcp add nole -s user -- ...`; not counted in configured total. |
| Cursor | Cursor user MCP config | JSON | `writeMCPJSONConfig` `internal/cli/setup.go:438` | Shared with Windsurf; preserves unknown server fields via RawMessage map, replaces only `nole`. |
| Windsurf | Windsurf user MCP config | JSON | `writeMCPJSONConfig` `internal/cli/setup.go:438` | Same shared JSON writer as Cursor. |
| Codex | Codex TOML config | TOML | `writeCodexConfigPath` `internal/cli/setup.go:472` | Hand-rolled `[mcp_servers.nole]` table via `upsertCodexTomlTable` (508); `codexMCPServerBlock` (489) embeds `/bin/sh -lc 'set -a; . .env; exec nole mcp'` unless wrapper set. |
| OpenCode | OpenCode JSON config (`mcp` key) | JSON | `writeOpenCodeConfigPath` `internal/cli/setup.go:566` | `{type:local, command:[bin,mcp], enabled, environment}` shape (`openCodeEntry` 591). |
| Kimi | Kimi JSON config (`mcpServers` key) | JSON | `writeKimiConfigPath` `internal/cli/setup.go:639` | `{command}` in wrapper mode or `{command,args}` bare (`kimiEntryRaw` 663), matching `kimi mcp add` output. |
| Hermes | Hermes YAML config (`mcp_servers.nole`) | YAML (`yaml.Node` tree) | `writeHermesConfigPath` `internal/cli/setup.go:253` | Comment-preserving via `yamlMappingUpsert` (369); `upsertHermesNoleServer` (314) sets command/args + default timeout=120/connect_timeout=60 + tools policy. |

**Not yet implemented (explicit new targets for Phase 2):**

| Platform | Status | Notes |
|---|---|---|
| Gemini | NOT implemented | No writer in `setup.go`, no entry in the per-agent dispatch, no doc. Explicit new target. |
| Grok | NOT implemented | No writer in `setup.go`, no entry in the per-agent dispatch, no doc. Explicit new target. |

Optional `--local-extract` (`setupLocalExtract` `setup_local_extract.go:26`) provisions an isolated Python venv, installs `scrapling[fetchers]`, persists `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env`, and emits a 0700 POSIX `/bin/sh` env-sourcing wrapper (`writeMCPWrapper` `setup_local_extract.go:195`) so MCP clients that do not inherit shell env still find provider keys.

---

## Dependency graph

### Internal package import edges

- `main` → `internal/cli`, `internal/safeerr`
- `internal/cli` (app/composition root) → `internal/core`, `internal/providers/{brave,ddgs,firecrawl,mock,scrapling,tavily}`, `internal/safeerr`
- `internal/cli` (mcp/serve/http) → `internal/mcpserver`, `internal/core`, `internal/safeerr`
- `internal/cli` (bench) → `internal/bench`, `internal/core`, `internal/providers/{brave,ddgs,firecrawl,scrapling,tavily}`, `internal/safeerr`
- `internal/mcpserver` → `internal/core`, `internal/version`, `internal/safeerr`, `internal/cli` (HTTP wiring sets `EphemeralCtxKey`; `defaultService`)
- `internal/core` (service) → `internal/safenet`, intra-package (registry, quota, router, planner, cache, insight, byok_metadata, setup_hints, types, errors)
- `internal/core` → consumes `internal/providers/providerhttp` indirectly (via `safeerr` / error classification) and the `core.Provider` interface implemented by all provider packages
- `internal/providers/{brave,ddgs,firecrawl,tavily}` → `internal/core`, `internal/providers/providerhttp`
- `internal/providers/scrapling` → `internal/core` (no HTTP; shells to Python)
- `internal/providers/mock` → `internal/core`
- `internal/bench` → `internal/core`, `internal/providers/providerhttp` (error classification), `internal/providers/{brave,ddgs,firecrawl,scrapling,tavily}`, `internal/safeerr`
- `internal/safeerr` → `internal/providers/providerhttp` (special-cases `HTTPStatusError`)
- `internal/safenet` → stdlib only (`net`, `net/url`)
- `internal/version` → stdlib only (build-stamped vars; consumed only by `internal/mcpserver`)

### External modules (from `go.mod`)

| Module | Version | Role |
|---|---|---|
| `github.com/mark3labs/mcp-go` | v0.43.1 | MCP server library: `NewMCPServer`, `AddTool`, `ServeStdio`, in-process sessions, `HandleMessage`, JSON-RPC tool result/error types. Backbone of `internal/mcpserver` + the HTTP MCP bridge. |
| `github.com/spf13/cobra` | v1.10.2 | CLI command framework for the root command and all 11 subcommands. |
| `gopkg.in/yaml.v3` | v3.0.1 | `yaml.Node` tree parsing/marshaling for the comment-preserving Hermes setup writer. |
| `github.com/bahlo/generic-list-go` | v0.2.0 (indirect) | Transitive dep of mcp-go / jsonschema tooling. |
| `github.com/buger/jsonparser` | v1.1.1 (indirect) | Transitive (mcp-go JSON handling). |
| `github.com/google/uuid` | v1.6.0 (indirect) | Transitive (mcp-go session IDs). |
| `github.com/inconshreveable/mousetrap` | v1.1.0 (indirect) | Transitive cobra (Windows entry detection). |
| `github.com/invopop/jsonschema` | v0.13.0 (indirect) | Transitive (mcp-go tool schema generation). |
| `github.com/mailru/easyjson` | v0.7.7 (indirect) | Transitive (jsonschema). |
| `github.com/spf13/cast` | v1.7.1 (indirect) | Transitive cobra. |
| `github.com/spf13/pflag` | v1.0.9 (indirect) | Transitive cobra flag parsing. |
| `github.com/wk8/go-ordered-map/v2` | v2.1.8 (indirect) | Transitive (jsonschema ordered maps). |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 (indirect) | Transitive (mcp-go URI templates). |

Build toolchain: `go 1.25.10`. CI also invokes `golang.org/x/vuln/cmd/govulncheck@latest`, the `gh` CLI, and `python3` (for `secret-scan.sh` and the orphaned `cmd/bench/main.py`).

---

## Build / test / bench / CI harness

### Local gate steps (`scripts/audit.sh`)

`audit.sh` (`scripts/audit.sh:33`) is the unified gate, run both locally and in CI (`--ci` from `ci.yml:36` and `release.yml:85`). It fans out to:

1. **Doc-framing guard** — `check-docs-framing.sh` (`:9`), which re-invokes `check-benchmark-claims.sh` and `check-integration-evidence.sh`.
2. **Go gates** — `go test`, `go vet`.
3. **Smoke commands** — subcommand invocations to confirm the binary runs.
4. **Integration evidence** — `verify-integration-evidence.sh` (`:11`) generates Markdown and pipes it into `check-integration-evidence.sh` (`audit.sh:44-47`).
5. **Dirty-tree diff check** (non-CI, `audit.sh:59`) — fails if the working tree drifted during the run.

### The two doc-guard scripts

| Script | Anchor | Enforces |
|---|---|---|
| `check-docs-framing.sh` | `scripts/check-docs-framing.sh:9` | A required list of 24 docs (line 35) plus ~100 literal `grep` assertions over README framing and per-client Status labels (loop at line 145). Truth-preservation by literal grep — brittle to wording edits. |
| `check-benchmark-claims.sh` | `scripts/check-benchmark-claims.sh:40` | The 4 benchmark docs exist and carry mandated disclaimers; greps for unsupported ranking/speed regexes (excluding negated "do not / never" lines). Driven in tests by `claims_guard_test.go:14`, with a separate `claims_guard_test.go:154` test scanning BYOK metadata strings for banned words. |

### CI workflow

`ci.yml` runs four jobs: verify (`audit.sh --ci`), public-safety (`secret-scan.sh` — Python scanner over `git ls-files`, `safe_value` whitelists values under 12 chars), cross-platform-build (`check-release-builds.sh`, which also ldflag-stamps `internal/version.Version` at line 44), and `govulncheck`. `release.yml` re-runs the same gates + build, then `gh release create` uploads dist assets — so a tag publish cannot bypass PR checks. A brittle coupling chain exists: `doctor.go` sorted tool slice → `verify-integration-evidence.sh:39` grep → `check-integration-evidence.sh:29` assertion.

### Offline bench harness

`nole bench` (default) runs `bench.RunOffline(DefaultFixtureSet, DefaultRouteMatrix)` (`bench.go:498`): a deterministic, network-free, key-free eval scoring 17 versioned fixtures (`2026-05-17.offline.v1`, `bench.go:473`) against the route matrix using a hardcoded observation table (`bench.go:646`). Output (`GeneratedAt='deterministic-offline'`) is reproducible and CI-stable. Optional `--live` (smoke) and `--live --comprehensive` (per-provider full matrix, router/policy/quota bypassed) modes exist; all Markdown output is sanitized via `sanitizeMarkdownCell` (`bench.go:452`). A standalone Python runner (`cmd/bench/main.py:232`) is parallel/legacy and not invoked by Go code or CI.

---

## Improvement seed inventory

This feeds Phase 2 research. Only seeds present in the area maps are listed.

| Title | Area | Anchor | Confidence | Rationale |
|---|---|---|---|---|
| version.Commit and version.Date are dead code | quality | `internal/version/version.go:5` | high | Only `version.Version` is read (`mcpserver/server.go:10`) and only Version is ldflag-stamped (`check-release-builds.sh:44`); Commit/Date are declared but never consumed and always stay "unknown". Wire them in or remove. |
| Redundant/unreachable error branch in defaultQuotaLedger | quality | `internal/cli/app.go:120` | high | `if err != nil && ledger != nil { return ledger }` (120-122) is identical to the following `if ledger != nil` (123-125); `NewFileQuotaLedgerWithPolicy` always returns non-nil, so the memory-fallback at 126-130 is unreachable for a non-empty path. |
| No top-level version command despite version package | docs | `internal/cli/root.go:14` | medium | Root registers 11 subcommands but no `version` subcommand and no cobra Version field, though `internal/version` is build-stamped. CLI users/agents cannot query the running version. |
| defaultService() reconstructs the whole Service on every call | latency | `internal/cli/app.go:24` | medium | `runSearch` and other subcommands rebuild registry, re-open the file-backed ledger (disk read + lock), and allocate a fresh cache per invocation. Acceptable for per-turn MCP spawn; any in-process multi-call path pays full reconstruction + cold cache. No memoization. |
| All registry.Register errors are silently discarded | stability | `internal/cli/app.go:38` | medium | Every `registry.Register(...)` discards its error with `_ =` (38,40,45,47,52,54,58,61). A duplicate/invariant violation is invisible at startup, surfacing only as a missing provider at routing time with no breadcrumb. |
| Router.Select is dead code in production | quality | `internal/core/router.go:41` | high | `.Select(` appears only in `router_test.go`; `service.go` reads `s.router.matrix` directly (`service.go:260`) and re-walks providers itself. The two paths can drift; well-tested Select gives false confidence the runtime path is covered. |
| Planner has no rules for TaskSemantic or TaskExtract | coverage | `internal/core/planner.go:71` | high | `plannerRules` (71-99) and `taskPriority` (262) omit TaskSemantic/TaskExtract though both exist in the taxonomy (`types.go:13,18`). Semantic/extract intents can never classify; they fall through to general and tie-break last at default 100. |
| taskLabel returns raw enum string for tasks without a planner rule | docs | `internal/core/planner.go:249` | medium | `taskLabel` only finds friendly labels for rule-backed tasks + general (255); TaskSemantic/TaskExtract fall to `return string(task)` (258), so `TaskOverride=semantic` produces uncurated label 'semantic'. |
| Router.Select drops the ledger's denial reason | quality | `internal/core/router.go:59` | medium | Select uses only `Decide(name).Allowed`, discarding Reason/Policy/CostClass; returned route is bare `[]string` with no skip reasons, vs service's rich `RouteAttempt` traces — reinforces divergence risk. |
| hasTopScoreTie flags ambiguity on any tie of top two scores | correctness | `internal/core/planner.go:227` | medium | Compares only `intents[0].Score == intents[1].Score` (231); two genuinely distinct strong intents (docs=4, news=4) mark the whole classification Ambiguous, conflating 'no clear single task' with 'multiple clear tasks'. |
| markUnavailableLocked ignores its err argument | quality | `internal/core/quota.go:645` | high | Takes an `err error` param but never uses it; warning is a fixed string. Callers pass real I/O errors (e.g. `persistLocked` failure at `quota.go:349`) that are silently dropped. Log/wrap it or drop the param. |
| Windows file lock is non-blocking, unlike the Unix LOCK_EX path | correctness | `internal/core/file_lock_windows.go:21` | medium | `LockFileEx` called with `LOCKFILE_EXCLUSIVE_LOCK` only (no FAIL_IMMEDIATELY), 1-byte region, no retry loop, vs blocking `flock(LOCK_EX)` (`file_lock_unix.go:11`). If it returns without truly blocking under contention, the multi-process race guard weakens on Windows. No Windows contention test. |
| PeriodStart >= now refresh predicate skips refresh on backward clock | correctness | `internal/core/quota.go:598` | medium | `refreshExpiredEntriesLocked` only refills when `PeriodStart < now`. A future-dated PeriodStart (clock skew, ledger copied from another host) never refreshes; provider stays exhausted indefinitely. No guard/warning for `PeriodStart > now`. |
| Corrupt-recovery/reload errors surfaced only via in-memory state | stability | `internal/core/quota.go:449` | medium | `reloadFromDiskLocked` returns `recoverCorruptLedgerLocked`'s (often nil) result, so the constructor returns `(ledger, nil)` on a corrupt ledger; only signal is `BudgetStatus().LedgerWarning`. Callers checking only `err` treat a fail-closed corrupt ledger as a clean start. |
| Cache has no size bound / eviction beyond lazy TTL expiry | stability | `internal/core/cache.go:22` | medium | search/extract maps grow unbounded; entries deleted only lazily on a Get that finds them expired (67, 95). A long-lived MCP server with many distinct never-repeated queries accumulates entries. No background sweep / max-entries cap. |
| refreshExpiredEntriesLocked from Decide() mutates in-memory state without persisting | correctness | `internal/core/quota.go:188` | low | `Decide()` discards the `changed` return and never persists (intentional, 184-186). A second process Deciding the same provider re-reads stale disk and may see the pre-refresh value; two processes can transiently disagree on FreeRemaining across a month boundary. |
| routeFor duplicates Router's route-resolution logic and bypasses Router | quality | `internal/core/service.go:259` | high | `Service.routeFor` reaches into `s.router.matrix` and reimplements the task→TaskGeneral fallback already in `Router.Select` (`router.go:45-48`). The Router field is built but only its matrix is read; Select is never called — leaky abstraction that can drift. |
| Search and Extract pipelines are ~90% duplicated | quality | `internal/core/service.go:57` | medium | The gate/timing/Record/trace/cache loop is copy-pasted between Search (57-127) and Extract (161-220); only the capability constant, empty check, and insight builder differ. Any fix must be applied twice. |
| Provider call is unbounded by any Service-level timeout/deadline | stability | `internal/core/service.go:81` | medium | `provider.Search`/`Extract` invoked with the caller's ctx and no Service-imposed deadline (81, 185). A hung provider that ignores ctx blocks the whole route walk; no per-attempt timeout bounds fallback latency. |
| No empty-results-then-success per-provider quota assertion | coverage | `internal/core/service_test.go:257` | medium | `TestServiceDoesNotDecrementFreeTierOnEmptyResults` covers a single empty provider; no test asserts that when an empty provider precedes a successful one, ONLY the successful provider's quota is recorded. Fallback test (273) checks route/trace, not per-provider ledger state. |
| Cache-hit branch bypasses quota and is entirely untested | correctness | `internal/core/service.go:123` | medium | On a cache hit (44-56) the path returns before any `Decide`/`Record`, so a cached premium response is served with no quota check; `SetSearch` (124) caches under full req. No test uses `WithResponseCache`, so the cache short-circuit's interaction with cost policy is untested. |
| research extract step has no per-URL timeout or overall budget guard | latency | `internal/cli/research.go:138` | medium | `researchPipeline` loops `Service.Extract` on up to 5 URLs sequentially with inherited `context.Background()` (32); each extract can try multiple providers with 20-30s client timeouts, so worst-case `research` can block for minutes with no upper bound/cancellation. |
| research synthesis/extract limits are magic numbers duplicated inline | quality | `internal/cli/research.go:151` | medium | 2000-char trunc (151-154), 300-char summary cap (202-204), per-task Limit:5 (97), extractLimit min(...,5) (125), additional-sources cap 5 (213) are all hardcoded with no named constants — hard to tune, easy to drift between extract loop and summary builder. |
| research URL filter is a brittle substring/suffix heuristic | correctness | `internal/cli/research.go:132` | medium | Skips via `HasSuffix(url,".pdf")` + `Contains(url,"reddit.com")`; misses `.PDF`, query-string PDFs, catches `old.reddit.com` only incidentally, wrongly skips unrelated hosts with `reddit.com` in a path. Undocumented as best-effort. |
| insight recompute runs on zero-value response in error path | quality | `internal/cli/search.go:26` | low | `applySearchInsightMode` (and `extract.go:25`) execute before the `if err != nil` check (27); on error `runSearch` returns a zero response (`app.go:390`), so the builder runs over an empty response and the result is discarded. Harmless now, invites a bug if the error response ever carries partial data. |
| checkMCPStdioSmoke mutates global os.Stdout without synchronization | stability | `internal/cli/doctor.go:176` | low | Swaps `os.Stdout` for a pipe, restores via defer (176-181); process-global mutable state, non-reentrant. Works for single-threaded doctor but is a fragile test-style technique in production diagnostic code with no concurrency guard. |
| HTTP /api/* JSON encode errors are silently ignored | stability | `internal/cli/http.go:100` | low | Success-path responses call `json.NewEncoder(w).Encode(resp)` discarding the error (46,59,65,100,127,211). After a partial write failure the client gets a truncated 200 body and the server logs nothing. Error-path `writeHTTPJSONError` also ignores its encode error (260). |
| Windows local-extract setup has no test coverage; venv-python tests Unix-only | platform | `internal/cli/setup_local_extract_test.go:63` | medium | `venvPythonPath` branches on `GOOS=='windows'` returning `Scripts/python.exe`, but every local-extract test `t.Skip`s on Windows (fake python is a `/bin/sh` script). The Windows interpreter-path branch and whole flow are never exercised on their target platform. |
| writeMCPWrapper emits a POSIX /bin/sh wrapper with no Windows equivalent | platform | `internal/cli/setup_local_extract.go:205` | medium | Generated wrapper is hardcoded `#!/bin/sh` and is the auto-default for `--local-extract` (`setup.go:98`). On Windows this produces a non-executable script and a non-launching wrapper command, yet `venvPythonPath` shows Windows is otherwise a supported target. |
| resolveLocalExtractVenvPath error echoes the un-expanded raw input | quality | `internal/cli/setup_local_extract.go:87` | low | After `expandHomePath`, the non-absolute error reports `%q` of the original raw tilde/relative string, not the expanded path actually rejected — misleading about what was checked. |
| upsertShellEnvAssignment rewrites every matching line instead of the first | quality | `internal/cli/setup_local_extract.go:175` | low | No `break` after `replaced=true`, so a `.env` with duplicate `KEY=` lines gets all of them overwritten (none deduped). Idempotent for the single-line case but leaves duplicate keys in place. |
| Python candidate list caps at 3.13, so 3.14+ found only via generic python3/python | coverage | `internal/cli/setup_local_extract.go:105` | low | `setupPythonCandidates` hardcodes `python3.13..3.10` then falls back to `python3`/`python`. A system with only `python3.14` as a versioned name (no `python3` symlink) fails detection despite satisfying the >=3.10 gate. |
| codexMCPServerBlock relies on sh -lc login-shell semantics to source .env | correctness | `internal/cli/setup.go:498` | low | The non-wrapper Codex launch uses `/bin/sh -lc` and relies on sourcing `$HOME/.config/nole/.env` guarded only by a file-exists test; `-l` (login) behavior under dash/posix sh varies, so on strict POSIX `/bin/sh` the assumption is fragile. |
| Headerless HTTP clients re-emit setup_tip on every search | correctness | `internal/mcpserver/tools.go:123` | medium | For HTTP clients omitting `Mcp-Session-Id`, `ephemeral=true` forces `shouldEmit` on EVERY request (126), so the once-per-session tip repeats on every search. The once-per-session guarantee only holds for stdio or header-pinning clients — a gap vs. the documented intent (`core/types.go:95`). |
| First search per session does a second full provider-status pass | latency | `internal/mcpserver/tools.go:172` | medium | When `shouldEmit`, the search handler invokes `svc.ProviderStatus(ctx)` in addition to the `svc.Search` already done, to derive suggestions. For ephemeral HTTP (always-emit), every single search triggers a full provider-status pass, adding hot-path latency. |
| Advertised MCP server version defaults to "dev" | docs | `internal/mcpserver/server.go:10` | low | `NewMCPServer` reports `version.Version`, the literal "dev" (`version.go:4`) unless ldflag-overridden. If the release build does not inject the real version, MCP clients see 'dev' even for tagged releases (v0.2.4). Confirm the pipeline sets `-X .../version.Version`. |
| toolErrorJSON marshal-failure fallback concatenates operation unescaped | correctness | `internal/mcpserver/errors.go:28` | low | The fallback string-concatenates `operation` into a JSON literal. `operation` is always a hardcoded literal at the only call sites (`tools.go:113,199`), so currently safe; but the signature accepts arbitrary strings, so a future caller could emit invalid/injected JSON. |
| 408 classified 'transient' by statusCategory but NOT retried by isTransientStatus | correctness | `internal/providers/providerhttp/retry.go:121` | medium | `errors.go:42` maps 408/429/502/503/504 to 'transient', but `isTransientStatus` (122-127) only retries 429/502/503/504. A 408 surfaces as 'transient' yet was never retried — category label and retry policy disagree. |
| DoWithRetry sets lastErr on transport error then returns, making fallthrough dead | quality | `internal/providers/providerhttp/retry.go:52` | medium | On `client.Do` error the loop does `lastErr = err; return nil, err` (52-55), so transport errors are never retried and the post-loop `if lastErr != nil` (66) is only reachable on normal exit. Transport-level transient failures (reset, DNS blip) get zero retries despite MaxAttempts>1. |
| brave.go helper named clampMin actually computes max(a,b) | quality | `internal/providers/brave/brave.go:140` | high | `clampMin` returns `a if a>b else b` — that is max, not min/clamp. Used as `clampMin(req.Limit, 1)` to floor at 1, so behavior is correct but the name is the opposite of what it does, inviting future misuse. |
| ddgs result parsing relies on positional snippet alignment | correctness | `internal/providers/ddgs/ddgs.go:100` | medium | `linkMatches` and `snippetMatches` are gathered independently (94-95) then zipped by a shared `snippetIdx` that only advances inside the link loop (110-113). When an ad redirect is `continue`d (105-107) before consuming a snippet, subsequent results can pair with the wrong snippet. Also brittle to DDG HTML class changes. |
| firecrawl/tavily/brave check only StatusOK and ignore success:false in 200 bodies | correctness | `internal/providers/firecrawl/firecrawl.go:103` | medium | `firecrawlSearchResponse`/`firecrawlScrapeResponse` carry a `Success bool` (61,145) decoded but never checked — a 200 with `success:false` and empty data is reported as a successful empty result rather than an error. Status guard is StatusOK-only. |
| 300-byte snippet truncation slices on byte boundaries, can split UTF-8 runes | correctness | `internal/providers/tavily/tavily.go:123` | medium | `snippet[:300]` (tavily 123, firecrawl 120, ddgs 115) slices by bytes; if the 300th byte falls mid-rune the snippet ends with invalid UTF-8. Non-ASCII web results can produce mojibake before the trailing '...'. |
| Snippet 300-char truncation logic duplicated verbatim across three providers | quality | `internal/providers/firecrawl/firecrawl.go:119` | low | The identical `if len(snippet) > 300 { snippet = snippet[:300] + "..." }` appears in tavily.go:122, firecrawl.go:119, ddgs.go:114. A shared helper would centralize the limit and fix the UTF-8 issue once. |
| Redact only covers a fixed set of credential key names | security | `internal/safeerr/safeerr.go:14` | medium | The key=value regex matches only api_key/token/secret/password. Other common names (access_token, client_secret, x-api-key, refresh_token) pass through unredacted. The value char-class also terminates at the first comma/semicolon/brace/whitespace, so structured payloads with other delimiters could partially leak. |
| safeerr.Message silently drops wrapping context for HTTPStatusError | correctness | `internal/safeerr/safeerr.go:22` | medium | When err is a wrapped `*HTTPStatusError`, `errors.As` matches the inner error and Message returns only `statusErr.Error()`, erasing any outer `fmt.Errorf` prefix. Intentional (per `ddgs.go:71`) but an implicit, untested contract: future callers wrapping with meaningful context lose it on every surface. |
| safeerr.Message has zero direct test coverage | testing | `internal/safeerr/safeerr_test.go:8` | high | The only test exercises `Redact`. `Message`, including its nil-guard (19) and the `errors.As` HTTPStatusError branch that bypasses Redact (22-25), is never tested despite being the public entry point used by 8 call sites. A regression in the structured-error branch goes uncaught. |
| IPv6 private/loopback/link-local coverage is unverified by tests | coverage | `internal/safenet/url_test.go:76` | medium | All IP-block tests (76-130) use IPv4 literals only. `validateIP` relies on `net.IP` methods that do cover IPv6 and `ValidateURL` accepts bracketed IPv6, but no test asserts `fc00::/7`, `::1`, or `fe80::/10` are blocked. |
| IsBlockedHost denylist is minimal; internal aliases rely on the DNS path | security | `internal/safenet/url.go:85` | medium | The static DNS-free denylist is only `{localhost, localhost.localdomain}`. Other internal-name forms (`*.localhost`, `*.internal`, `*.local`, `metadata.google.internal`, rebinding hosts) depend wholly on later DNS resolution + `validateIP`. Consistent with the documented "not a complete SSRF sandbox" caveat but a concrete limit. |
| Python bench runner (cmd/bench/main.py) is orphaned from CI and the Go harness | coverage | `cmd/bench/main.py:232` | high | `main.py`/`test_main.py` are never invoked by Go code, Makefile, or CI (audit.sh runs only `go run . bench` + the claims script). It duplicates provider logic that the Go providers implement, can silently drift, and its test never runs in CI. |
| Python runner emits a routing matrix that can claim a single top provider | correctness | `cmd/bench/main.py:299` | medium | `main()` ranks providers by avg_score and prints matrix order. This output never passes through `sanitizeMarkdownCell` or `check-benchmark-claims.sh`, so if pasted into docs it would carry exactly the unsupported ranking/speed claim the Go-side guard blocks. |
| liveScore and offline scoreMetrics use unrelated, undocumented scales | quality | `internal/cli/bench.go:209` | medium | `liveScore` is a bespoke 40+30+30 heuristic; offline `scoreMetrics` averages 9 normalized metrics × 100; Python `score_result` (main.py:212) is a third formula. Three divergent 0-100 'score' scales feed the same Score/avg_score field with no shared definition. |
| Live extract case ignores resp.Content length asymmetrically vs comprehensive | correctness | `internal/cli/bench.go:185` | medium | `runLiveBenchCase` scores extract success on `resp.Content != ""` while `runComprehensiveOne` treats `TrimSpace(resp.Content) == ""` as failure (comprehensive.go:145). A whitespace-only extract is success in live mode but failure in comprehensive — inconsistent semantics. |
| sortedKeys hand-rolls insertion sort instead of using sort.Strings | quality | `internal/cli/bench.go:104` | medium | A manual O(n^2) insertion sort sorts provider names while the rest of the package uses `sort.Strings`/`sort.Slice` (bench.go:709, comprehensive.go:105). Dead-weight custom code duplicating stdlib with higher maintenance/risk. |
| Comprehensive run swallows cancellation/timeout without surfacing partial status | stability | `internal/bench/comprehensive.go:85` | medium | When ctx is cancelled mid-run the per-provider loop breaks (85-87) and the partial batch is flattened into a normal-looking Report with no field indicating truncation. A consumer cannot distinguish 'few fixtures' from 'aborted run', understating failure counts. |
| classifyComprehensiveError maps HTTP 202 to rate_limited | correctness | `internal/bench/comprehensive.go:193` | medium | The structured branch returns 'rate_limited' for 429 OR 202. 202 Accepted is not a rate-limit signal; bucketing it under rate_limited misattributes async/queued behavior as throttling in the error histogram. |
| MCP tool detection brittle-couples to slice formatting/order | stability | `scripts/verify-integration-evidence.sh:39` | high | Grep matches the literal sorted tool list from `doctor.go:136/452`; any tool add/rename silently sets `mcp_tools` unknown (line 38) then fails `check-integration-evidence.sh:29`. |
| check-docs-framing required list omits docs sibling guards need | docs | `scripts/check-docs-framing.sh:9` | medium | Omits `LIVE-VERIFICATION.md` (check-integration-evidence.sh:35/37) and `LIVE-BENCHMARK-PLAN/SUMMARY-TEMPLATE` (check-benchmark-claims.sh:11-12); deletion passes the framing loop but fails later in a downstream grep. |
| secret-scan whitelists every value shorter than 12 chars | security | `scripts/secret-scan.sh:56` | medium | `safe_value` passes any value under 12 chars, so short real credentials on TOKEN/SECRET/PASSWORD vars escape this leak-prevention scan. |
