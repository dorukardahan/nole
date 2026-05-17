# Nólë Next Steps

Nólë is intentionally staying small for the public-readiness pass. The items below should be implemented as separate, test-driven changes rather than bundled into the current hardening work. Keep the repository generic and public-safe: no API keys, private queries, personal paths, raw live benchmark logs, or client/runtime-specific private details.

## Benchmark and routing evidence

- Expand the offline fixture set only when a new case changes a provider decision or catches a real routing failure mode. Keep fixtures deterministic, generic, and safe to commit.
- Add an optional live benchmark writer that emits sanitized summaries only: fixture version, provider/task success counts, latency ranges, result counts, citation/source URL quality, extraction success, and error categories. Do not commit raw provider payloads, headers, keys, or private queries.
- Keep the current route matrix unchanged until new evidence supports a change. Offline assumptions should be labeled as fixture evidence; live provider canaries should be explicit, low-limit, and reproducible.
- Add a public route-evidence document once enough offline/live results exist to justify changing provider order.
- Keep empty-result semantics aligned between production and bench: empty search results and empty extracted content should remain fallback attempts, with explicit `route_trace` reasons.

## Cache and quota ledger

- TTL cache can reduce free-tier usage, but should be opt-in or clearly bounded. Suggested scope: cache normalized search/extract responses by provider, task, query/URL, and options; include TTL and a cache-bypass flag.
- File-backed quota ledger can make the `$0` hard-cap behavior more robust across restarts. Suggested scope: store provider, window, free remaining/unknown, keyless-free flag, last updated, and source of quota knowledge under the user config directory.
- Do not add partial cache/quota code without tests for stale entries, corruption recovery, and fail-closed behavior when a paid provider has no known free quota.

## Agent integrations

Verified writers currently target the config formats implemented by `nole setup` (Claude/Cursor-style `mcpServers`, Codex TOML `mcp_servers`, OpenCode `mcp`). Continue to merge existing config and create backups before updates. Preserve unknown client-specific JSON fields and preserve existing file modes; new sensitive config/backup files should stay private (`0600`).

Generic MCP stdio template for clients that are not yet verified:

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole",
      "args": ["mcp"]
    }
  }
}
```

Client notes:

- Codex CLI: template uses `[mcp_servers.nole]` in `~/.codex/config.toml`; verify exact CLI version behavior before marking new setup flows as verified.
- Claude Code CLI: Claude/Cursor-style `mcpServers` JSON is supported by the current writer; users may still need to restart or enable MCP tools per workspace.
- OpenCode: current writer uses top-level `mcp`; keep this marked as the implemented template unless tested against a specific OpenCode release.
- Kimi CLI, Hermes, OpenClaw, and other MCP clients: provide generic stdio examples until their stable config formats are verified in this repo. Do not label these as verified integrations until tested.

## Doctor and MCP reliability

- Keep `doctor --mcp` as a real subprocess smoke test that performs `initialize` and `tools/list`, verifies expected tools, and treats non-JSON stdout as a failure.
- Keep all protocol-external logs on stderr. Any stdout banner in `nole mcp` should be treated as a bug because MCP stdio reserves stdout for JSON-RPC.
- Add client-config discovery checks only when they can avoid printing secrets and avoid reading provider key values.

## Configuration

- Add a small config file for default task, timeout, cache TTL, retry knobs, and route-matrix overrides.
- Keep environment variables as the simplest setup path; diagnostics should show only set/not-set status and never provider key values.

## CI and release readiness

- Add GitHub Actions for `go test ./...`, `go vet ./...`, offline `nole bench --json`, and cross-platform builds.
- Add release automation only after the repository is intentionally made public.
- Add a public-safety check that rejects `.env`, raw benchmark output, provider keys, bearer tokens, and private local paths before release artifacts are published.
