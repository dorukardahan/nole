# Product framing

Nólë is a free, local web search router for AI agents and coding CLI tools.

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

Default policy: free-first and BYOK-safe.

This means:

- no hidden paid spend by default;
- provider keys are user-owned;
- Nólë reports key presence, never key values;
- free quota exhaustion should fail closed when policy requires no paid usage;
- premium-capable providers can participate only according to explicit policy.

Future policies can include:

- `free-first`: prefer free/keyless/free-tier routes when they meet quality needs.
- `cost-capped`: respect local daily/monthly provider budgets.
- `quality-first`: allow premium-capable providers when evidence says they are materially better for the intent.

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

- `Nólë: docs search via Brave, extracted with Jina, 1 fallback.`
- `Nólë: docs+pricing search via Brave/Tavily under free-first policy.`

Detailed `route_trace` should remain in JSON/debug surfaces, not dumped into normal chat unless the user is debugging.

## Benchmark/evidence language

Be precise:

- Deterministic offline harness validates routing/fallback contracts. It does not measure live web quality.
- Optional live benchmark summaries compare configured providers on a fixture set and must be sanitized.
- Route matrix changes require evidence. Do not change routes just because a provider is popular or paid.

## v0.1 private-prep definition

v0.1 private-prep is ready when:

- README tells the right story in the first screen.
- AGENTS.md lets an agent install Nólë from scratch.
- Priority client docs exist with verified/unverified labels.
- CI gates are present and do not require secrets.
- Core commands pass locally and in CI.
- Benchmark docs are honest.
- Provider key docs explain BYOK and overage cautions.
- Repo remains local/BYOK-first and safe to inspect publicly.

Public release, repo visibility changes, hosted deployment and any paid provider usage still require explicit user approval.
