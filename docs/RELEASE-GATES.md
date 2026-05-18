# Private-prep release gates

Nólë is not publishing a public release from this repository yet. Public release, repo visibility changes, hosted deployment, and any paid-provider spend still require explicit owner approval.

This document defines the v0.1 private-prep gates used by CI and local maintainers.

## CI workflow

`.github/workflows/private-prep.yml` runs on pull requests and pushes to `main` with read-only repository permissions.

Jobs:

- `tests, docs, doctor, bench`
  - `./scripts/check-docs-framing.sh`
  - `./scripts/check-benchmark-claims.sh`
  - `go test ./...`
  - `go vet ./...`
  - `go run . doctor`
  - `go run . doctor --mcp`
  - `go run . bench --json`
  - `go run . bench --evidence-md`
  - `go run . providers --json`
  - `git diff --check`
- `public-safety secret scan`
  - `./scripts/secret-scan.sh`
  - Scans tracked text files for real-looking secrets, private keys, auth headers, and personal machine paths.
  - Allows documented environment variable names and obvious placeholders.
- `non-publishing build/checksums`
  - `./scripts/check-release-builds.sh`
  - Cross-compiles local release-shaped artifacts for Linux, macOS, and Windows on amd64/arm64.
  - Generates checksums only inside the workflow/temp directory.
  - Does not create tags, GitHub releases, uploaded release assets, or hosted deployments.

## Local gate

Run before merge when touching release, install, routing, MCP, or provider behavior:

```bash
./scripts/check-docs-framing.sh
./scripts/check-benchmark-claims.sh
go test ./...
go vet ./...
go run . doctor
go run . doctor --mcp
go run . bench --json
go run . bench --evidence-md
go run . providers --json
git diff --check
./scripts/secret-scan.sh
./scripts/check-release-builds.sh
```

## Safety invariants

- Nólë stays local/BYOK and default free-tier/BYOK-safe.
- MCP stdout remains protocol-clean.
- No secrets, tokens, auth headers, `.env` values, raw provider payloads, personal paths, or private URLs should appear in docs, fixtures, workflow logs, or reports.
- CI must not require real provider credentials.
- Build/checksum checks are private-prep validation only; they do not publish releases.
