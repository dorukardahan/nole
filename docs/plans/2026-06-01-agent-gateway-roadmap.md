# Agent Gateway Roadmap (v0.5.0 → v0.7.0)

**Date:** 2026-06-01
**Status:** Draft for owner review
**Theme:** Make the router actually route, return clean evidence, and stay a
dumb-but-excellent pipe for frontier agents.

---

## North star (read this first)

Nólë is the **internet gateway for frontier AI agents and personal assistants**
(Claude Code, Codex, Hermes, OpenClaw, OpenCode, Cursor, …). Its job is to
*strengthen those tools' web-search capability* — not to be smart itself.

> Nólë is not an AI. It does not think, synthesize, or answer. The agent thinks;
> Nólë hands the agent the cleanest possible raw material from the web, routed
> well, cheaply, and honestly.

Every change in this roadmap is judged by a single question:

> **Does this make Claude Code / Codex / Hermes / OpenClaw's web search
> measurably better, cheaper, fresher, or more citable?**

If a feature makes *Nólë* cleverer but does not make the *agent's* search better,
it does not belong here.

## Why this roadmap exists

A purpose-anchored audit (2026-06-01) found that Nólë's hardening pillar
(SSRF, circuit-breaker, quota durability, fail-closed cost control) is genuinely
strong and is the product's real trust anchor. But the pillar that justifies the
word **router** is largely unwired on the hot path:

1. **The deterministic planner never fires on real searches.** `ClassifyQuery` /
   `BuildRoutePlan` are called only from the `classify` / `route-plan` inspection
   commands and the plan-insight builder. Real searches go through
   `Service.Search`, which takes `req.Task` verbatim and defaults empty to
   `TaskGeneral`. The MCP `search` tool defaults `task` to a free-text
   `"general"`. So unless the calling agent supplies the exact task string,
   **every query routes through the generic `brave→tavily→firecrawl→ddgs`
   matrix** and the task-tuned matrices never fire.
2. **"Route by evidence" is aspirational at runtime.** Selection is positional
   first-allowed-provider over a frozen matrix. The one free quality signal we
   already receive — Tavily's per-result relevance `Score` — is parsed and
   discarded; `SearchResult` carries no score, rank, or date. There is no
   cross-provider dedup or ordering.
3. **Task type changes the route but not the request or the output.** A `news`
   query and a `general` query return structurally identical objects: no
   freshness/time-range parameter is sent to providers, and results carry no
   publication date — yet agents are told to cite result URLs.
4. **The richest output is unreachable by agents.** `research` (multi-step
   search→extract→dedup) exists only as a human CLI command; it is not exposed
   over MCP or REST, and it ships a string-concatenation "summary" that invites
   the exact Perplexity comparison the positioning tries to avoid.
5. **The local quota counter can fail in the dangerous direction.** A hardcoded
   `1000/month` is applied identically to all three BYOK providers, but they meter
   differently (Tavily is credit-based, Firecrawl was a one-time grant, Brave is
   ~2000/month with a rate cap). Under-counting real consumption keeps allowing
   calls past the real free tier → surprise spend.
6. **Production observability is near-zero.** `route_trace` is ephemeral, the
   circuit-breaker's `IsOpen()` is wired to nothing (a tripped provider still
   reports "available"), `/health` always returns 200, and there is no
   effective-config dump.

## Design principles

- **Gateway, not brain.** Deterministic, reproducible behavior. No LLM, no ML,
  no self-tuning. The planner is a fast keyword router, not "intelligence."
- **Evidence, not answers.** Nólë returns sources + extracts + the signals it
  was given (relevance, date). It never composes the answer.
- **Pass signals through; let the agent judge.** When a provider gives a
  relevance score or a date, surface it — do not have Nólë invent its own
  cleverness on top.
- **Agent-first DX.** The MCP/REST surface is the product. Default output is
  compact; the common workflow is one call; tokens are not wasted on debug blobs.
- **Honest cost.** Never imply a hard cap we cannot enforce. Label estimates as
  estimates. Fail safe, and say when we might be wrong.
- **Solo-maintainable.** Effort flows to the differentiating core, not to
  fragile breadth.

## Non-goals (explicit)

These are deliberately rejected so scope stays disciplined and on-mission:

- **No adaptive / learning / self-improving router.** Routing stays a frozen,
  evidence-derived prior plus deterministic signal-based ordering. (Rejected:
  the persistent success/latency learning loop.)
- **No LLM or synthesis inside Nólë.** No answer generation. The `research`
  string-concat summary is removed, not improved.
- **No becoming a research assistant / Perplexity / hosted SaaS / agent
  replacement / provider marketplace.** Unchanged from existing positioning.
- **No re-ordering the provider matrix without sanitized evidence.** Unchanged.

---

## v0.5.0 — Route correctly + pass quality signals through

*"The pipe does its job."* This is the single most important release: it makes
the **router** claim true and makes results more useful to the agent — without
adding any intelligence to Nólë.

| # | Change | Why (north-star) | Touches |
|---|--------|------------------|---------|
| 1 | **Wire the planner into the live path.** In `Service.Search`, when `req.Task` is empty, call `ClassifyQuery` and use `PrimaryTask`; keep explicit `--task` / `task` arg as an override. Record the *detected* task in `route_trace` / `routing_insight`. | The agent gets task-fit routing by default instead of always hitting the generic matrix. This is deterministic routing, not "smartness." | `internal/core/service.go`, `internal/cli/search.go`, `internal/mcpserver/tools.go`, `internal/cli/http.go` |
| 2 | **Stop discarding provider quality signals.** Add optional `Score`/`Rank` and `PublishedAt`/`Date` fields to `core.SearchResult`; populate from each adapter where available (Tavily `Score`, provider dates). Order results deterministically by the provider's own signal (relevance, then recency where relevant). | The agent can rank and **cite results by date**; we pass the signal through so the agent judges — Nólë does not invent a score. | `internal/core/types.go`, `internal/providers/*/*.go` |
| 3 | **Task-aware request shaping.** Map task → provider request parameters: send a freshness/time-range for `news`/`factcheck`, scholarly filters for `academic`, where the provider supports it. | A `news` query actually returns *fresher* results to the agent — task-fit finally delivers value, not just a different provider order. | `internal/providers/{brave,tavily,firecrawl}/*.go`, route plumbing |
| 4 | **Fix the planner taxonomy.** Add `plannerRules` for `TaskSemantic` and `TaskExtract` (currently advertised but unclassifiable), or drop them from the advertised list. Make `researchPipeline` classify the question to drive its fan-out instead of a hardcoded `[General, Research, Docs]`. | Removes the gap between advertised intents and what can actually be detected; keeps the taxonomy honest. | `internal/core/planner.go`, `internal/cli/research.go` |

**Risk:** Item 1 changes the default route for queries that previously fell to
`general`. Mitigation: the explicit override always wins, the detected task is
surfaced in the trace (visible, debuggable), and the change is covered by tests
asserting classification → route. Item 2 changes the `SearchResult` shape
(additive, optional fields → backward-compatible JSON).

## v0.6.0 — Cheap, safe, and easy for agents

*"The pipe is pleasant and honest to use."* Low-to-medium effort, low risk.

| # | Change | Why (north-star) | Touches |
|---|--------|------------------|---------|
| 5 | **Make `route_trace` opt-in on MCP/REST.** Default response = `routing_insight` + results; gate the full per-attempt trace behind `include_trace: true` (or a `NOLE_MCP_INSIGHT` env), mirroring CLI `--insight off`. | The debug blob is forced into every agent turn's context today — pure token waste on the primary surface. Compact-by-default respects the agent's context budget. | `internal/mcpserver/tools.go`, `internal/cli/http.go` |
| 6 | **Add a `search_and_extract` primitive** (MCP tool + REST route): one call that searches, then extracts the top result(s). | The dominant agent workflow (search → read the top hit) is two round-trips today (double routing, double quota gate, double context dump). One call halves latency and quota pressure. | `internal/mcpserver/tools.go`, `internal/core/service.go`, `internal/cli/http.go` |
| 7 | **Expose `research` as structured evidence and DROP `synthesizeSummary`.** Add a `research` MCP tool + `/api/research` route returning `{sources, extracts, providers_used}` — no composed "answer." | Unlocks Nólë's richest output to the agents it serves, while honoring "evidence, not answers." Nólë stops being a degraded answer-writer and becomes a strong evidence pipe. | `internal/cli/research.go`, `internal/mcpserver/tools.go`, `internal/cli/http.go` |
| 8 | **Make the quota model honest.** Per-provider-honest free quota + refresh window (credit-weight Tavily debits, model Firecrawl's grant, Brave's true ~2000 + rate cap); add a drift/uncertainty note to `budget_status` / `doctor` ("local estimate, may diverge from provider dashboard"); ship usable `cost-capped` defaults (or have the failure reason name the env var to set). | "Keep control of cost / no hidden paid spend" is the real trust anchor — a counter that under-counts real spend gives false confidence and risks a surprise bill. Honesty protects the user's money. | `internal/core/byok_metadata.go`, `internal/core/quota.go`, `internal/cli/doctor.go` |

## v0.7.0 — Observable and easy to adopt

*"The pipe is easy to run and trust in production."*

| # | Change | Why (north-star) | Touches |
|---|--------|------------------|---------|
| 9 | **Observability.** Opt-in redaction-safe structured log (`NOLE_LOG=json` to stderr, one line per search/extract reusing the leak-proof insight fields); fold breaker state into `core.ProviderStatus` via the existing `IsOpen()` peek (so a tripped provider stops reporting "available"); make `/health` (or `/ready`) reflect real readiness; add `nole config` to dump effective `NOLE_*` values and their source. | "Return enough routing context to explain what happened" must survive past the live response; the v0.4.0 hardening (esp. the breaker) only pays off if the operator can see it act. | `internal/core/{service,registry,quota}.go`, `internal/cli/{http,doctor,config}.go` |
| 9b | **Agent-facing usage stats (owner's idea, 2026-06-01).** A persistent, AGGREGATE, redaction-safe usage tally per `(provider, task)` — call/success/failure counts, p50 latency, result counts, quota burn, cache hit — exposed via an MCP tool/resource + `nole stats`. The point is a **feedback surface for the agent (and human), not learning by Nólë**: the agent reads it and adapts its OWN usage ("Firecrawl has been flaky for me, skip it"). | Turns observability from operator-logs into a mirror the smart agent can use — literally "strengthen the agent." Distinct from the rejected learning router: Nólë only RECORDS mechanical facts (never quality), never reorders its own routes from it; intelligence stays with the agent. Aggregate-only (not per-query) for privacy. Build the simple version, after the core works. | `internal/core/` (ledger-style persistence pattern), `internal/mcpserver/tools.go`, `internal/cli/stats.go` |
| 10 | **Onboarding.** Ship a prebuilt binary / install script (`go install` or `curl|sh`); rewrite the `setup` completion message to lead with "you can start now with zero keys (DDGS search + local extract are free)"; add a `doctor` staleness check (configured client command resolves to the current binary). | Free-first, BYOK-optional is the adoption hook for the target audience; an 8-step build-from-source path with a keys-first completion message is a high activation tax that makes devs think they're blocked without keys. | `internal/cli/{setup,doctor}.go`, packaging, README |
| 11 | **(Optional) Triage the client writers.** Keep the 4–5 priority clients as bespoke writers; demote the rest to the generic MCP template + docs to shrink the high-churn serializer surface. | ~1225 LOC of fragile per-client serializers vs ~333 LOC of routing brain is an inverted effort allocation for a solo maintainer; trim churn, reinvest in the core. | `internal/cli/setup*.go`, `docs/CLIENTS/*` |

## Sequencing rationale

Make the center work before polishing the shell. v0.5.0 makes Nólë actually
*route* and return better raw material — the reason to use it over calling Brave
directly. v0.6.0 makes that pipe cheap, honest, and pleasant for the agent
audience (and protects the user's money). v0.7.0 makes it observable and easy to
adopt. Each release is independently shippable and marketable, matching the
project's existing focused-release discipline (v0.3.x → v0.4.0).

## Open questions for the owner

1. **v0.5.0 item 3 (freshness params):** acceptable to send provider-specific
   time-range parameters (Brave `freshness`, Tavily `days`, etc.), or keep the
   request identical across providers for reproducibility? (Recommendation: send
   them — it is the whole point of task-fit, and it is deterministic per task.)
2. **v0.6.0 item 7 (research over MCP):** expose `research` as an agent tool, or
   keep it CLI-only and just drop the summary? (Recommendation: expose it — the
   agent audience is exactly who should get multi-source evidence.)
3. **v0.7.0 item 11 (client triage):** which clients are "priority" enough to
   keep as bespoke writers? (Recommendation: Claude Code, Codex, Hermes,
   OpenClaw, Cursor.)

## Success criteria

- A search with no explicit task is classified and routed via the task-tuned
  matrix; the detected task is visible in the trace. *(v0.5.0)*
- `SearchResult` carries provider relevance/date where available; a `news` query
  returns results ordered by recency with dates the agent can cite. *(v0.5.0)*
- The default MCP search response contains results + a compact insight and **no**
  full trace unless requested. *(v0.6.0)*
- An agent can search-then-read in one call, and can fetch multi-source evidence
  (no composed answer) over MCP. *(v0.6.0)*
- `budget_status` states the local count is an estimate and is honest per
  provider; `cost-capped` authorizes bounded spend out of the box or tells the
  user exactly what to set. *(v0.6.0)*
- A tripped breaker is visible in `provider_status`; `/health` can go non-200;
  `nole config` dumps effective settings. *(v0.7.0)*
- The quickstart path from zero to a working agent tool is one documented
  command, and the keyless-free path is stated up front. *(v0.7.0)*

## Firsthand dogfood validation (2026-06-01)

The findings here were not just code-read — Nólë was run live on real providers.
Every major item was confirmed: the owner's own example, `search "concerts this
week"`, classified as `general/score:0`; `--task news` returned byte-identical,
**undated** results (no freshness benefit); Tavily's relevance `Score` is
dropped; the MCP `search` `task` param is free-text with no enum; `research`
ships a `summary` that concatenates page extracts *including nav cruft* ("Our
Repos / Documentation / Careers / Skip to content"). The dogfood confirmed the
code analysis was accurate, not misleading.

## Status & what happens next

The owner (Doruk) reviewed this direction, sharpened the north star ("Nólë is a
dumb gateway, not an AI; it never judges quality"), and **delegated autonomous,
quality-first execution**. This roadmap spans three releases — too large for one
plan — so it is executed one release at a time.

- **v0.5.0 detailed spec: written** → `docs/plans/2026-06-01-v0.5.0-wire-routing-spec.md`.
- Next: grounding (code-anchored briefs + adversarial design review) → feature
  branch → main-loop implementation with tests → adversarial review → full gate
  → release. v0.6.0 and v0.7.0 each get their own spec → plan cycle later.
