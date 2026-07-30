# Nole architecture and dependency map

> Phase 1 mapping. Anchors are file:line at time of writing; verify before relying on stale anchors.

Nole is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools. It routes search and extraction requests to the best available provider under a spend-safe quota policy, without making any hidden paid calls. The router core is deterministic and LLM-free. MCP is one entrypoint (the `mcp` stdio server and `serve --mcp` HTTP server expose the same engine to coding agents), not the whole product — the same `core.Service` is reachable from the plain CLI subcommands (`search`, `extract`, `research`, etc.), an HTTP REST surface, and the bench harness.

---

## Binary and command tree

The process entrypoint is `main.go`, which builds the Cobra command tree and renders redacted errors to stderr (stdout stays clean on failure).

| Element | Anchor | Notes |
|---|---|---|
| `main` (process entry) | `main.go:11` | Builds root via `cli.NewRootCommand()`, executes, on error prints `safeerr.Message(err)` to stderr then `os.Exit(1)`. |
| `NewRootCommand` (cobra root) | `internal/cli/root.go:7` | `Use="nole"`, `SilenceUsage+SilenceErrors=true`. Registers all 14 subcommands. |

Subcommands (registered `internal/cli/root.go:18-31`):

| Subcommand | Constructor | Anchor | Role |
|---|---|---|---|
| `search` | `newSearchCommand` | `internal/cli/search.go:11` | Routed web search via `Service.Search`; text or `--json`, `--task`, `--limit`, `--insight`. |
| `classify` | `newClassifyCommand` | `internal/cli/plan.go:11` | JSON-only deterministic intent classification (`core.ClassifyQuery`), no provider calls. |
| `route-plan` | `newRoutePlanCommand` | `internal/cli/plan.go:44` | JSON-only deterministic route plan (`core.BuildRoutePlan`), no provider calls; reflects the configured TinyFish route tail when its key is present. |
| `extract` | `newExtractCommand` | `internal/cli/extract.go:11` | URL extraction via `Service.Extract` (SSRF-gated). |
| `research` | `newResearchCommand` | `internal/cli/research.go:15` | Multi-step Search+Extract pipeline, markdown synthesis, no LLM. |
| `bench` | `newBenchCommand` | `internal/cli/bench.go:21` | Offline/live/comprehensive eval + sanitized route-evidence. |
| `providers` | `newProvidersCommand` | `internal/cli/providers.go:10` | Provider status table / JSON via `Service.ProviderStatus`. |
| `doctor` | `newDoctorCommand` | `internal/cli/doctor.go:50` | Health + secret-presence + budget + optional `--mcp` smoke checks. |
| `config` | `newConfigCommand` | `internal/cli/config.go:111` | Read-only config/budget/key-presence dump; reports secret status as set/unset only. |
| `mcp` | `newMCPCommand` | `internal/cli/mcp.go:9` | stdio MCP transport: `server.ServeStdio(mcpserver.New(...))`. |
| `serve` | `newServeCommand` | `internal/cli/serve.go:9` | HTTP server (requires `--mcp`); exposes `/mcp` JSON-RPC + REST + `/health`. |
| `setup` | `newSetupCommand` | `internal/cli/setup.go:39` | Writes MCP client configs for 11 platforms (10 file writers + Claude instruction-only) + optional local Scrapling bootstrap. |
| `version` | `newVersionCommand` | `internal/cli/version.go` | Prints `version.Version`/`Commit`/`Date`; the runtime consumer for build-stamped identity. |
| `self-update` | `newSelfUpdateCommand` | `internal/cli/selfupdate.go` | Checks for and installs signed/checksummed release assets for the current platform. |

The `version` subcommand lets the CLI report build identity; the MCP server also reports `version.Version`. The `self-update` subcommand gives installed binaries a built-in upgrade path without requiring a source checkout.

---

## Package map

### Area: entry-cli — CLI entrypoint + composition root (`main`, `cli/app`, `cli/root`, `version`)

**Purpose.** Process entrypoint and dependency-composition root. `main.go` runs the Cobra tree and renders redacted errors to stderr. `cli/root.go` assembles the 14 subcommands. `cli/app.go` is the wiring layer that reads environment variables and constructs a fully-configured `core.Service` (provider registry, quota ledger, response cache, route matrix) plus shared JSON/insight/task-parsing helpers used by every subcommand. `internal/version` holds build-stamped identity vars consumed by the MCP server and the `version` CLI command.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `main` | func | `main.go:11` | Entrypoint; builds root, executes, prints redacted error to stderr + `os.Exit(1)`. stdout clean on failure. |
| `NewRootCommand` | func | `internal/cli/root.go:7` | Root cobra.Command; registers all 14 subcommands. |
| `defaultService` | func | `internal/cli/app.go:24` | Central composition root: loads `.env`, builds `core.Registry`, registers real-or-mock providers + keyless ddgs/scrapling, assembles quota ledger + optional cache, and uses `configuredRouteMatrix` so a configured TinyFish adapter is appended at each route tail. Discards every `registry.Register` error via `_ =`. |
| `defaultQuotaLedger` | func | `internal/cli/app.go:83` | Selects ledger backend: memory/off/none → in-memory; else file-backed at `NOLE_QUOTA_LEDGER_PATH` or default path, with memory fallback. Has a redundant error branch (lines 120-125). |
| `defaultQuotaLedgerPath` | func | `internal/cli/app.go:144` | Resolves on-disk ledger location; honors `XDG_STATE_HOME` only when absolute, else `~/.local/state/nole/quota-ledger.json`; "" when no home. |
| `providerQuotaEntry` | func | `internal/cli/app.go:205` | Maps provider+key-presence to a `core.QuotaEntry` cost class, including TinyFish's key-required, non-decrementing `KeyedFree` class before paid-mode evaluation. |
| `defaultCacheTTL` | func | `internal/cli/app.go:162` | Parses cache TTL from `NOLE_CACHE_TTL` or `NOLE_CACHE_TTL_SECONDS`; returns 0 (disabled) on parse failure / non-positive. |
| `parseTaskStrict` / `parseTask` | func | `internal/cli/app.go:357` | Strict (validation, plan.go) vs lenient (forgiving fallback to TaskGeneral, search.go) task-string parsing. `parseTask` at 349. |
| `buildCLIErrorWithInsightMode` | func | `internal/cli/app.go:268` | Builds `cliErrorEnvelope` JSON (operation, redacted error, defensively-copied route + route_trace, routing_insight). `buildCLIError` at 264. |
| `cliErrorEnvelope` | type | `internal/cli/app.go:256` | JSON error contract: operation, error, optional route, routing_insight, route_trace. |
| `writeJSON` / `writeJSONTo` | func | `internal/cli/app.go:246` | Shared 2-space-indented JSON encoders (stdout / any `io.Writer`). |
| `runSearch` | func | `internal/cli/app.go:388` | Thin search facade; rejects empty query, calls `defaultService().Search` with `context.Background()`. Builds a new Service per call. |
| `version` vars | var | `internal/version/version.go:3` | Build-stamped `Version`/`Commit`/`Date`. `Version` is consumed by the MCP server and the `version` CLI command; `Commit`/`Date` are consumed by the `version` CLI command and stamped by release-shaped builds. |

**Data flow.** `os/shell` → `main.main` → `cli.NewRootCommand().Execute()`. Service-backed commands call `defaultService()` → load provider-key presence → register real-or-mock adapters → seed the quota ledger → `configuredRouteMatrix(tinyfishKey)` → `core.NewService(...)`. `configuredRouteMatrix` deep-copies the canonical matrix: no TinyFish key is exactly equal to `DefaultRouteMatrix`, while a present key appends `tinyfish` without reordering the evidence-backed prefix. Output flows through the shared JSON/insight writers; errors are redacted before stderr.

### Area: core-routing — `internal/core` routing engine (route matrix, registry, deterministic planner, shared types, insight formatting)

**Purpose.** Deterministic, LLM-free decision core. Defines the task taxonomy and provider contracts (`types.go`), the evidence-derived task→provider route matrix + a quota-aware single-provider selector (`router.go`), a name-keyed provider registry (`registry.go`), a rule-based query classifier + route planner that produces routes without provider calls (`planner.go`), and the human-readable "Nole:" routing-insight string builders (`insight.go`). Separates "what provider should handle this" from actual provider I/O (which lives in `service.go`).

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `RouteMatrix` | type | `internal/core/router.go:3` | `map[TaskType][]string` — ordered fallback chain per task. |
| `DefaultRouteMatrix` | func | `internal/core/router.go:5` | Canonical per-task provider ordering (routing prior from benchmark evidence). Extract route excludes brave/ddgs, leads with local scrapling. |
| `Router` | type | `internal/core/router.go` | Holds registry + QuotaLedger + matrix; the route decision unit. |
| `Router.Route` | method | `internal/core/router.go` | Resolves task→route with TaskGeneral fallback and returns a defensive route copy. |
| `Router.Candidate` / `Router.Candidates` | methods | `internal/core/router.go` | Annotate provider slot(s) with registration, capability, and quota-policy gate results. `Service.Search`/`Extract` use the single-slot `Candidate` path lazily so later-provider quota decisions are not made after an earlier success. |
| `Router.Select` | method | `internal/core/router.go` | Compatibility/convenience wrapper over `Route` + lazy `Candidate`; returns first routable provider + full route, else `NoFreeQuotaError`. |
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

**Data flow.** Two surfaces. (1) Planning (no I/O): `BuildRoutePlan` (`planner.go:144`) → `ClassifyQuery` (`planner.go:102`) → `normalizeQuery`+`scoreIntents` over `plannerRules` → per-intent `routeForPlan` (`planner.go:234`) reads `RouteMatrix`/override → `PlannedRoute[]` + 'planned' RouteTrace → `BuildRoutePlanRoutingInsight`. (2) Selection: `Router.Route` resolves task fallback, `Router.Candidate` performs registration/capability/quota gates for one provider slot, and `Router.Select` is a thin first-routable-provider wrapper for callers that do not need runtime traces. At runtime `service.go` consumes `Router.Candidate` lazily while walking `Router.Route`; Service still owns provider `Status(ctx)`, provider calls, `ledger.Record`, cache, and RouteTrace/insight construction.

### Area: core-state — `internal/core` cost/quota ledger, response cache, BYOK metadata, setup hints, file locking, typed errors

**Purpose.** Implements the "no hidden paid spend" guarantee. The quota ledger classifies every provider into a cost class and decides, under a configurable cost policy (free-first / cost-capped / quality-first), whether a call is allowed, persisting free-tier counters and paid spend to a crash-safe, file-locked JSON ledger. `keyed-free` represents key-required APIs with no modeled monthly credit balance: it is allowed under every policy and `Record` is a no-op, including fail-closed ledger states. Supporting pieces include the cache, authoritative keyed-provider metadata, setup hints and typed no-free-quota errors.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `MemoryQuotaLedger` | type | `internal/core/quota.go:132` | Core ledger struct (also backs file ledger). Mutex-guarded; `*Locked` methods assume the lock held. |
| `QuotaLedger` | interface | `internal/core/quota.go:87` | Public contract: Allow, Decide, Record, RecordDrift, Get, Entries, BudgetStatus. |
| `RecordDrift` | method | `internal/core/quota.go:392` | Observability-only (v0.7.0): upserts a per-provider drift signal (provider returned 429 while local FreeRemaining>0). Never debits, never changes routing; reload-merges + persists under the file lock; best-effort. Surfaced in BudgetStatus, aged out of output after 24h. |
| `decideLocked` | method | `internal/core/quota.go:221` | Policy engine; maps (CostClass × CostPolicy) → Allowed + Reason; honors `failClosedReason`. Default branch (285) coerces unknown classes. |
| `Record` | method | `internal/core/quota.go:325` | Atomic charge: file lock, re-read disk (defeats stale instances), decrement FreeRemaining / add SpentCents, persist. On reload failure marks unavailable + fails closed. |
| `recordLocked` | method | `internal/core/quota.go:340` | Refreshes expired monthly quotas, re-decides, mutates per cost class, persists with rollback (348). |
| `reloadFromDiskLocked` | method | `internal/core/quota.go:599` | Reads + validates JSON ledger (schema 1..2), merges, refreshes, migrates v1→v2, persists when changed. Missing file = fresh OK ledger. |
| `mergeLedgerEntries` | func | `internal/core/quota.go:687` | Merges disk onto seeds; drops orphans; carries counters across cost-class transitions; takes `max(SpentCents)`. |
| `refreshExpiredEntriesLocked` | method | `internal/core/quota.go:757` | Refills FreeRemaining for monthly entries whose PeriodStart < current YYYY-MM. |
| `persistLocked` | method | `internal/core/quota.go:783` | Crash-safe write: mkdir 0700, write .tmp 0600, rename, chmod 0600. Schema v2. |
| `withFileLockLocked` | method | `internal/core/quota.go:657` | Wraps fn in exclusive advisory lock on `<path>.lock`; no-op for in-memory. |
| `recoverCorruptLedgerLocked` | method | `internal/core/quota.go:640` | On parse error / bad schema: backup, RecoveredCorrupt state + `ledger_corrupt_fail_closed`, pathless warning, persist. |
| `normalizeQuotaEntry` | func | `internal/core/quota.go:858` | Infers CostClass, syncs flags, clamps negatives to 0. |
| `premiumWithinCap` | func | `internal/core/quota.go:887` | Cost-capped gate: blocks if no cap / unknown cost / total+estimated > HardCapCents. |
| `MemoryResponseCache` | type | `internal/core/cache.go:22` | Mutex-guarded TTL cache w/ injectable clock; lazy eviction on read; nil/ttl<=0 safe. |
| `searchCacheKey` | func | `internal/core/cache.go:112` | NUL-joined key from task + normalized query + limit + canonical `SearchOptions`; empty task → TaskGeneral. |
| `cloneSearchResponse` | func | `internal/core/cache.go:145` | Defensive deep-ish copy (Results/Route/RouteTrace) so callers can't mutate stored entries. |
| `lockLedgerFile` (unix) | func | `internal/core/file_lock_unix.go:10` | Blocking exclusive `flock(LOCK_EX)`; build-tag `!windows`. |
| `lockLedgerFile` (windows) | func | `internal/core/file_lock_windows.go:19` | `LockFileEx` `LOCKFILE_EXCLUSIVE_LOCK` (0x2) over 1 byte; build-tag `windows`. |
| `byokProviders` | var | `internal/core/byok_metadata.go:22` | Authoritative keyed-provider metadata (brave, tavily, firecrawl, tinyfish): default cost class, quota/refresh where applicable, capabilities and setup fields. TinyFish is request-rate-only `keyed-free` with zero quota/refresh. Never mutate after init. |
| `BuildSetupSuggestions` | func | `internal/core/setup_hints.go:15` | One suggestion per missing BYOK key, classified high/medium/low, sorted. |
| `BuildSetupTip` | func | `internal/core/setup_hints.go:103` | Once-per-session upgrade nag from high/medium suggestions; nil when nothing missing or only low-impact. |
| `NoFreeQuotaError` | type | `internal/core/errors.go:5` | Typed no-free-provider error; `IsNoFreeQuota` handles value + pointer. |

**Data flow.** Wiring builds seeds from `LookupBYOK`, carrying cost-class and metering metadata into the ledger. Hot path is `Decide(provider)` → refresh → cost-class/policy decision. On a committed tracked call, `Record(provider)` re-reads disk under lock, decrements/adds and persists; `keyed-free` returns before any mutation. Corruption triggers backup + fail-closed, while both `keyless-free` and `keyed-free` remain callable because neither can consume tracked/paid balance. Setup hints read `BYOKProviders()` vs configured state; TinyFish's missing key stays low-impact and is excluded from the ordinary once-per-session setup tip.

### Area: core-service — `internal/core` Service orchestration (Search/Extract/ProviderStatus/BudgetStatus)

**Purpose.** Central orchestrator turning a Search/Extract request into a routed, quota-aware, cache-aware provider call. Walks the per-task route applying capability checks, cost-policy quota decisions, and availability gates; falls back across providers on failure/empty; emits a per-attempt RouteTrace + human-readable RoutingInsight. Owns the money-safety invariant: quota is debited only on a successful response.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `Service` | type | `internal/core/service.go:26` | Orchestrator: registry, ledger, router, optional cache. |
| `NewService` | func | `internal/core/service.go:47` | Builds Service + internal Router; applies variadic nil-guarded `ServiceOption`. |
| `WithResponseCache` | func | `internal/core/service.go:41` | Option injecting a ResponseCache (otherwise nil/skipped). |
| `Service.Search` | method | `internal/core/service.go:57` | Resolves/defaults task, validates + normalizes `SearchOptions`, checks cache keyed by canonical options, iterates route w/ registration/capability/quota/availability gates, calls `provider.Search`, falls back on error/empty, records quota only on success, returns response+trace or `NoFreeQuotaError`. |
| `Service.Extract` | method | `internal/core/service.go:260` | Mirrors Search: trims URL, defaults Format→markdown, runs `safenet.ValidateURL` (SSRF guard) before routing, same pipeline on TaskExtract route. |
| `Service.ProviderStatus` | method | `internal/core/service.go:437` | Lists providers, calls Status each, merges quota decision, annotates DriftWarning from recent drift signals, computes BYOK suggestions. Provider Status() also folds breaker `IsOpen()` into Available (Reason `circuit_open`) + reports raw `breaker_state`. |
| `Service.BudgetStatus` | method | `internal/core/service.go:473` | Thin delegate to `ledger.BudgetStatus()`. |
| `Service.routeFor` | method | `internal/core/service.go` | Thin delegate to `Router.Route`, keeping task fallback in one place. |
| `attemptWithDecision` | func | `internal/core/service.go:509` | Core RouteAttempt builder folding QuotaDecision + latency + count. |
| `cacheHitAttempt` | func | `internal/core/service.go:493` | Builds cache_hit attempt; provider name defaults to 'cache'. |
| `mergeProviderCostStatus` | func | `internal/core/service.go:522` | Copies QuotaDecision cost/policy fields onto a ProviderStatus. |
| `contentResultCount` | func | `internal/core/service.go:533` | 1 for non-blank extract content, else 0. |

**Data flow.** Caller (CLI/MCP) → `Service.Search`/`Extract`. Both normalize and prevalidate, consult the canonical-options cache, then walk route slots through registration/capability/quota/status gates. Provider adapters forward only documented SearchOptions: Brave all five, Tavily/Firecrawl country+freshness, TinyFish country+search_lang+freshness, and keyless search providers ignore unsupported fields. Errors/empty outputs fall through. Only successful calls reach `ledger.Record`; `keyed-free` succeeds without mutating counters. The service then builds trace/insight, caches successful normalized output and returns.

### Area: cli-commands — `internal/cli` command surface (search, extract, classify/route-plan, providers, doctor, research, HTTP serve, .env loader)

**Purpose.** Cobra command layer. Each file builds command constructors translating flags/args into `core.Service` calls or pure `core` planners, then renders text or JSON. Hosts the `serve` HTTP handler (REST + MCP JSON-RPC bridge), the `doctor` health/MCP-smoke diagnostic, the `research` pipeline, and the shell-style `.env` loader that primes provider keys before the service is constructed.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `newSearchCommand` | func | `internal/cli/search.go:11` | `search <query>`; `--task/--limit/--json/--insight`; renders text or JSON. |
| `taskHelpText` | func | `internal/cli/search.go:50` | Builds `--task` help from `core.TaskTypes()`, skipping TaskExtract. |
| `newExtractCommand` | func | `internal/cli/extract.go:11` | `extract <url>`; `Service.Extract` w/ `--format`; mirrors search error/JSON handling. |
| `newClassifyCommand` | func | `internal/cli/plan.go:11` | `classify <query>`; JSON-only `core.ClassifyQuery`. |
| `newRoutePlanCommand` | func | `internal/cli/plan.go:44` | JSON-only deterministic route plan over `configuredRouteMatrix`; no provider calls, and no-key output stays identical to the canonical matrix. |
| `planOptionsFromFlags` | func | `internal/cli/plan.go:79` | `--task/--providers/--single-intent` → `core.PlanOptions`; rejects TaskExtract override + invalid providers. |
| `validPlannerProvider` | func | `internal/cli/plan.go:122` | Allowlist: brave, tavily, firecrawl, ddgs. |
| `newProvidersCommand` | func | `internal/cli/providers.go:10` | `providers`; tab rows or JSON from `ProviderStatus`. |
| `newDoctorCommand` | func | `internal/cli/doctor.go:50` | `doctor`; binary/stdio notes, provider table, secret presence, paid warnings, budget, optional `--mcp`. |
| `writePaidModeWarnings` | func | `internal/cli/doctor.go:31` | Warnings for `NOLE_<P>_PAID` mode + Brave subscription/CC note. |
| `checkMCPStdioSmoke` | func | `internal/cli/doctor.go:165` | Redirects os.Stdout to pipe, builds `mcpserver.New`, asserts 0 stdout bytes at startup. |
| `checkMCPProtocolSmoke` | func | `internal/cli/doctor.go:229` | Spawns `<binary> mcp`, runs initialize + tools/list handshake (10s timeout), validates tools + JSON-only stdout. |
| `rpcIDMatches` | func | `internal/cli/doctor.go:421` | Matches a JSON-RPC id (number or numeric string) against wanted int. |
| `newResearchCommand` | func | `internal/cli/research.go:12` | `research <question>`; thin wrapper over `core.Service.ResearchWithOptions` w/ `--max-steps` plus optional SearchOptions flags for internal search passes; prints sources + short extract previews (no summary). |
| `printResearchReport` | func | `internal/cli/research.go:54` | Human view: header + sources + short extract previews (full content in `--json`). |
| `Service.Research` / `Service.ResearchWithOptions` | methods | `internal/core/research.go` | Classifies the question, fans `Service.Search` across task-fit routes, passes normalized SearchOptions to each search leg, extracts `min(sources, max_steps, 5)` non-pdf/non-reddit URLs (truncate 2000); returns evidence (sources + extracts) — NO composed summary (the agent synthesizes). |
| `Service.SearchAndExtract` | method | `internal/core/service.go` | One search + extract of the top `min(extract_top, 3)` result URLs; per-URL failure is non-fatal (`extract_errors`), URLs de-duplicated. |
| `newHTTPHandler` | func | `internal/cli/http.go:37` | Wraps `core.Service` + `mcpserver.New` via `buildMCPServer`. |
| `httpHandler.buildMux`/`start` | method | `internal/cli/http.go:57`/`276` | `buildMux` registers `/health`, `/mcp`, `/api/{providers,budget,search,extract,search_and_extract,research}` w/ 1MiB caps + slowloris timeouts; `start` runs it with graceful drain. `/health` is a REAL readiness check (v0.7.0): 200 iff ≥1 search-capable provider is Available && AllowedByPolicy (Available already folds breaker IsOpen), else 503; keyless DDGS keeps a zero-key deploy ready. |
| `httpHandler.handleMCP` | method | `internal/cli/http.go:334` | POST-only JSON-RPC bridge → `mcp.HandleMessage`; `-32700`/`-32603` envelopes on failure. |
| `httpBuildContext` | func | `internal/cli/http.go:405` | Injects `InProcessSession` if `Mcp-Session-Id` present, else sets `mcpserver.EphemeralCtxKey`. |
| `httpSessionForRequest` | func | `internal/cli/http.go:247` | Legacy helper retained only for tests (deprecated path). |
| `loadDefaultNoleEnvFile` | func | `internal/cli/env_file.go:8` | Loads default `.env` (unless `NOLE_DISABLE_ENV_FILE`); called from `app.go:25` and `bench.go:118`. |
| `parseShellEnvAssignment` | func | `internal/cli/env_file.go:45` | Parses `KEY=VALUE` (supports `export `, comments, quotes); existing env wins. |
| `parseDoubleQuotedShellValue` | func | `internal/cli/env_file.go:121` | Double-quoted shell values w/ escapes + `os.ExpandEnv`, preserving `\$`. |

**Data flow.** Entry is `root.go:18-31`. Before any service call, `app.go:25` calls `loadDefaultNoleEnvFile()`. `search.go`/`extract.go` parse `--insight` (`app.go:282`), call `runSearch`→`Service.Search`/`Service.Extract`, apply `applyXxxInsightMode`, then `writeJSONTo` or `writeHumanRoutingInsight`. On error with `--json` they emit `buildCLIErrorWithInsightMode`. `plan.go` classify/route-plan call pure core planners (provider-free). `research.go` is a thin wrapper over `core.Service.ResearchWithOptions`, which classifies the question, prevalidates/canonicalizes optional SearchOptions, fans `Service.Search` across task-fit routes with those options, dedupes, and `Service.Extract`s `min(sources, max_steps, 5)` URLs, returning evidence (sources + extracts) with no composed summary; `core.Service.SearchAndExtract` does one search + top-N extract. `http.go` re-exposes Service over REST (incl. `/api/search_and_extract`, `/api/research`) + MCP bridge, with `route_trace` opt-in via `include_trace`. `doctor.go --mcp` runs `checkMCPStdioSmoke` (0 stdout) + `checkMCPProtocolSmoke` (subprocess handshake).

### Area: cli-setup — `nole setup` MCP client config writers + local-extract Scrapling bootstrap

**Purpose.** Implements `nole setup`, registering Nole as an MCP server across eleven AI coding agents (Claude, Cursor, Codex, OpenCode, Kimi, Windsurf, Hermes, Antigravity, Gemini, Grok superagent, Grok Build) and optionally bootstrapping an isolated local Scrapling runtime. Each file-writing agent has a bespoke or shared writer that merges Nole's entry into the agent's native user-scope config (JSON/TOML/YAML) preserving unknown fields, file permissions, a `.bak` backup, and writing atomically. `--local-extract` provisions a Python venv, installs `scrapling[fetchers]`, persists `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env`, and emits an env-sourcing shell wrapper.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `newSetupCommand` | func | `internal/cli/setup.go:39` | Builds `setup`; parses agent flags, resolves binary path, builds launchSpec, runs local-extract first, dispatches per-agent writers, prints count + key hints. |
| `launchSpec` | type | `internal/cli/setup.go:20` | `{Binary, Wrapper}`; `command()`/`args()` (25,32) return wrapper alone or bare binary + `[mcp]`. |
| `printClaudeInstructions` | func | `internal/cli/setup.go:221` | Claude is instruction-only: NO file; prints `claude mcp add nole -s user -- ...`. |
| `writeHermesConfigPath` | func | `internal/cli/setup.go:270` | YAML-node writer; backs up, preserves comments, upserts `mcp_servers.nole`, atomic-write preserving mode. |
| `upsertHermesNoleServer` | func | `internal/cli/setup.go:331` | Sets command+args, adds default timeout=120/connect_timeout=60 if missing, ensures tools policy. |
| `yamlMappingUpsert` | func | `internal/cli/setup.go:386` | Replace/append key in yaml.Node mapping, carrying Head/Line/Foot comments — comment-preserving idempotency core. |
| `writeMCPJSONConfig` | func | `internal/cli/setup.go:813` | Shared Cursor/Windsurf/Gemini writer; preserves unknown root fields and sibling servers while replacing only `nole`. Antigravity uses a policy-preserving specialized JSON upsert. |
| `writeCodexConfigPath` | func | `internal/cli/setup.go:847` | TOML writer via `upsertCodexTomlTable`; block by `codexMCPServerBlock` embedding `/bin/sh -lc 'set -a; . .env; exec nole mcp'` unless wrapper set. |
| `upsertCodexTomlTable` | func | `internal/cli/setup.go:917` | Hand-rolled TOML table replacement; strips preceding comment/blank lines so marker doesn't accumulate. |
| `writeOpenCodeConfigPath` | func | `internal/cli/setup.go:974` | Merges into `mcp` JSON key with `{type:local, command:[bin,mcp], enabled, environment}`. |
| `writeKimiConfigPath` | func | `internal/cli/setup.go:1047` | Merges into `mcpServers`; `{command}` in wrapper mode or `{command,args}` bare. |
| `atomicWriteFile` | func | `internal/cli/setup.go:1176` | Durability primitive: temp file same dir, chmod, sync, close, rename, re-chmod; default mode 0600. |
| `configWriteMode` | func | `internal/cli/setup.go:1167` | Preserves existing perms on rewrite, else 0600 (no widening secret configs). |
| `readExistingFileWithMode` | func | `internal/cli/setup.go:1145` | Stat+read returning (bytes, exists, perm, err); ENOENT → exists=false. |
| `setupLocalExtract` | func | `internal/cli/setup_local_extract.go:26` | Bootstraps Scrapling runtime: resolve venv+python, gate >=3.10, create venv, verify-or-pip-install, persist `NOLE_SCRAPLING_PYTHON`. |
| `resolveSetupPython` | func | `internal/cli/setup_local_extract.go:92` | `--python` value or LookPath-probes `python3.13..3.10, python3, python`. |
| `upsertShellEnvAssignment` | func | `internal/cli/setup_local_extract.go:171` | Idempotent `KEY=value` upsert into `.env`; values shell-quoted. |
| `writeMCPWrapper` | func | `internal/cli/setup_local_extract.go:195` | Emits 0700 POSIX `/bin/sh` wrapper sourcing `.env` then exec'ing the binary (exit 127 if none). |
| `shellQuote` | func | `internal/cli/setup_local_extract.go:229` | Single-quote shell escaping for env values + wrapper binary path. |
| `venvPythonPath` | func | `internal/cli/setup_local_extract.go:133` | `Scripts/python.exe` on Windows, `bin/python` elsewhere. |

**Data flow.** `root.go` → `AddCommand(newSetupCommand)`. `RunE`: `--all` sets every client bool; reject if none + no `--local-extract`; `os.Executable` → `filepath.Abs` → `launchSpec`; validate that a supplied wrapper is absolute. If `--local-extract`: `setupLocalExtract` → resolve venv/python/version → venv create → verify-or-install Scrapling → `writeNoleEnvValue(NOLE_SCRAPLING_PYTHON)`; then default wrapper to `~/.local/bin/nole-mcp` + `writeMCPWrapper`. Per-agent dispatch: Claude → instructions only; Cursor/Windsurf/Gemini → shared MCP JSON writer; Antigravity → policy-preserving MCP JSON upsert; Codex → TOML writer; OpenCode/Kimi/Grok → native JSON writers; Grok Build → TOML writer; Hermes → YAML writer. Each file writer resolves home, reads and validates existing config before backup, merges the Nólë entry while preserving its documented unknown fields/options, then atomically writes with the prior mode. Writer errors go to stderr, skip `configured++`, and make setup exit non-zero.

### Area: mcp — MCP server surface (stdio + HTTP) and tool registration (`internal/cli/{mcp.go,serve.go}` + `internal/mcpserver/*`)

**Purpose.** Exposes the engine as a Model Context Protocol server over two transports: stdio (`nole mcp`) for local single-process agent usage, and Streamable-HTTP (`nole serve --mcp`) for team/remote usage. Constructs an `mcp-go` server, registers six tools (search, extract, search_and_extract, provider_status, budget_status, research — extract and search_and_extract gated on an available extract-capable provider) wrapping `core.Service`, serializes responses/errors as JSON; `search`/`extract`/`search_and_extract` omit `route_trace` by default unless `include_trace` is set. Central concern: once-per-session emission of a setup_tip with transport-aware semantics (stdio one-per-process, HTTP persistent-per-session, HTTP ephemeral-always) and a bounded dedup map.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `New` | func | `internal/mcpserver/server.go:9` | Builds mcp-go MCPServer "nole" at `version.Version`, tool caps disabled, then `RegisterTools`. Shared by both transports. |
| `RegisterTools` | func | `internal/mcpserver/tools.go:80` | Registers six tools, binds handler closures over `*core.Service`; owns the per-server `tipState` dedup map (closure-captured). |
| `providerStatusToolResponse` | type | `internal/mcpserver/tools.go:39` | MCP-only additive envelope that exposes `server_version` beside the unchanged core provider-status fields. |
| `HasExtractCapableConfigured` | func | `internal/mcpserver/tools.go:63` | Reports whether a keyed or local higher-fidelity extract provider is configured; doctor and setup hints use it, while MCP advertisement uses the service registry capability. |
| `hashSessionID` | func | `internal/mcpserver/tools.go:48` | SHA-256→hex of client session ID; used only as dedup-map key (anti-memory-inflation). |
| `tipStateMaxEntries` | const | `internal/mcpserver/tools.go:37` | Caps tip-dedup map at 1000; beyond cap new IDs fail-open without recording. |
| `EphemeralCtxKey` | type | `internal/mcpserver/session.go:19` | Empty-struct context key signaling a stateless HTTP request. Set in `http.go:233`, read in `tools.go:120`. |
| `toolErrorJSON` | func | `internal/mcpserver/errors.go:18` | Sanitized JSON error envelope (operation, `safeerr.Message`, route, insight, trace); falls back to hand-built JSON on marshal failure. |
| `toolErrorEnvelope` | type | `internal/mcpserver/errors.go:10` | Struct of the sanitized tool-error payload w/ omitempty. |
| `buildTaskDescription` | func | `internal/mcpserver/tools.go:229` | Generates the search tool's `task` description from `core.TaskTypes()`, skipping TaskExtract. |
| `newMCPCommand` | func | `internal/cli/mcp.go:9` | `nole mcp`; runs `server.ServeStdio(mcpserver.New(defaultService()))`. |
| `newServeCommand` | func | `internal/cli/serve.go:9` | `nole serve`; requires `--mcp`, builds `httpHandler`, starts it, prints `/mcp` + `/health` URLs. |
| `searchToolDescription` | const | `internal/mcpserver/tools.go:21` | Long NL description for the search tool (paired with `extractToolDescription` at 22). |

**Data flow.** Two transports converge on `mcpserver.New`. (1) stdio: `newMCPCommand` (`mcp.go:14`) → `mcpserver.New(defaultService())` → `RegisterTools` → `ServeStdio` reads JSON-RPC from stdin. (2) HTTP: `newServeCommand` (`serve.go:22`) → `newHTTPHandler(svc)` (`cli/http.go:26`) → `handler.start(addr)`; each request's `httpBuildContext` injects in-process session (header present) or sets `EphemeralCtxKey=true`, then `HandleMessage` dispatches to the tool closure. Search handler: `RequireString(query)` → `svc.Search`; on error → `NewToolResultError(toolErrorJSON("search", err, resp.Route, resp.RouteTrace))`. On success: tip decision — ephemeral→always emit; else session key from `ClientSessionFromContext().SessionID()` or `"stdio-default"`, hashed, checked/recorded under `tipState` mutex (fail-open beyond cap). If shouldEmit → `svc.ProviderStatus` → `core.BuildSetupTip`. Response marshaled via `json.MarshalIndent` → `NewToolResultText`. Extract tools register when `svc.HasExtractCapableProvider()` is true, which the keyless `httpfetch` backstop guarantees in the default service. The `provider_status` handler wraps the core response at the MCP boundary with `server_version`; CLI provider JSON remains on the core envelope.

### Area: providers — web search/extract provider adapters + shared HTTP retry/error layer

**Purpose.** Concrete adapters satisfying `core.Provider`, translating Nole's neutral request/response types into each upstream API and back. Four keyed network providers (brave, tavily, firecrawl, tinyfish); keyless network providers — `ddgs`, `wikipedia`, `arxiv`, and `httpfetch`; one local subprocess extractor (`scrapling`); a deterministic mock; and shared `providerhttp` retry/breaker/safe-error primitives. TinyFish also reuses `safenet` before forwarding fetch targets. Provider-specific auth, decoding and rate-limit behavior stays isolated per adapter.

| Symbol | Kind | Anchor | Summary |
|---|---|---|---|
| `providerhttp.DoWithRetry` | func | `internal/providers/providerhttp/retry.go:40` | Core retry loop: clones request per attempt, calls `client.Do`, returns on transport error or non-transient status, else drains+sleeps backoff. Network adapters opt into it instead of duplicating retry logic. |
| `providerhttp.RetryOptions` | type | `internal/providers/providerhttp/retry.go:13` | Tunable config (MaxAttempts, BaseDelay, MaxDelay, injectable Sleep). |
| `providerhttp.DefaultRetryOptions` | func | `internal/providers/providerhttp/retry.go:20` | From env (`NOLE_RETRY_MAX_ATTEMPTS` clamped 1..5, `NOLE_RETRY_BASE_DELAY_MS`), 5s MaxDelay. |
| `providerhttp.retryDelay` | func | `internal/providers/providerhttp/retry.go:92` | Honors Retry-After (seconds or HTTP date), else exponential `base*2^(attempt-1)` capped. |
| `providerhttp.isTransientStatus` | func | `internal/providers/providerhttp/retry.go:121` | Retryable: 429, 502, 503, 504. Excludes 408 and 202. |
| `providerhttp.cloneRequestForAttempt` | func | `internal/providers/providerhttp/retry.go:72` | Replays body via GetBody; 'not replayable' error if body w/o GetBody on attempt>1. |
| `providerhttp.HTTPStatusError` | type | `internal/providers/providerhttp/errors.go:9` | Public-safe non-2xx error: stores only provider/operation/status/body-size/category, never raw body. |
| `providerhttp.NewHTTPStatusError` | func | `internal/providers/providerhttp/errors.go:17` | Records `len(body)` + derived category; returned by brave/tavily/firecrawl/ddgs on non-200. |
| `providerhttp.statusCategory` | func | `internal/providers/providerhttp/errors.go:38` | Classifies status into auth/transient/server/client/unexpected. |
| `brave.Provider.Search` | method | `internal/providers/brave/brave.go:81` | GET w/ `X-Subscription-Token`; clamps count to the selected endpoint cap; forwards supported `SearchOptions`; uses Web Search for general/non-recency tasks and News Search for `news`/`factcheck`; maps provider `web.results` or News Search top-level `results`. |
| `brave.braveSearchEndpoint` / `brave.braveCountMax` / `brave.braveFreshness` / `brave.clampRange` | funcs | `internal/providers/brave/brave.go:190` / `:206` / `:216` / `:228` | Endpoint + count + freshness shaping for Brave Search; recency tasks use News Search + `freshness=pm`; count is clamped to Web's 1..20 or News' 1..50 range. |
| `brave.New` | func | `internal/providers/brave/brave.go:28` | Functional-options; falls back to `BRAVE_API_KEY` then `BRAVE_SEARCH_API_KEY`. |
| `tavily.Provider.Search` | method | `internal/providers/tavily/tavily.go:69` | POST JSON; advanced depth for TaskResearch else basic; maps `country` + freshness→`time_range`; default limit 5; snippet trunc 300. |
| `tavily.Provider.Extract` | method | `internal/providers/tavily/tavily.go:156` | POST single URL to `/extract`; returns first result's `raw_content`. |
| `firecrawl.Provider.Search` | method | `internal/providers/firecrawl/firecrawl.go:76` | POST `{baseURL}/search`; maps `country` + freshness→`tbs`; snippet Description→Markdown then trunc 300. |
| `firecrawl.Provider.Extract` | method | `internal/providers/firecrawl/firecrawl.go:154` | POST `/scrape`; markdown content + string-only metadata. |
| `firecrawl.WithBaseURL` | func | `internal/providers/firecrawl/firecrawl.go:29` | Overrides default `https://api.firecrawl.dev/v2`; used by tests. |
| `tinyfish.Provider.Search` | method | `internal/providers/tinyfish/tinyfish.go` | GET `https://api.search.tinyfish.ai/` with `X-API-Key`; maps only documented task/country/language/freshness fields, requests page 0 once, trims locally and never fabricates score. |
| `tinyfish.Provider.Extract` | method | `internal/providers/tinyfish/tinyfish.go` | SSRF-prevalidates one public target, POSTs to `https://api.fetch.tinyfish.ai/`, maps markdown/html/JSON text plus an allowlisted metadata subset, and reduces per-URL failures to sanitized codes. |
| `ddgs.Provider.Search` | method | `internal/providers/ddgs/ddgs.go:40` | Scrapes DDG no-JS HTML via POST w/ browser headers; regex-parses links/snippets; skips ad redirects; special-cases 202 as rate-limit. |
| `ddgs.cleanHTML` | func | `internal/providers/ddgs/ddgs.go:150` | Strips tags + decodes a hand-picked entity set. |
| `ddgs` regex set | var | `internal/providers/ddgs/ddgs.go:33` | Package-level compiled regexes driving the brittle scrape. |
| `wikipedia.Provider.Search` | method | `internal/providers/wikipedia/wikipedia.go:108` | Keyless MediaWiki Action API (`list=search`); breakered; reinforces factcheck/people/academic before the ddgs fallback. Manual breaker mgmt (maxlag is a 200+error-body). |
| `arxiv.Provider.Search` | method | `internal/providers/arxiv/arxiv.go:184` | Keyless arXiv Atom API; breakered; reinforces academic only. stdlib `encoding/xml`; single-connection + >=3s pacing limiter per arXiv ToU; refuses to clobber a customized entry. |
| `httpfetch.Provider.Extract` | method | `internal/providers/httpfetch/httpfetch.go:123` | Keyless pure-Go extract backstop (last-resort on TaskExtract); no-follow redirect walk re-validated by safenet (dial-time IP guard); stdlib HTML-to-text. |
| `scrapling.Provider.Extract` | method | `internal/providers/scrapling/scrapling.go:60` | Runs embedded Python via `exec.CommandContext`, pipes JSON on stdin, decodes `{content,metadata}`; distinguishes timeout, sanitizes stderr. |
| `scrapling.extractScript` | const | `internal/providers/scrapling/scrapling.go:141` | Embedded Python using `scrapling.fetchers.Fetcher` + HTMLParser fallback. |
| `scrapling.sanitizeError` | func | `internal/providers/scrapling/scrapling.go:133` | Trims + caps subprocess stderr at 500 chars. |
| `mock.Provider` | type | `internal/providers/mock/mock.go:10` | Deterministic placeholder; `New`/`NewUnavailable` toggle availability; used when keys absent. |

**Data flow.** Wiring: `app.go:53-94` constructs each provider (real w/ `WithAPIKey` if env key present, else `mock.NewUnavailable` for brave/tavily/firecrawl; ddgs, wikipedia, arxiv (keyless search), scrapling, and httpfetch (keyless extract backstop) always registered — wikipedia/arxiv carry circuit breakers since they are routed before the ddgs fallback). `bench.go:131-138` builds a parallel map of all providers. `core.Router`/`Service` select by Task+Capability then call Search/Extract. Each network adapter: guard missing key → build request (GET brave, POST+JSON tavily/firecrawl, POST+form ddgs) → `DoWithRetry(ctx, client, req, DefaultRetryOptions())` → non-200 → `NewHTTPStatusError` (body discarded, size kept) → `json.Decode` → map to `core.SearchResult`/`ExtractResult` w/ Provider tag + 300-char snippet trunc. ddgs diverges: reads HTML, regex-extracts, treats 202 as a locally-built sanitized rate-limit error (documented at `ddgs.go:69-78` because `safeerr.Message` unwraps to `HTTPStatusError.Error()`). scrapling diverges: no HTTP, shells out to Python with per-call timeout. Errors render through `safeerr.Message` for redaction.

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
| `runComprehensiveBench` | func | `internal/cli/bench.go:117` | CLI adapter; loads env, uses `ComprehensiveFixtureSet`, builds the provider map, and calls `RunComprehensiveLive` with a two-second TinyFish spacing floor. |
| `comprehensiveBenchProviders` | func | `internal/cli/bench.go:126` | Fixed provider map including TinyFish and all existing comparator adapters (router/policy/ledger bypassed). Missing TinyFish key fails locally before network I/O. |
| `runLiveBench` | func | `internal/cli/bench.go:136` | Live smoke; clamps maxCases (0,10]→3, truncates fixtures, runs through real `core.Service`. |
| `runLiveBenchCase` | func | `internal/cli/bench.go:172` | One live fixture via `svc.Extract`/`svc.Search`; records route, sanitized attempts, coarse `liveScore`. |
| `liveScore` | func | `internal/cli/bench.go:209` | Coarse 0-100: 40 base + up to 30 for count + latency bucket bonus. |
| `sanitizedBenchError` | func | `internal/cli/bench.go:232` | Wraps `safeerr.Message`, truncates to 160 chars. |
| `sortedKeys` | func | `internal/cli/bench.go:104` | Generic map-key sorter via hand-rolled insertion sort. |
| `Report` | type | `internal/bench/bench.go:111` | Top-level JSON artifact: schema, mode, fixture version, evidence metadata, summary, results, matrix, comprehensive-only fields (nil in legacy). |
| `EvidenceMetadata` | type | `internal/bench/bench.go:27` | Self-describing 'measures / does not measure / how to reproduce' block — core anti-overclaim contract. |
| `DefaultFixtureSet` | func | `internal/bench/bench.go:473` | Versioned fixtures (`2026-05-17.offline.v1`): 17 fixtures (16 search + 1 extract), en/tr/es/de. |
| `ComprehensiveFixtureSet` | func | `internal/bench/bench.go` | Copies the default set, then adds localization/freshness search options and static/JS-heavy/redirect/error/structured extract cases for explicit live comprehensive runs only. Offline fixtures stay unchanged. |
| `RunOffline` / `RunOfflineWithObservations` | func | `internal/bench/bench.go:498` | Deterministic offline eval; iterates fixtures, evaluates against matrix + observation table. |
| `evalOfflineCase` | func | `internal/bench/bench.go:530` | Walks route (TaskGeneral fallback), simulates per-provider attempts, marks first success, scores. |
| `defaultOfflineObservations` | func | `internal/bench/bench.go:646` | Hardcoded per-(provider,task) Observation table — synthetic 'fixture data'. |
| `scoreMetrics` / `latencyScore` / `metricsFromObservation` | func | `internal/bench/bench.go:620` | Offline scoring: averages 9 normalized metrics × 100; latency buckets; Kind-derived defaults. |
| `MarkdownEvidenceSummary` | func | `internal/bench/bench.go:226` | Sanitized offline/live Markdown evidence summary. |
| `sanitizeMarkdownCell` | func | `internal/bench/bench.go:452` | Escapes pipes, redacts Authorization/Bearer/SECRET/TOKEN/api_key, replaces URLs + abs paths. |
| `publicReason` | func | `internal/bench/bench.go:399` | Allowlist mapping known reasons to themselves, else `sanitized_error`. |
| `RunComprehensiveLive` | func | `internal/bench/comprehensive.go:48` | Per-provider goroutines run fixtures serially while providers run concurrently; capability-filters, applies max(global spacing, provider floor), honors cancellation, and flattens alphabetically. |
| `runComprehensiveOne` | func | `internal/bench/comprehensive.go:126` | Single (provider, fixture) probe w/ per-call timeout; records Success/ResultCount/LatencyMS/ErrorClass. |
| `classifyComprehensiveError` | func | `internal/bench/comprehensive.go:173` | Reduces errors to a sanitized vocabulary; `HTTPStatusError` first, then string match; 202 treated as 429. |
| `summarizeMeasurements` | func | `internal/bench/comprehensive.go:222` | Aggregates per-provider: calls/successes/failures, avg/p50/p95 latency (successful only), error histogram. |
| `MarkdownComprehensiveSummary` | func | `internal/bench/comprehensive.go:296` | Sanitized comprehensive Markdown: aggregate + per-(provider,task) tables. |
| `check-benchmark-claims.sh` | file | `scripts/check-benchmark-claims.sh:40` | CI shell guard: requires 4 benchmark docs + mandated disclaimers, greps for unsupported ranking/speed regexes. |
| `cmd/bench/main.py:main` | func | `cmd/bench/main.py:232` | Standalone Python provider-benchmark runner across 12 categories; parallel/legacy to Go harness, not invoked by Go/CI. |

**Data flow.** `nole bench` defaults to the unchanged deterministic `DefaultFixtureSet` + `DefaultRouteMatrix`. `--live` goes through the real service/router/policy. `--live --comprehensive` requires explicit operator intent, bypasses router/policy/ledger, uses `ComprehensiveFixtureSet`, runs each provider serially with per-provider spacing (TinyFish floor: two seconds), then sanitizes and aggregates only summary measurements. Output uses the existing Markdown/JSON sanitizers; CI runs offline mode only.

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
3. **Service entry.** `Service.Search` (`service.go:37`): resolve/default task, normalize limit, validate + canonicalize `SearchOptions` (invalid caller values fail before provider calls).
4. **Route resolution.** `routeFor` (`service.go:536`) delegates to `Router.Route`, which resolves task→route with TaskGeneral fallback and returns a defensive route copy.
5. **Cache.** If `cache != nil` (`WithResponseCache`, `service.go:21`), `cache.GetSearch` keyed via `searchCacheKey` (`cache.go:112`) over task + normalized query + limit + canonical SearchOptions; hit → rebuild Route/RouteTrace via `cacheHitAttempt` (`service.go:275`) + `BuildSearchRoutingInsight` (`insight.go:29`) and return before any quota check; miss → append cache-miss.
6. **Per-provider gate loop.** For each provider name in route: `Router.Candidate` performs the shared lazy registration/capability/quota gates (`not_registered`, `missing_search_capability`, `premium_blocked_free_first`, `free_quota_exhausted`) only when that slot is reached; `provider.Status(ctx)` then skips if unavailable. Each outcome produces a typed `RouteAttempt` (`types.go:69`) via `skippedRouteCandidateAttempt` / `attemptWithDecision` (`service.go:540/571`).
7. **Provider call.** `provider.Search(ctx, req)` (e.g. `brave.go:81`) receives canonical SearchOptions; adapters forward only their documented subset (Brave all five fields, Tavily/Firecrawl country+freshness, DDGS/Wikipedia/arXiv ignore unsupported options), routed through `providerhttp.DoWithRetry` (`retry.go:40`) for network providers; latency via `time.Since`. Error → `provider_error` trace, continue; empty results → `empty_results` trace, continue (free tier NOT debited).
8. **Quota debit (money-safety invariant).** On success ONLY: `ledger.Record(name)` (`quota.go:297`) takes the file lock, re-reads disk, decrements `FreeRemaining` or adds `SpentCents`, persists (`persistLocked` `quota.go:609`). If Record errors, re-Decide and set trace reason `success_<reason>` but still return the response (`service_test.go:185` guards this).
9. **Cache write.** On success `cache.SetSearch` stores a clone (`cloneSearchResponse` `cache.go:145`).
10. **Insight + return.** `BuildSearchRoutingInsight` (`insight.go:29`) → `buildRuntimeRoutingInsight` (`insight.go:117`) reconstructs the winning provider/policy/cache summary from the trace. Response (results + Route + RouteTrace + RoutingInsight + optional SetupTip) returns to the caller.
11. **Output / error rendering.** CLI: `writeJSON` (`app.go:246`) or `writeHumanRoutingInsight`. MCP: `json.MarshalIndent` → `NewToolResultText`, or `toolErrorJSON` (`errors.go:18`) on failure. Exhausted route → `NoFreeQuotaError` (`errors.go:5`) or `lastErr`, both with `BuildErrorRoutingInsight`. All error text passes `safeerr.Message` (`safeerr.go:18`) for redaction before reaching stderr/JSON.

---

## Setup-writer subsystem

`nole setup` (`setup.go:39`) registers Nole as an MCP server across eleven platforms (Claude is instruction-only, so ten write a file). A `configure(name, fn)` helper runs each file-writer, reporting success/failure; a failed requested agent makes the command exit non-zero. Each writer merges only the `nole` entry into the agent's native user-scope config, preserving unknown sibling fields/comments, writing atomically via `atomicWriteFile` (`setup.go:1103`) with a `.bak` backup and `configWriteMode` (`setup.go:1094`) preserving existing permissions. `launchSpec` centralizes bare-binary vs wrapper launch semantics: bare mode emits `command=/absolute/path/to/nole, args=["mcp"]`; wrapper mode emits `command=/absolute/path/to/nole-mcp, args=[]`.

| Platform | Config path family | Serialization | Writer anchor | Notes |
|---|---|---|---|---|
| Claude | Managed by Claude Code CLI (no file written) | n/a (CLI command printed) | `printClaudeInstructions` `internal/cli/setup.go:221` | Instruction-only; prints `claude mcp add nole -s user -- ...`; not counted in configured total. |
| Cursor | Cursor user MCP config | JSON | `writeMCPJSONConfig` `internal/cli/setup.go:813` | Shared with Windsurf/Gemini; preserves unknown root fields and sibling servers, replaces only `nole`. |
| Windsurf | Windsurf user MCP config | JSON | `writeMCPJSONConfig` `internal/cli/setup.go:813` | Same shared JSON writer as Cursor. |
| Codex | Codex TOML config | TOML | `writeCodexConfigPath` `internal/cli/setup.go:847` | Hand-rolled `[mcp_servers.nole]` table via `upsertCodexTomlTable`/`codexMCPServerBlock`; embeds `/bin/sh -lc 'set -a; . .env; exec nole mcp'` unless wrapper set. |
| OpenCode | OpenCode JSON config (`mcp` key) | JSON | `writeOpenCodeConfigPath` `internal/cli/setup.go:974` | `{type:local, command:[bin,mcp], enabled, environment}` shape (`openCodeEntry`). |
| Kimi | Kimi JSON config (`mcpServers` key) | JSON | `writeKimiConfigPath` `internal/cli/setup.go:1047` | `{command}` in wrapper mode or `{command,args}` bare (`kimiEntryRaw`), matching `kimi mcp add` output. |
| Hermes | Hermes YAML config (`mcp_servers.nole`) | YAML (`yaml.Node` tree) | `writeHermesConfigPath` `internal/cli/setup.go:270` | Comment-preserving via `yamlMappingUpsert`; `upsertHermesNoleServer` sets command/args + default timeout=120/connect_timeout=60 + tools policy. |
| Antigravity | `~/.gemini/config/mcp_config.json` (`mcpServers` object keyed by name) | JSON | `writeAntigravityConfig` `internal/cli/setup.go:445` | `--antigravity`; writes a local stdio entry, updating only `command`/`args` while preserving existing Nólë policy/options, sibling servers, and remote `serverUrl` entries. Status `repo-tested`; see `docs/CLIENTS/antigravity.md`. |
| Gemini | `~/.gemini/settings.json` (`mcpServers` object keyed by name) | JSON | `writeGeminiConfig` `internal/cli/setup.go:513` | `--gemini`; preserved for Gemini CLI Standard/Enterprise/Cloud/paid API-key users. Delegates to `writeMCPJSONConfig` (same shape as Cursor). Status `repo-tested`; see `docs/CLIENTS/gemini.md`. |
| Grok (superagent) | `~/.grok/user-settings.json` (`mcp.servers` array keyed by `id`) | JSON | `writeGrokConfig` `internal/cli/setup.go:534` | `--grok`; targets `superagent-ai/grok-cli`. Array upsert-by-`id` via `upsertGrokNoleServer`, preserving other servers + unknown fields. Status `repo-tested`; see `docs/CLIENTS/grok.md`. |
| Grok Build (xAI) | `~/.grok/config.toml` (`[mcp_servers.nole]`) | TOML | `writeGrokBuildConfig` `internal/cli/setup.go:663` | `--grok-build`; targets xAI's Grok Build TUI (a different product from the superagent row). Reuses `upsertCodexTomlTable` via `grokBuildMCPServerBlock`; preserves a user-set `enabled=false`; refuses to overwrite a customized entry (sub-table / extra key). Status `verified`; see `docs/CLIENTS/grok.md`. |

> **Map maintenance note.** This is a point-in-time Phase-1 map. Per-symbol file:line anchors drift as code changes (re-verify before relying on them — see the note at the top of this doc). The setup-writer roster above was last reconciled when `--antigravity` was added alongside the existing Gemini CLI writer, and `--gemini` remained scoped to Gemini CLI Standard/Enterprise/Cloud/paid API-key users. See `docs/CLIENTS/` and the CHANGELOG for current client status.

Optional `--local-extract` (`setupLocalExtract` `setup_local_extract.go:26`) provisions an isolated Python venv, installs `scrapling[fetchers]`, persists `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env`, and emits a 0700 POSIX `/bin/sh` env-sourcing wrapper (`writeMCPWrapper` `setup_local_extract.go:195`) so MCP clients that do not inherit shell env still find provider keys.

---

## Dependency graph

### Internal package import edges

- `main` → `internal/cli`, `internal/safeerr`
- `internal/cli` (app/composition root) → `internal/core`, `internal/providers/{arxiv,brave,ddgs,firecrawl,httpfetch,mock,scrapling,tavily,wikipedia}`, `internal/safeerr`
- `internal/cli` (mcp/serve/http) → `internal/mcpserver`, `internal/core`, `internal/safeerr`
- `internal/cli` (bench) → `internal/bench`, `internal/core`, `internal/providers/{arxiv,brave,ddgs,firecrawl,httpfetch,scrapling,tavily,wikipedia}`, `internal/safeerr`
- `internal/mcpserver` → `internal/core`, `internal/version`, `internal/safeerr`, `internal/cli` (HTTP wiring sets `EphemeralCtxKey`; `defaultService`)
- `internal/core` (service) → `internal/safenet`, intra-package (registry, quota, router, planner, cache, insight, byok_metadata, setup_hints, types, errors)
- `internal/core` → consumes `internal/providers/providerhttp` indirectly (via `safeerr` / error classification) and the `core.Provider` interface implemented by all provider packages
- `internal/providers/{arxiv,brave,ddgs,firecrawl,httpfetch,tavily,tinyfish,wikipedia}` → `internal/core`, `internal/providers/providerhttp` (arxiv/httpfetch also use `internal/version`; httpfetch and tinyfish use `internal/safenet` for URL safety)
- `internal/providers/scrapling` → `internal/core` (no HTTP; shells to Python)
- `internal/providers/mock` → `internal/core`
- `internal/bench` → `internal/core`, `internal/providers/providerhttp` (error classification), `internal/safeerr` (provider instances are injected by `internal/cli/bench.go`, not imported here)
- `internal/safeerr` → `internal/providers/providerhttp` (special-cases `HTTPStatusError`)
- `internal/safenet` → stdlib only (`net`, `net/url`)
- `internal/version` → stdlib only (build-stamped vars; consumed by `internal/mcpserver` and the `version` CLI command)

### External modules (from `go.mod`)

| Module | Version | Role |
|---|---|---|
| `github.com/mark3labs/mcp-go` | v0.43.1 | MCP server library: `NewMCPServer`, `AddTool`, `ServeStdio`, in-process sessions, `HandleMessage`, JSON-RPC tool result/error types. Backbone of `internal/mcpserver` + the HTTP MCP bridge. |
| `github.com/spf13/cobra` | v1.10.2 | CLI command framework for the root command and all 14 subcommands. |
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

Build toolchain: `go 1.25.12`. CI also invokes `golang.org/x/vuln/cmd/govulncheck` (pinned in `ci.yml`/`release.yml`), the `gh` CLI, and `python3` (for `secret-scan.sh` and the orphaned `cmd/bench/main.py`).

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

> **Note:** This is the Phase-1 snapshot. Several of these seeds were verified and
> implemented in this same release — including the `version` command (consuming
> `Commit`/`Date`), transport-error/408 retry, rune-safe truncation, the DDGS
> snippet-alignment fix, the bounded cache, and the future-dated `PeriodStart`
> self-heal. See `docs/RESEARCH-FINDINGS.md` (verified vs. proposed) and the
> CHANGELOG for what shipped vs. what remains proposed.

| Title | Area | Anchor | Confidence | Rationale |
|---|---|---|---|---|
| Redundant/unreachable error branch in defaultQuotaLedger | quality | `internal/cli/app.go:120` | high | `if err != nil && ledger != nil { return ledger }` (120-122) is identical to the following `if ledger != nil` (123-125); `NewFileQuotaLedgerWithPolicy` always returns non-nil, so the memory-fallback at 126-130 is unreachable for a non-empty path. |
| defaultService() reconstructs the whole Service on every call | latency | `internal/cli/app.go:24` | medium | `runSearch` and other subcommands rebuild registry, re-open the file-backed ledger (disk read + lock), and allocate a fresh cache per invocation. Acceptable for per-turn MCP spawn; any in-process multi-call path pays full reconstruction + cold cache. No memoization. |
| All registry.Register errors are silently discarded | stability | `internal/cli/app.go:38` | medium | Every `registry.Register(...)` discards its error with `_ =` (38,40,45,47,52,54,58,61). A duplicate/invariant violation is invisible at startup, surfacing only as a missing provider at routing time with no breadcrumb. |
| Router.Select dead-production-path drift | quality | `internal/core/router.go` | RESOLVED (v1.7.0) | `Router.Route` now owns route fallback, `Router.Candidate` owns single-slot registration/capability/quota gates, `Service.Search`/`Extract` consume it lazily, and `Router.Select` is a wrapper over the same candidate path. |
| Planner has no rules for TaskSemantic or TaskExtract | coverage | `internal/core/planner.go:71` | high | `plannerRules` (71-99) and `taskPriority` (262) omit TaskSemantic/TaskExtract though both exist in the taxonomy (`types.go:13,18`). Semantic/extract intents can never classify; they fall through to general and tie-break last at default 100. |
| taskLabel returns raw enum string for tasks without a planner rule | docs | `internal/core/planner.go:249` | medium | `taskLabel` only finds friendly labels for rule-backed tasks + general (255); TaskSemantic/TaskExtract fall to `return string(task)` (258), so `TaskOverride=semantic` produces uncurated label 'semantic'. |
| Router.Select drops the ledger's denial reason | quality | `internal/core/router.go` | RESOLVED (v1.7.0) | `RouteCandidate` carries the `QuotaDecision`, `DecisionChecked`, and skip reason; Service converts those into rich `RouteAttempt` rows while Select remains a thin provider picker. |
| hasTopScoreTie flags ambiguity on any tie of top two scores | correctness | `internal/core/planner.go:227` | medium | Compares only `intents[0].Score == intents[1].Score` (231); two genuinely distinct strong intents (docs=4, news=4) mark the whole classification Ambiguous, conflating 'no clear single task' with 'multiple clear tasks'. |
| markUnavailableLocked ignores its err argument | quality | `internal/core/quota.go:645` | high | Takes an `err error` param but never uses it; warning is a fixed string. Callers pass real I/O errors (e.g. `persistLocked` failure at `quota.go:349`) that are silently dropped. Log/wrap it or drop the param. |
| Windows file lock is non-blocking, unlike the Unix LOCK_EX path | correctness | `internal/core/file_lock_windows.go:21` | medium | `LockFileEx` called with `LOCKFILE_EXCLUSIVE_LOCK` only (no FAIL_IMMEDIATELY), 1-byte region, no retry loop, vs blocking `flock(LOCK_EX)` (`file_lock_unix.go:11`). If it returns without truly blocking under contention, the multi-process race guard weakens on Windows. No Windows contention test. |
| PeriodStart >= now refresh predicate skips refresh on backward clock | correctness | `internal/core/quota.go:598` | medium | `refreshExpiredEntriesLocked` only refills when `PeriodStart < now`. A future-dated PeriodStart (clock skew, ledger copied from another host) never refreshes; provider stays exhausted indefinitely. No guard/warning for `PeriodStart > now`. |
| Corrupt-recovery/reload errors surfaced only via in-memory state | stability | `internal/core/quota.go:449` | medium | `reloadFromDiskLocked` returns `recoverCorruptLedgerLocked`'s (often nil) result, so the constructor returns `(ledger, nil)` on a corrupt ledger; only signal is `BudgetStatus().LedgerWarning`. Callers checking only `err` treat a fail-closed corrupt ledger as a clean start. |
| Cache has no size bound / eviction beyond lazy TTL expiry | stability | `internal/core/cache.go:22` | medium | search/extract maps grow unbounded; entries deleted only lazily on a Get that finds them expired (67, 95). A long-lived MCP server with many distinct never-repeated queries accumulates entries. No background sweep / max-entries cap. |
| refreshExpiredEntriesLocked from Decide() mutates in-memory state without persisting | correctness | `internal/core/quota.go:188` | low | `Decide()` discards the `changed` return and never persists (intentional, 184-186). A second process Deciding the same provider re-reads stale disk and may see the pre-refresh value; two processes can transiently disagree on FreeRemaining across a month boundary. |
| routeFor duplicated Router route-resolution logic | quality | `internal/core/service.go` | RESOLVED (v1.7.0) | `Service.routeFor` now delegates to `Router.Route`; runtime route gates flow through lazy `Router.Candidate`, so Service no longer reaches into `s.router.matrix` directly. |
| Search and Extract pipelines still share substantial shape | quality | `internal/core/service.go` | medium | The static route gates are now shared through `Router.Candidate`, but Search and Extract still duplicate provider call/timing/Record/trace/cache flow; future fixes to that runtime loop still need care on both paths. |
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
| Advertised MCP server version defaults to "dev" | docs | `internal/mcpserver/server.go:10` | RESOLVED (v0.3.0) | `NewMCPServer` reports `version.Version`, the literal "dev" (`version.go:4`) unless ldflag-overridden. Resolved in v0.3.0: the `nole version` command surfaces the stamped fields, and `scripts/check-release-builds.sh` (invoked by the tag-triggered `release.yml`) injects `-X .../version.Version` (plus `Commit`/`Date`) into release builds, so tagged releases no longer advertise 'dev'. A development build still reports 'dev'/'unknown' by design. |
| toolErrorJSON marshal-failure fallback concatenates operation unescaped | correctness | `internal/mcpserver/errors.go:28` | low | The fallback string-concatenates `operation` into a JSON literal. `operation` is always a hardcoded literal at the only call sites (`tools.go:113,199`), so currently safe; but the signature accepts arbitrary strings, so a future caller could emit invalid/injected JSON. |
| 408 classified 'transient' by statusCategory but NOT retried by isTransientStatus | correctness | `internal/providers/providerhttp/retry.go:121` | medium | `errors.go:42` maps 408/429/502/503/504 to 'transient', but `isTransientStatus` (122-127) only retries 429/502/503/504. A 408 surfaces as 'transient' yet was never retried — category label and retry policy disagree. |
| DoWithRetry sets lastErr on transport error then returns, making fallthrough dead | quality | `internal/providers/providerhttp/retry.go:52` | medium | On `client.Do` error the loop does `lastErr = err; return nil, err` (52-55), so transport errors are never retried and the post-loop `if lastErr != nil` (66) is only reachable on normal exit. Transport-level transient failures (reset, DNS blip) get zero retries despite MaxAttempts>1. |
| brave.go helper named clampMin actually computes max(a,b) | quality | `internal/providers/brave/brave.go:228` | RESOLVED | Resolved before this snapshot: the helper is now `clampRange`, constraining Brave `count` to the selected endpoint's documented range. |
| ddgs result parsing relies on positional snippet alignment | correctness | `internal/providers/ddgs/ddgs.go:100` | medium | `linkMatches` and `snippetMatches` are gathered independently (94-95) then zipped by a shared `snippetIdx` that only advances inside the link loop (110-113). When an ad redirect is `continue`d (105-107) before consuming a snippet, subsequent results can pair with the wrong snippet. Also brittle to DDG HTML class changes. |
| Firecrawl explicit `success:false` handling | correctness | `internal/providers/firecrawl/firecrawl.go:75` | RESOLVED | Resolved in v1.7.0: Firecrawl search, Research Index paper search, and scrape response structs model `success` as `*bool`, treating explicit `success:false` as a sanitized provider error while preserving compatibility for omitted `success` fields. |
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
