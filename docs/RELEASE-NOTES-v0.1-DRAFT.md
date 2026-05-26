# v0.1 release notes draft

This is a draft for a future Nólë v0.1 release. It does not create tags,
publish a GitHub Release, upload assets, publish packages or deploy anything.

## Release status

- Current status: public repository with v0.1 release prep ready.
- Publication status: no GitHub Release is published by this draft.
- Repository visibility: public.
- Release assets: built and uploaded only by the tag-triggered release workflow.
- Package registries: not published by this draft.
- Hosted deployment: not part of v0.1 unless separately approved.

## Suggested title

Nólë v0.1.x — local web search router for AI agents

Use the approved tag in the final title. Do not move or reuse an existing tag.

## Summary

Nólë is a free, local web search router for AI agents and coding CLI tools. It gives Claude Code, Codex CLI, OpenClaw, Hermes Agent, OpenCode, Cursor, Kimi and generic MCP clients a local CLI/MCP search and extraction layer backed by BYOK/free-tier providers.

The release focuses on truthful integration evidence, deterministic routing
tests, default no-hidden-paid-spend policy and agent-readable installation docs.

## Highlights

- Go single-binary CLI with MCP stdio support.
- Stable MCP tools: `search`, `extract`, `provider_status`, `budget_status`.
- CLI commands for search, extract, classify, route-plan, providers, doctor and benchmark.
- LLM-free multi-intent classifier and route planner.
- `free-first` default policy with premium-capable provider support only when explicitly allowed.
- Provider adapters/status support for Brave, Tavily, Firecrawl, DDGS fallback and optional local Scrapling extraction fallback.
- In-process TTL cache and optional file-backed local quota/cost ledger.
- Sanitized `route_trace` and compact `routing_insight` fields.
- `doctor --mcp` subprocess smoke for protocol cleanliness and tool visibility.
- Deterministic offline benchmark harness and public-safe route evidence summary.
- Agent setup docs and client-specific setup writers/checklists, including the Hermes Agent YAML setup writer.
- Priority named-client verification evidence recorded for Claude Code, Codex CLI, OpenCode, Kimi, Cursor, OpenClaw and Hermes Agent.
- CI gates, public-safety secret scan and automated cross-platform GitHub
  Release build/checksum validation.

## Provider and cost safety

Default policy is `free-first`. A provider key by itself does not make a paid-capable provider eligible for calls. Premium-capable providers require an explicit policy such as `cost-capped` or `quality-first`, local cap/estimate settings where applicable and user acceptance of provider-account cost risk.

Nólë reports key presence/status only. It must not print key values, auth headers or raw provider payloads.

## Benchmark and evidence wording

The deterministic benchmark validates routing/fallback contracts and fixture coverage. It does not measure live web quality, provider uptime, provider billing or statistically meaningful provider rankings.

Optional live benchmark summaries are separately approval-gated. They should use `docs/LIVE-BENCHMARK-PLAN.md` and `docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md` and remain sanitized.

## Installation sketch

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

Install the binary to PATH or use an absolute path in MCP configs:

```bash
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
nole doctor
```

## Verification before release publication

Before converting this draft into a published release, run:

```bash
./scripts/check-docs-framing.sh
./scripts/check-benchmark-claims.sh
./scripts/check-integration-evidence.sh
go test ./...
go vet ./...
go run . doctor
go run . doctor --mcp
go run . providers --json
go run . bench --json
go run . bench --evidence-md
./scripts/check-release-builds.sh
./scripts/secret-scan.sh
git diff --check
```

## Explicit non-claims

This v0.1 draft does not claim:

- Nólë is a hosted SaaS;
- a GitHub Release has been published;
- release assets are available before the release workflow uploads them;
- package registries are published;
- any provider is globally ranked over another;
- deterministic offline benchmarks measure live web quality;
- optional live smoke observations are statistically significant;
- local quota/cost ledger equals provider-dashboard balance;
- generic MCP clients are verified without named runtime evidence.

## Known limitations

- HTTP/REST remains experimental compared with CLI and MCP stdio.
- Extract quality depends on configured provider availability and policy.
- DDGS is keyless fallback/control only; it is not a provider-quality guarantee.
- Scrapling extraction is optional local runtime integration. Users install and operate their own Python environment, and must respect target website terms, robots.txt and rate limits.
- Paid-capable provider usage depends on the user's own account settings and explicit policy.
- Package-manager channels are future work unless separately approved.

## Publication checklist

Before publishing:

1. Confirm explicit release approval.
2. Confirm the repository is intended to remain public.
3. Confirm tag name and release title.
4. Confirm asset matrix and checksums.
5. Confirm public-safety scan and CI success.
6. Confirm no raw logs, secrets, private queries or private URLs are included.
7. Push only the approved semantic version tag, then let the release workflow
   publish the GitHub Release and assets.
