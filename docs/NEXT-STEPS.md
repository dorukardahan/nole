# Next steps toward v0.1 private-prep

Nólë is now positioned as a free, local web search router for AI agents and coding CLI tools. The goal is not to build a hosted search SaaS or replace an agent's research workflow. The goal is to improve the internet/search/extract layer used by Claude Code, Codex, OpenClaw, Hermes, OpenCode and similar tools.

## Current baseline

Completed technical MVP hardening:

- task-based route matrix;
- safe provider error envelopes;
- `route_trace` in CLI/MCP surfaces;
- `doctor --mcp` subprocess smoke;
- deterministic benchmark harness;
- private-prep CI/release gates;
- LLM-free `classify` and `route-plan` commands;
- compact `routing_insight` output for search/extract/classify/route-plan and CLI/MCP error envelopes;
- config merge/backup safety for supported setup writers;
- provider error redaction;
- core checks passing on main after PR #1.

Remaining product gap: v0.1 private-prep needs clearer docs, CI gates, agent install experience and truthfully labeled client support.

## Roadmap order

### 1. Product framing and docs

Status: in progress.

Goals:

- README first screen says: `Free web search router for AI agents and coding CLI tools`.
- MCP is described as an entrypoint, not the product category.
- AGENTS.md and install docs let an AI agent clone, build, install, configure and verify Nólë.
- Provider key docs explain BYOK, free-tier and overage cautions.
- Client docs exist for Claude Code, Codex, OpenCode, OpenClaw, Hermes, Cursor/Kimi/generic MCP paths.
- Benchmark docs distinguish deterministic offline harness from optional live summaries.

### 2. CI and private-prep release gates

Goals:

- GitHub Actions without secrets.
- Run `go test ./...`, `go vet ./...`, `go run . bench --json`, `go run . doctor`, `go run . doctor --mcp`, `git diff --check` and docs/public-safety checks.
- Add cross-platform build/release-prep workflow for Linux and macOS, Windows if low-friction.
- Document private-prep vs public release.
- Do not publish a release or change repo visibility without explicit approval.

### 3. Rule-based multi-intent planner

Status: implemented for CLI JSON planning with compact `routing_insight` fields; MCP search/extract responses inherit core response fields.

Goals:

- Add LLM-free deterministic classifier/planner.
- Support multiple intents per query: general, news, docs, academic, factcheck, semantic, code, social/community, people/company, pricing, research, extract.
- Include matched signals, task scores, selected intents and confidence.
- Preserve `--task` compatibility and planning overrides.
- Add `nole classify` and `nole route-plan` JSON output.
- Expose inferred intents in MCP search responses.
- Do not reorder provider priority without evidence.

### 4. Compact insight UX

Status: implemented for core search/extract/classify/route-plan responses, CLI human/JSON output and CLI/MCP error envelopes.

Goals:

- Add one short Nólë insight line for human output and JSON/MCP responses where practical.
- Keep full `route_trace` for debugging.
- Add insight mode config/flag if practical: compact/off/verbose.
- Tell agents not to dump full traces unless debugging.

Usage notes:

- `--insight compact` is the default and emits one deterministic, sanitized `routing_insight` line.
- `--insight off` omits the user-facing `routing_insight` field while preserving `route_trace` where available.
- `--insight verbose` keeps the compact line and adds route trace lines to human search/extract output for debugging.
- Compact insights must not include API keys, bearer tokens, auth headers, raw provider payloads or private URLs.

### 5. Cost policy and premium-capable routing

Goals:

- Model `free-first`, `cost-capped`, `quality-first` policies.
- Keep default no-hidden-paid-spend behavior.
- Distinguish provider status: keyless-free, free-tier-BYOK, premium-capable, unknown-cost, disabled-no-key.
- Do not select a paid provider merely because a paid key exists.
- Add provider_status and budget_status fields without printing secrets.

### 6. Benchmark and route evidence

Status: implemented for deterministic fixture metadata, public-safe Markdown summaries, `docs/ROUTE-EVIDENCE.md`, and docs/claim checks. Optional live summaries remain explicit low-limit smoke artifacts, not CI defaults.

Goals:

- Keep deterministic harness for routing/fallback contract.
- Add sanitized live summary writer if needed.
- Add `docs/ROUTE-EVIDENCE.md` structure.
- Add generic scenarios for docs, code, pricing, people/company, academic, news, social/community, factcheck, semantic and extraction.
- Route matrix changes require evidence.

### 7. Cache and quota ledger

Status: implemented for in-process normalized search/extract TTL cache, cache hit/miss trace/insight fields, file-backed local quota/cost ledger, fail-closed corrupt-ledger recovery and persistence tests.

Goals:

- TTL cache for normalized search/extract responses.
- Cache hit/miss in route_trace/insight.
- File-backed quota/cost ledger.
- Fail closed under no-paid-spend policy when free quota is known exhausted.
- Corruption recovery tests.

### 8. Agent install experience

Status: implemented for agent-readable install docs, provider cost/overage checklist, client support matrix, status-label rules and exact local/VPS/PATH/env/MCP troubleshooting paths. Real-client verification remains Milestone 9.

Goals:

- Improve AGENTS.md and docs until an AI agent can install Nólë from a GitHub link.
- Add exact local/VPS, PATH, env, MCP config and troubleshooting steps.
- Add provider-by-provider free-tier/overage notes.
- Improve setup writers where safe, but do not claim verified support without real client tests.

### 9. Integration verification

Status: implemented for local-offline integration evidence, MCP stdio smoke evidence, no-secret deterministic command verification, and truthful client status labels. Real client UI/tool visibility remains pending until each client is actually launched and tested.

Goals:

- Verify priority agents when installed/available: Claude Code, Codex, OpenCode, Hermes, OpenClaw.
- Record config path, setup command, tool visibility, doctor output and failure notes.
- Keep unavailable clients labeled generic/unverified with future checklist.

### 10. v0.1 private-prep polish

Status: implemented for release-gate alignment, integration-evidence gate documentation, and public-safe local search/extract smoke expectations. Final private-prep report is produced after this milestone merges and post-merge verification passes.

Goals:

- Main clean and CI green.
- Core deterministic commands pass:
  - `nole doctor`
  - `nole doctor --mcp`
  - `nole providers --json`
  - `nole bench --json`
  - classifier/route-plan command once implemented.
- Public-safe local CLI smoke is interpreted truthfully:
  - `nole search "..." --json` may succeed via keyless DDGS or return a sanitized provider/network error envelope.
  - `nole extract "..." --json` fails closed in no-key/free-first mode because v0.1 has no keyless extract provider; it may return content only with explicit user-owned extract-provider keys and policy.
- Final `/tmp/nole-v0.1-private-prep-report.md`.
- Public release/repo visibility still requires explicit user approval.

## Success criteria

- Product framing stable and consistent.
- Priority client docs present with truthful status labels.
- Agent-readable install path works from scratch.
- Multi-intent planner implemented and tested.
- Compact insight implemented and tested.
- Cost policy safe by default and premium-capable by design.
- Benchmark/evidence docs honest.
- CI green.
- Codex review loop complete for each PR.
- Local main clean after merges.
