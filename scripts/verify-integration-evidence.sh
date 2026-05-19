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

common_env=(
  env
  -u BRAVE_API_KEY
  -u BRAVE_SEARCH_API_KEY
  -u TAVILY_API_KEY
  -u JINA_API_KEY
  -u FIRECRAWL_API_KEY
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
if grep -Fq 'tools: [budget_status extract provider_status search]' "$tmpdir/doctor-mcp.txt"; then
  mcp_tools="search, extract, provider_status, budget_status"
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
Provider key handling: presence/status only; keys unset during this run

## What this verifies

- CLI binary builds from the current repository state.
- Core no-secret smoke commands execute locally.
- MCP stdio smoke: verified by nole doctor --mcp.
- MCP smoke binary: temporary binary built from current repository state.
- Tools observed: ${mcp_tools}.
- Provider status JSON: ${provider_status}.
- Deterministic benchmark JSON and Markdown evidence generation complete without live provider calls.
- Repo-tested setup writers: Claude Code, Codex CLI, OpenCode, Cursor.
- Generic/unverified clients: Hermes Agent, OpenClaw, Kimi, generic MCP clients.

## Commands represented

- go build -o [temporary-binary] .
- nole doctor
- nole doctor --mcp
- nole providers --json
- nole bench --json
- nole bench --evidence-md

All commands above ran with provider key environment variables unset and with local-only accounting/cache settings.

## Client status evidence

| Client | Evidence in this run | Status label |
| --- | --- | --- |
| Claude Code | setup writer/config merge covered by repo tests; real client not launched | repo-tested, live verification pending |
| Codex CLI | TOML setup writer/config merge covered by repo tests; real client not launched | repo-tested, live verification pending |
| OpenCode | setup writer/config merge covered by repo tests; real client not launched | repo-tested, live verification pending |
| Cursor | shared MCP JSON setup path covered by repo tests; real client not launched | repo-tested, live verification pending |
| Hermes Agent | generic MCP template only; no local Hermes config/tool visibility test recorded | generic/unverified |
| OpenClaw | generic MCP template only; no real client config/tool visibility test recorded | generic/unverified |
| Kimi | generic MCP template only; no real client config/tool visibility test recorded | generic/unverified |
| generic MCP clients | generic stdio command template only | generic/unverified |

## Limitations

- This evidence does not verify real-client UI/tool visibility.
- This evidence does not verify client-specific environment inheritance in GUI/gateway/service modes.
- This evidence does not measure live web result quality.
- This evidence does not measure provider uptime, currentness of indexes, live quota behavior or provider-account billing.
- This evidence does not upgrade any client to verified status.

## Raw artifact policy

No raw provider payloads, provider key values, auth headers, private URLs or private paths are included.
Local scratch outputs are temporary and are not committed.
EOF
