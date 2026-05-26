Nólë is a local/BYOK web search and extraction router for AI agents. It is not a hosted search SaaS, and it does not proxy your provider keys through a Nólë service.

This release publishes GitHub Release binaries and checksums for the tagged commit. Provider keys, local env files and optional local runtimes such as Scrapling remain user-controlled.

## Install from assets

Download the binary for your platform from the assets below, verify it with `SHA256SUMS`, put it on `PATH`, then run:

```bash
nole doctor
nole doctor --mcp
```

## Safety notes

- Default provider policy is `free-first`.
- Premium-capable providers require explicit opt-in.
- Scrapling support is an optional local runtime integration. Nólë does not vendor or redistribute Scrapling.
- Deterministic benchmark results validate routing contracts. They do not rank live web providers.
