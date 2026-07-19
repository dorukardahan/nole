#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "docs framing check failed: $*" >&2
  exit 1
}

required=(
  README.md
  AGENTS.md
  docs/PRODUCT.md
  docs/AGENT-INSTALL.md
  docs/PROVIDER-KEYS.md
  docs/BENCHMARKS.md
  docs/ROUTE-EVIDENCE.md
  docs/INTEGRATION-VERIFICATION.md
  docs/NEXT-STEPS.md
  docs/RELEASE-GATES.md
  docs/PUBLIC-RELEASE-CHECKLIST.md
  docs/RELEASE-NOTES-v0.1-DRAFT.md
  docs/PACKAGING.md
  docs/COST-QUOTA-CACHE-QUALITY.md
  docs/CLIENTS/claude-code.md
  docs/CLIENTS/codex.md
  docs/CLIENTS/opencode.md
  docs/CLIENTS/openclaw.md
  docs/CLIENTS/hermes.md
  docs/CLIENTS/generic-mcp.md
  docs/CLIENTS/cursor.md
  docs/CLIENTS/kimi.md
  docs/CLIENTS/antigravity.md
  docs/CLIENTS/gemini.md
  docs/CLIENTS/grok.md
  docs/CLIENTS/LIVE-VERIFICATION.md
  docs/CLIENTS/README.md
)

for path in "${required[@]}"; do
  [[ -f "$path" ]] || fail "missing required doc $path"
done

head -n 8 README.md | grep -Fq "Local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools." \
  || fail "README first screen must contain the primary one-liner"

grep -Fq "Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools." AGENTS.md \
  || fail "AGENTS product framing must match the primary one-liner"

grep -Fq -- "- Local, free-first/BYOK web search and page extraction router." docs/PRODUCT.md \
  || fail "product category labels must include the primary search-and-extraction framing"

grep -Fq '"description": "Local, free-first/BYOK web search and page extraction router for AI agents"' packaging/scoop/nole.json.tmpl \
  || fail "Scoop package description must match the primary search-and-extraction framing"

grep -Fq 'desc "Local, free-first/BYOK web search and page extraction router for AI agents"' packaging/homebrew/nole.rb.tmpl \
  || fail "Homebrew package description must match the primary search-and-extraction framing"

stale_primary_framing=$(git grep -niE 'web[- ]search router' -- README.md AGENTS.md docs packaging || true)
if [ -n "$stale_primary_framing" ]; then
  fail "public docs contain stale primary product framing: $stale_primary_framing"
fi

stale_gateway_framing=$(git grep -niE 'internet gateway|dumb-but.*gateway|dumb-but-excellent pipe' -- README.md AGENTS.md docs packaging || true)
if [ -n "$stale_gateway_framing" ]; then
  fail "public docs contain stale gateway-era product framing: $stale_gateway_framing"
fi

head -n 20 README.md | grep -Fq "not a hosted SaaS" \
  || fail "README first screen must say Nólë is not hosted SaaS"

head -n 20 README.md | grep -Fq "not a replacement for your agent" \
  || fail "README first screen must say Nólë is not an agent replacement"

if head -n 20 README.md | grep -Eiq "^#+ .*MCP server"; then
  fail "README must not frame MCP server as the primary title/category"
fi

grep -Fq "MCP is one important entrypoint" README.md \
  || fail "README must describe MCP as an entrypoint, not the product category"

grep -Fq "Claude Code" README.md \
  || fail "README must mention Claude Code"
grep -Fq "Codex" README.md \
  || fail "README must mention Codex"
grep -Fq "OpenClaw" README.md \
  || fail "README must mention OpenClaw"
grep -Fq "Hermes" README.md \
  || fail "README must mention Hermes"
grep -Fq "OpenCode" README.md \
  || fail "README must mention OpenCode"
grep -Fq "Kimi" README.md \
  || fail "README must mention Kimi as secondary/generic"
grep -Fq "Cursor" README.md \
  || fail "README must mention Cursor as secondary/generic"

grep -Fq "Deterministic offline harness" docs/BENCHMARKS.md \
  || fail "benchmark docs must describe deterministic offline harness"
grep -Fiq "does not measure live web quality" docs/BENCHMARKS.md \
  || fail "benchmark docs must not overclaim offline web quality"
./scripts/check-benchmark-claims.sh
./scripts/check-integration-evidence.sh

grep -Fq "./scripts/check-integration-evidence.sh" docs/RELEASE-GATES.md \
  || fail "release gates must list the integration evidence guard"
grep -Fq "./scripts/verify-integration-evidence.sh" docs/RELEASE-GATES.md \
  || fail "release gates must list generated integration evidence verification"
grep -Fq "nole search" docs/RELEASE-GATES.md \
  || fail "release gates must include search smoke expectations"
grep -Fq "nole extract" docs/RELEASE-GATES.md \
  || fail "release gates must include extract smoke expectations"
grep -Fq "docs/PUBLIC-RELEASE-CHECKLIST.md" docs/RELEASE-GATES.md \
  || fail "release gates must link the public release checklist"
grep -Fq "docs/PACKAGING.md" README.md \
  || fail "README must link packaging prep docs"
root_command_span=$(awk '/cmd\.AddCommand\(/ { if (first == 0) first = NR; last = NR; count++ } END { if (count == 0) exit 1; printf "%d %d %d", count, first, last }' internal/cli/root.go) \
  || fail "could not derive root command count/range from internal/cli/root.go"
read -r root_command_count root_command_first root_command_last <<EOF
$root_command_span
EOF
expected_root_count="Registers all ${root_command_count} subcommands"
expected_root_range='Subcommands (registered `internal/cli/root.go:'"${root_command_first}-${root_command_last}"'`)'
grep -Fq "$expected_root_count" docs/ARCHITECTURE.md \
  || fail "architecture docs must match current root command count: $expected_root_count"
grep -Fq "$expected_root_range" docs/ARCHITECTURE.md \
  || fail "architecture docs must match current root.go subcommand range: $expected_root_range"
stale_root_counts=$(grep -Eon '[0-9]+ subcommands' docs/ARCHITECTURE.md | grep -Fv ":${root_command_count} subcommands" || true)
if [ -n "$stale_root_counts" ]; then
  fail "architecture docs contain stale subcommand count(s): $stale_root_counts"
fi
stale_root_ranges=$(grep -Eon '((internal/cli/)?root\.go):[0-9]+-[0-9]+' docs/ARCHITECTURE.md \
  | grep -Fv ":internal/cli/root.go:${root_command_first}-${root_command_last}" \
  | grep -Fv ":root.go:${root_command_first}-${root_command_last}" || true)
if [ -n "$stale_root_ranges" ]; then
  fail "architecture docs contain stale root.go subcommand range(s): $stale_root_ranges"
fi
if grep -Fq "no keyless extract provider" docs/NEXT-STEPS.md; then
  fail "next-steps docs contain stale no-keyless-extract wording"
fi
if grep -Fq "fake-integration-evidence-key" scripts/verify-integration-evidence.sh; then
  fail "integration evidence script must not inject a fake provider key"
fi
if grep -Eq '^[[:space:]]*[A-Z0-9_]*API_KEY=' scripts/verify-integration-evidence.sh; then
  fail "integration evidence script must not assign provider API keys"
fi
grep -Fq "NOLE_DISABLE_ENV_FILE=1" scripts/verify-integration-evidence.sh \
  || fail "integration evidence script must disable the local Nólë env file"
if grep -Fq "no keyless-free provider" docs/RELEASE-GATES.md; then
  fail "release gates must not claim extract has no keyless-free provider"
fi
if grep -Fq "firecrawl/tavily/brave check only StatusOK and ignore success:false" docs/ARCHITECTURE.md; then
  fail "architecture docs must not list resolved Firecrawl success:false handling as open"
fi
if grep -Fq 'firecrawlSearchResponse`/`firecrawlScrapeResponse` carry a `Success bool`' docs/ARCHITECTURE.md; then
  fail "architecture docs must not describe Firecrawl success as bool after pointer guard fix"
fi
if grep -Fq "500 for Tavily/Firecrawl" docs/PROVIDER-KEYS.md; then
  fail "provider docs must not use stale Firecrawl 500-call quota wording"
fi
grep -Fq "Firecrawl | yes/no | present/absent/not checked | keyless-free/free-tier-BYOK/premium-capable" docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md \
  || fail "live benchmark template must include Firecrawl keyless-free cost class"
grep -Fq "httpfetch | yes/no | not required | keyless-free extract backstop" docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md \
  || fail "live benchmark template must include httpfetch inventory row"
grep -Fq 'keyless `httpfetch` backstop' docs/NEXT-STEPS.md \
  || fail "next-steps docs must mention the keyless httpfetch extract backstop"
grep -Fq "optional speed/fidelity upgrade hint" docs/PROVIDER-KEYS.md \
  || fail "provider docs must frame setup_tip as an optional upgrade hint when keyless baselines work"
if grep -R -Fq "Until then your AI tool will use its own built-in fallbacks where needed." README.md CHANGELOG.md docs; then
  fail "public docs must not reintroduce stale setup_tip built-in-fallback wording"
fi
grep -Fq "Public repository visibility" docs/PUBLIC-RELEASE-CHECKLIST.md \
  || fail "public release checklist must gate repository visibility"
grep -Fq "GitHub Release publication approved" docs/PUBLIC-RELEASE-CHECKLIST.md \
  || fail "public release checklist must gate GitHub Release publication"
grep -Fq "Release asset upload approved" docs/PUBLIC-RELEASE-CHECKLIST.md \
  || fail "public release checklist must gate release assets"
grep -Fq "This is a draft" docs/RELEASE-NOTES-v0.1-DRAFT.md \
  || fail "release notes draft must state it is a draft"
grep -Fq "does not create tags" docs/PACKAGING.md \
  || fail "packaging docs must preserve non-publishing scope"
grep -Fq "local accounting only" docs/COST-QUOTA-CACHE-QUALITY.md \
  || fail "cost/quota/cache audit must avoid provider-dashboard claims"

grep -Fq "free-first" docs/PRODUCT.md \
  || fail "product docs must include free-first policy"
grep -Fq "premium-capable" docs/PRODUCT.md \
  || fail "product docs must include premium-capable provider philosophy"

grep -Fq "M8 agent handoff checklist" docs/AGENT-INSTALL.md \
  || fail "agent install docs must include the M8 handoff checklist"
grep -Fq "Agent copy/paste install block" docs/AGENT-INSTALL.md \
  || fail "agent install docs must include a copy/paste install block"
grep -Fq "PATH and absolute binary discovery" docs/AGENT-INSTALL.md \
  || fail "agent install docs must explain PATH and absolute binary discovery"
grep -Fq "Cost-aware environment template" docs/AGENT-INSTALL.md \
  || fail "agent install docs must include cost-aware env template"
grep -Fq "## Tool decision recipe" docs/AGENT-INSTALL.md \
  || fail "agent install docs must include a search/research tool decision recipe"
grep -Fq 'Use `search` when' docs/AGENT-INSTALL.md \
  || fail "agent install recipe must say when to use search"
grep -Fq 'Use `search_and_extract` when' docs/AGENT-INSTALL.md \
  || fail "agent install recipe must say when to use search_and_extract"
grep -Fq 'Use `extract` when' docs/AGENT-INSTALL.md \
  || fail "agent install recipe must say when to use extract"
grep -Fq 'Use `research` when' docs/AGENT-INSTALL.md \
  || fail "agent install recipe must say when to use research"
grep -Fq '`search`, `search_and_extract`, and `research` accept optional' CHANGELOG.md \
  || fail "changelog must document research SearchOptions parity"
grep -Fq 'and `/api/research`' README.md \
  || fail "README search options docs must include REST research"
grep -Fq '`search`, `search_and_extract`, and `research`' docs/STABILITY.md \
  || fail "stability docs must include MCP research search options"
grep -Fq 'For `research`, they apply to internal' docs/STABILITY.md \
  || fail "stability docs must state research SearchOptions scope"
grep -Fq 'Router.Candidate' docs/ARCHITECTURE.md \
  || fail "architecture docs must describe the shared Router.Candidate runtime path"
if grep -Fq 'Only exercised by tests — `service.go` reimplements this loop' docs/ARCHITECTURE.md; then
  fail "architecture docs must not claim Router.Select is test-only after route candidate refactor"
fi
if grep -Fqi 'service reimplements the loop' docs/ARCHITECTURE.md; then
  fail "architecture docs must not claim service reimplements the route loop after route candidate refactor"
fi
if grep -Fqi 'Router.Select' docs/ARCHITECTURE.md && grep -Fqi 'NOT on this path' docs/ARCHITECTURE.md; then
  fail "architecture docs must not claim Router.Select is absent from the shared route candidate path"
fi
if grep -Fq 'Service.routeFor` reaches into `s.router.matrix`' docs/ARCHITECTURE.md; then
  fail "architecture docs must not claim Service.routeFor bypasses Router"
fi
if grep -Fq 'reads `s.router.matrix' docs/ARCHITECTURE.md; then
  fail "architecture docs must not claim routeFor reads the router matrix directly"
fi
if grep -Fq './nole extract <url>` requires a Tavily or Firecrawl key' CONTRIBUTING.md; then
  fail "contributing docs must not claim extract requires Tavily/Firecrawl keys"
fi
grep -Fq "Client support matrix" docs/CLIENTS/README.md \
  || fail "client docs index must include support matrix"
grep -Fq "Do not upgrade a status label without evidence" docs/CLIENTS/README.md \
  || fail "client docs index must preserve status-label truthfulness rule"
grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' README.md \
  || fail "README install snippet must be PATH-safe"
grep -Fq "Optional live/provider smoke" AGENTS.md \
  || fail "AGENTS install verification must make live/provider smoke optional"
grep -Fq "| Cursor | verified (GUI MCP path) |" docs/CLIENTS/README.md \
  || fail "client support matrix must align Cursor setup-writer status"
grep -Fq "Status: verified (GUI MCP path + chat-agent tool dispatch)." docs/CLIENTS/cursor.md \
  || fail "Cursor client doc must use the recorded verified status"
grep -Fq "| OpenClaw | verified (OpenClaw Gateway/agent MCP path) |" docs/CLIENTS/README.md \
  || fail "client support matrix must align OpenClaw live verification status"
grep -Fq "Status: verified (OpenClaw Gateway/agent MCP path)." docs/CLIENTS/openclaw.md \
  || fail "OpenClaw client doc must use the recorded verified status"
grep -Fq "| Hermes Agent | verified (Hermes Agent MCP profile path) |" docs/CLIENTS/README.md \
  || fail "client support matrix must align Hermes Agent live verification status"
grep -Fq "Status: verified (Hermes Agent MCP profile path + chat-agent tool dispatch)." docs/CLIENTS/hermes.md \
  || fail "Hermes Agent client doc must use the recorded verified status"
grep -Fq 'Codex CLI lists or can call Nólë MCP tools `search`, `provider_status`, `budget_status`, `extract`, and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop' docs/CLIENTS/codex.md \
  || fail "Codex verification checklist must name the tool surface incl. the always-on keyless extract"
grep -Fq 'Nólë tools `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop' docs/CLIENTS/cursor.md \
  || fail "Cursor verification checklist must name the tool surface incl. the always-on keyless extract"

for path_doc in docs/CLIENTS/claude-code.md docs/CLIENTS/codex.md docs/CLIENTS/opencode.md docs/CLIENTS/cursor.md; do
  grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$path_doc" \
    || fail "$path_doc setup snippet must be PATH-safe"
done

for client in docs/CLIENTS/*.md; do
  grep -Eq "Status: (verified|repo-tested|generic/unverified)" "$client" \
    || fail "$client missing verification status label"
done

echo "docs framing check passed"
