# searchmcp

`searchmcp` is a Go single-binary BYOK search/retrieval router for AI agents.

Current MVP goals:

- MCP stdio server for Claude Code, Cursor, Codex CLI, OpenCode, Windsurf, and other MCP clients.
- CLI for setup, debugging, scripting, and provider verification.
- Task-based provider routing instead of simple sequential rotation.
- $0 hard-cap/free-tier-first quota policy.
- Fail closed with `no_free_quota` instead of spending money.

Status: early MVP scaffold.

## Commands

```bash
searchmcp doctor
searchmcp providers --json
searchmcp search "model context protocol go sdk" --task general --json
searchmcp extract "https://example.com" --json
searchmcp mcp
```

## Design

See `docs/plans/2026-05-12-searchmcp-mvp.md`.
