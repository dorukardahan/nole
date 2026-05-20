#!/usr/bin/env bash
set -euo pipefail

target="${1:-docs/INTEGRATION-VERIFICATION.md}"

fail() {
  echo "integration evidence check failed: $*" >&2
  exit 1
}

[[ -f "$target" ]] || fail "missing integration evidence doc $target"

grep -Fq "# Integration verification evidence" "$target" \
  || fail "missing integration evidence title"
grep -Fq "Mode: local-offline" "$target" \
  || fail "must label evidence mode as local-offline"
grep -Fq "Real client launches: not run" "$target" \
  || fail "must state real client launches were not run"
grep -Fq "Live provider calls: none" "$target" \
  || fail "must state live provider calls were not made"
grep -Fq "Network required: false" "$target" \
  || fail "must state no network is required"
grep -Fq "Secrets required: false" "$target" \
  || fail "must state secrets are not required"
grep -Fq "MCP stdio smoke: verified by nole doctor --mcp" "$target" \
  || fail "must record MCP stdio smoke evidence"
grep -Fq "MCP smoke binary: temporary binary built from current repository state" "$target" \
  || fail "must state MCP smoke uses the current repository binary"
grep -Fq "Tools observed: search, extract, provider_status, budget_status" "$target" \
  || fail "must record required MCP tools"
grep -Fq "Repo-tested setup writers: Claude Code, Codex CLI, OpenCode, Cursor, Kimi" "$target" \
  || fail "must distinguish repo-tested setup writers"
grep -Fq "Generic/unverified clients in this offline artifact: generic MCP clients" "$target" \
  || fail "must keep generic MCP clients generic/unverified"
grep -Fq '| Hermes Agent | real client not launched in this offline run; live evidence is recorded separately in `docs/CLIENTS/LIVE-VERIFICATION.md` | live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` |' "$target" \
  || fail "must point Hermes Agent offline row to live verification evidence"
grep -Fq '| OpenClaw | real client not launched in this offline run; live evidence is recorded separately in `docs/CLIENTS/LIVE-VERIFICATION.md` | live evidence in `docs/CLIENTS/LIVE-VERIFICATION.md` |' "$target" \
  || fail "must point OpenClaw offline row to live verification evidence"
grep -Fq "This evidence does not verify real-client UI/tool visibility" "$target" \
  || fail "must state real-client visibility limitation"
grep -Fq "This evidence does not measure live web result quality" "$target" \
  || fail "must state live web quality limitation"
grep -Fq "No raw provider payloads, provider key values, auth headers, private URLs or private paths are included." "$target" \
  || fail "must state raw/private artifact policy"

if grep -Eiq "fastest|best provider|#1 provider|guaranteed|outperforms|faster than" "$target"; then
  fail "unsupported benchmark/ranking claim in $target"
fi

if grep -Eiq "live client verified|real client verified|Live provider calls: (yes|true)|Network required: true|Secrets required: true" "$target"; then
  fail "overclaiming live/client/provider verification in $target"
fi

echo "integration evidence check passed"
