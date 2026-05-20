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
  docs/CLIENTS/claude-code.md
  docs/CLIENTS/codex.md
  docs/CLIENTS/opencode.md
  docs/CLIENTS/openclaw.md
  docs/CLIENTS/hermes.md
  docs/CLIENTS/generic-mcp.md
  docs/CLIENTS/cursor.md
  docs/CLIENTS/kimi.md
  docs/CLIENTS/README.md
)

for path in "${required[@]}"; do
  [[ -f "$path" ]] || fail "missing required doc $path"
done

head -n 8 README.md | grep -Fq "Free web search router for AI agents and coding CLI tools." \
  || fail "README first screen must contain the primary one-liner"

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
grep -Fq 'Codex CLI lists or can call Nólë MCP tools `search`, `extract`, `provider_status`, `budget_status`' docs/CLIENTS/codex.md \
  || fail "Codex verification checklist must name all required MCP tools"
grep -Fq 'Nólë tools `search`, `extract`, `provider_status`, `budget_status` are visible in Cursor' docs/CLIENTS/cursor.md \
  || fail "Cursor verification checklist must name all required MCP tools"

for path_doc in docs/CLIENTS/claude-code.md docs/CLIENTS/codex.md docs/CLIENTS/opencode.md docs/CLIENTS/cursor.md; do
  grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$path_doc" \
    || fail "$path_doc setup snippet must be PATH-safe"
done

for client in docs/CLIENTS/*.md; do
  grep -Eq "Status: (verified|repo-tested|generic/unverified)" "$client" \
    || fail "$client missing verification status label"
done

echo "docs framing check passed"
