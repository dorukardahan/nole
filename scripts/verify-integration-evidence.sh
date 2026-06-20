#!/usr/bin/env bash
set -euo pipefail

# Generate a public-safe local/offline integration evidence summary.
# This script intentionally does not launch real external clients and does not
# make live provider calls. It builds Nólë locally, runs deterministic smoke
# commands with provider keys unset, and prints a Markdown summary to stdout.

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

nole="$tmpdir/nole"
go build -o "$nole" .
mkdir -p "$tmpdir/home" "$tmpdir/config" "$tmpdir/state"

# Provider keys are fully unset and the local Nólë env file is disabled. Extract
# and search_and_extract are registered via the keyless httpfetch backstop, so no
# fake key is needed to populate the MCP tool surface.
common_env=(
  env
  -u BRAVE_API_KEY
  -u BRAVE_SEARCH_API_KEY
  -u JINA_API_KEY
  -u FIRECRAWL_API_KEY
  -u TAVILY_API_KEY
  -u NOLE_SCRAPLING_PYTHON
  HOME="$tmpdir/home"
  XDG_CONFIG_HOME="$tmpdir/config"
  XDG_STATE_HOME="$tmpdir/state"
  NOLE_DISABLE_ENV_FILE=1
  NOLE_COST_POLICY=free-first
  NOLE_QUOTA_LEDGER_PATH=memory
  NOLE_CACHE_TTL=5m
  NOLE_MCP_SMOKE_BINARY="$nole"
)

"${common_env[@]}" "$nole" doctor >"$tmpdir/doctor.txt"
"${common_env[@]}" "$nole" doctor --mcp >"$tmpdir/doctor-mcp.txt"
"${common_env[@]}" "$nole" providers --json >"$tmpdir/providers.json"
"${common_env[@]}" "$nole" bench --json >"$tmpdir/bench.json"
"${common_env[@]}" "$nole" bench --evidence-md >"$tmpdir/route-evidence.md"

mcp_tools="unknown"
if grep -Fq 'tools: [budget_status extract provider_status research search search_and_extract]' "$tmpdir/doctor-mcp.txt"; then
  mcp_tools="search, extract, search_and_extract, provider_status, budget_status, research"
fi

provider_status="generated with provider keys unset"
if grep -Fq '"name"' "$tmpdir/providers.json" || grep -Fq '"providers"' "$tmpdir/providers.json"; then
  provider_status="providers --json generated with provider keys unset"
fi

cat <<EOF
# Integration verification evidence

Mode: local-offline
Artifact kind: integration_verification_summary
Real client launches: not run
Live provider calls: none
Network required: false
Secrets required: false
Cost policy: free-first
Provider key handling: presence/status only; process env keys unset and local env file disabled during this run

## What this verifies

- CLI binary builds from the current repository state.
- Core no-secret smoke commands execute locally.
- MCP stdio smoke: verified by nole doctor --mcp.
- MCP smoke binary: temporary binary built from current repository state.
- Tools observed: ${mcp_tools}.
- Provider status JSON: ${provider_status}.
- Deterministic benchmark JSON and Markdown evidence generation complete without live provider calls.
- Repo-tested setup writers: Claude Code, Codex CLI, OpenCode, Cursor, Kimi.
- Generic/unverified clients in this offline artifact: generic MCP clients.

## Commands represented

- go build -o [temporary-binary] .
- nole doctor
- nole doctor --mcp
- nole providers --json
- nole bench --json
- nole bench --evidence-md

Runtime Nólë commands above ran with provider key environment variables unset, the local Nólë env file disabled, and local-only accounting/cache settings. The build step is provider-free and uses only the current repository state.

## Client status evidence

| Client | Evidence in this run | Status label |
| --- | --- | --- |
| Claude Code | setup writer/config merge covered by repo tests; real client not launched in this offline run | repo-tested; live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| Codex CLI | TOML setup writer/config merge covered by repo tests; real client not launched in this offline run | repo-tested; live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| OpenCode | setup writer/config merge covered by repo tests; real client not launched in this offline run | repo-tested; live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| Cursor | shared MCP JSON setup path covered by repo tests; real client not launched in this offline run | repo-tested; live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| Kimi | \`~/.kimi/mcp.json\` setup writer/config merge covered by repo tests; real client not launched in this offline run | repo-tested; live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| Hermes Agent | real client not launched in this offline run; live evidence is recorded separately in \`docs/CLIENTS/LIVE-VERIFICATION.md\` | live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| OpenClaw | real client not launched in this offline run; live evidence is recorded separately in \`docs/CLIENTS/LIVE-VERIFICATION.md\` | live evidence in \`docs/CLIENTS/LIVE-VERIFICATION.md\` |
| generic MCP clients | generic stdio command template only | generic/unverified |

## Limitations

This document is the offline/CI integration evidence artifact. For live-client evidence — M11 (Claude Code, Codex CLI, OpenCode, Kimi), the 2026-05-20 Cursor follow-up, the 2026-05-20 OpenClaw follow-up, and the 2026-05-20 Hermes Agent follow-up — see \`docs/CLIENTS/LIVE-VERIFICATION.md\`. That artifact is recorded separately so this document can continue to describe only the offline/CI run.

- This evidence does not verify real-client UI/tool visibility.
- This evidence does not verify client-specific environment inheritance in GUI/gateway/service modes.
- This evidence does not measure live web result quality.
- This evidence does not measure provider uptime, currentness of indexes, live quota behavior or provider-account billing.
- This evidence does not upgrade any client to verified status.

## Raw artifact policy

No raw provider payloads, provider key values, auth headers, private URLs or private paths are included.
Local scratch outputs are temporary and are not committed.
EOF
