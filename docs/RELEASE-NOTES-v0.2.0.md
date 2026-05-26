# Nólë v0.2.0 release notes

Nólë v0.2.0 is the first published GitHub Release line for the public
repository. It keeps Nólë local/BYOK-first and adds safer agent setup,
extraction fallback and release automation.

## Highlights

- Native `nole setup --hermes` support for Hermes Agent MCP config.
- Optional local Scrapling extraction fallback through
  `NOLE_SCRAPLING_PYTHON`.
- Tag-triggered GitHub Release automation with Linux, macOS and Windows
  binaries plus `SHA256SUMS`.
- Public-repo release docs, packaging guidance and GitHub About metadata
  aligned with the current product framing.
- Existing CI gates, secret scan, govulncheck and release-shaped build checks
  preserved.

## Safety notes

- Nólë does not vendor or redistribute Scrapling. Users install and operate
  their own Python environment.
- Default provider policy remains `free-first`.
- Premium-capable providers require explicit opt-in.
- Deterministic benchmark output validates routing contracts. It does not rank
  live web providers.

## Install

Download the matching binary from the GitHub Release assets, verify it with
`SHA256SUMS`, put it on `PATH`, then run:

```bash
nole doctor
nole doctor --mcp
```

For agent setup:

```bash
nole setup --claude
nole setup --codex
nole setup --hermes
nole setup --opencode
```

Run `nole setup --help` for the full client list.
