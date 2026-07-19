# Product framing

Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools.

It gives Claude Code, Codex, OpenClaw, Hermes, OpenCode and other AI tools a local search/extract layer backed by multiple free or BYOK providers. Nólë detects the web-search intent behind a request, chooses providers according to task fit, benchmark evidence, availability, quota/cache state and user cost policy, falls back safely, and returns a compact routing insight.

Use your own keys. Run it on your own machine or VPS. Keep your existing agent. Make its web search better.

## Category

Preferred category labels:

- Agent web-search router.
- Free/local web search router.
- BYOK provider router.
- Task/multi-intent routing layer.
- Benchmark/evidence-informed search/extract substrate.
- Curl for agent web search.

Avoid framing Nólë primarily as:

- MCP server.
- Search aggregator.
- Hosted search SaaS.
- Research assistant replacement.
- Perplexity clone.
- Provider marketplace.

MCP is important because many agents speak MCP, but Nólë should remain useful through CLI commands and future integrations too.

## Target users

Primary users are people who already use AI agents or coding CLI tools and want better internet context without switching agents.

Priority v0.1 tools:

1. Claude Code.
2. Codex CLI.
3. OpenClaw.
4. Hermes Agent.
5. OpenCode.

Secondary/generic docs should cover Cursor CLI, Kimi and any MCP-compatible client where a generic template is enough.

## Job to be done

When an agent needs current information, docs, citations, pricing, code examples, social/community discussion or extraction, Nólë provides a local routing layer that:

- turns the agent's request into one or more web-search intents;
- selects routes without requiring the user to think about provider/task details;
- keeps free-tier/BYOK-safe behavior by default;
- supports premium-capable providers when policy allows;
- explains the route compactly;
- preserves detailed `route_trace` for debugging.

Nólë should strengthen the agent's own research workflow, not replace it. The agent still decides what to ask, how to synthesize and what to tell the user.

## Routing philosophy

Provider choice should be based on:

1. Task or intent fit.
2. Benchmark/evidence quality.
3. Provider availability.
4. Quota/cache state.
5. User cost policy.
6. Failure history.

Nólë must not pick a paid provider merely because a paid key exists. It should also not trap users in free-only mode forever; premium provider accounts can be used when the user's policy permits and evidence supports it.

## Cost philosophy

Default policy: `free-first` and no-hidden-paid-spend.

This means:

- no hidden paid spend by default;
- provider keys are user-owned;
- Nólë reports key presence/status, never key values;
- a key alone classifies a provider as `premium-capable`, not automatically allowed;
- free quota exhaustion should fail closed when policy requires no paid usage;
- premium-capable providers can participate only according to explicit policy.

Implemented v0.1 policy modes:

- `free-first`: default; allow keyless/free-tier routes and block `premium-capable` routes.
- `cost-capped`: allow premium-capable providers only when a local hard cap, persisted ledger state when configured and explicit per-provider estimated cost keep the call inside cap.
- `quality-first`: explicitly allow premium-capable providers when evidence says they are materially better for the intent and the user accepts provider-account cost risk.

Provider cost classes are `keyless-free`, `free-tier-BYOK`, `premium-capable`, `unknown-cost` and `disabled-no-key`. These appear in safe status surfaces and route traces without secrets or raw provider payloads.

## Multi-intent direction

A single user request can need multiple web-search intents. Example:

"Which model should I use for this job?"

Potential intents:

- docs/provider pages for model capabilities;
- academic papers for methods;
- pricing pages for cost;
- news/benchmarks for freshness;
- code/community discussions for real-world usage.

The planner should start LLM-free and deterministic: keyword/regex/phrase signals, multi-label scoring, user override, maximum selected intents and a compact explanation such as `matched docs + pricing + academic signals`.

## Output UX

When Nólë is used by an agent, user-facing output should include at most one compact insight line when useful, for example:

- `Nólë: search docs via ddgs (1/1 attempts, 3 results)`
- `Nólë: extract page via firecrawl (1/1 attempts, content extracted)`
- `Nólë: route-plan planned docs via brave, pricing via tavily (2 intents, 4 provider slots)`

Detailed `route_trace` should remain in JSON/debug surfaces, not dumped into normal chat unless the user is debugging. `routing_insight` is the compact user-facing field; `--insight off` suppresses it and `--insight verbose` may print route trace lines for troubleshooting. Insights must remain deterministic and sanitized: no API keys, auth headers, raw provider payloads or private URLs.

## Benchmark/evidence language

Be precise:

- Deterministic offline harness validates routing/fallback contracts. It does not measure live web quality.
- `nole bench --evidence-md` writes a public-safe Markdown summary with methodology, reproduction steps, raw artifact policy and explicit limitations.
- Optional live benchmark summaries record configured-provider observations on a fixture set and must be sanitized.
- Route matrix changes require evidence. Do not change routes just because a provider is popular or paid.

## v0.1 release-prep definition

v0.1 release prep is ready when:

- README tells the right story in the first screen.
- AGENTS.md lets an agent install Nólë from scratch.
- Priority client docs exist with verified/unverified labels.
- CI gates are present and do not require secrets.
- Core commands pass locally and in CI.
- Benchmark docs are honest.
- Provider key docs explain BYOK and overage cautions.
- Repo remains local/BYOK-first and safe to inspect publicly.

Release tag creation, hosted deployment, package publication and any paid
provider usage still require explicit user approval.
