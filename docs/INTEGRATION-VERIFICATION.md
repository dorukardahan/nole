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
- Tools observed: search, extract, provider_status, budget_status.
- Provider status JSON: providers --json generated with provider keys unset.
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

This document is the offline/CI integration evidence artifact. For M11 live-client evidence on a macOS host (Claude Code, Codex CLI, OpenCode and Kimi), see `docs/CLIENTS/LIVE-VERIFICATION.md`. That artifact is recorded separately so this document can continue to describe only the offline/CI run.

- This evidence does not verify real-client UI/tool visibility.
- This evidence does not verify client-specific environment inheritance in GUI/gateway/service modes.
- This evidence does not measure live web result quality.
- This evidence does not measure provider uptime, currentness of indexes, live quota behavior or provider-account billing.
- This evidence does not upgrade any client to verified status.

## Raw artifact policy

No raw provider payloads, provider key values, auth headers, private URLs or private paths are included.
Local scratch outputs are temporary and are not committed.
