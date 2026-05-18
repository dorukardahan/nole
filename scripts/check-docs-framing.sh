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
  docs/NEXT-STEPS.md
  docs/CLIENTS/claude-code.md
  docs/CLIENTS/codex.md
  docs/CLIENTS/opencode.md
  docs/CLIENTS/openclaw.md
  docs/CLIENTS/hermes.md
  docs/CLIENTS/generic-mcp.md
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

grep -Fq "free-first" docs/PRODUCT.md \
  || fail "product docs must include free-first policy"
grep -Fq "premium-capable" docs/PRODUCT.md \
  || fail "product docs must include premium-capable provider philosophy"

for client in docs/CLIENTS/*.md; do
  grep -Eq "Status: (verified|repo-tested|generic/unverified)" "$client" \
    || fail "$client missing verification status label"
done

echo "docs framing check passed"
