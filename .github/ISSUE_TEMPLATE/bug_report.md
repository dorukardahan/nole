---
name: Bug report
about: Report a defect, unexpected behavior, or regression
title: ''
labels: bug
assignees: ''
---

## Summary

One sentence: what is broken?

## Environment

- nole version or commit hash (run `git rev-parse HEAD` in the repo, or use `--version` if available):
- OS and architecture (e.g. `darwin/arm64`, `linux/amd64`):
- Go version (`go version`):
- Client (Claude Code / Codex / OpenCode / Kimi / Cursor / OpenClaw / Hermes / CLI direct):

## Reproduction steps

1.
2.
3.

Does the issue reproduce on the deterministic offline benchmark (`nole bench --json`) or only on live providers? If live, which providers are configured?

## Expected behavior



## Actual behavior

Include the redacted error message and route trace (`--json` output). **Do not paste real API keys, authorization headers, or live provider response bodies.** Nólë's own redaction strips them; pasting around it defeats the purpose.

```
<paste redacted error / route_trace here>
```

## Additional context

Any relevant config, env-var settings (names only, never values), or recent changes.
