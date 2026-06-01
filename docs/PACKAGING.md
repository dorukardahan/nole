# Packaging and distribution prep

This document describes packaging and release automation for Nólë. Reading or
editing it does not create tags, publish GitHub Releases, upload assets, publish
packages or deploy endpoints.

## Current packaging stance

- Primary artifact shape: Go single binary named `nole`.
- First public artifact channel: GitHub Release binaries plus `SHA256SUMS`.
- Package managers are optional follow-ups after a published release exists.
- GitHub Releases are published automatically from approved semantic version
  tags by `.github/workflows/release.yml`.
- Package-manager publication actions remain separately approval-gated.

## Non-publishing dry-run

The repository includes a non-publishing build/checksum script:

```bash
./scripts/check-release-builds.sh
```

Behavior:

- cross-compiles release-shaped artifacts for Linux, macOS and Windows on amd64/arm64;
- writes artifacts to a temporary directory by default;
- supports `NOLE_BUILD_OUT` for a local output directory;
- supports `NOLE_BUILD_VERSION` for build metadata;
- generates `SHA256SUMS` in the output directory;
- prints checksum lines with artifact paths redacted as `[artifact]`;
- does not call GitHub release APIs;
- does not upload files;
- does not publish package registries.

Example local dry-run:

```bash
out=$(mktemp -d)
NOLE_BUILD_OUT="$out" NOLE_BUILD_VERSION="v0.1.0-dry-run" ./scripts/check-release-builds.sh
find "$out" -maxdepth 1 -type f -name 'nole-*' -o -name SHA256SUMS
rm -rf "$out"
```

Keep generated files out of git unless a future release process explicitly approves an artifact manifest.

## Artifact matrix

Default dry-run target matrix:

| OS | Arch | Artifact name |
| --- | --- | --- |
| linux | amd64 | `nole-linux-amd64` |
| linux | arm64 | `nole-linux-arm64` |
| darwin | amd64 | `nole-darwin-amd64` |
| darwin | arm64 | `nole-darwin-arm64` |
| windows | amd64 | `nole-windows-amd64.exe` |
| windows | arm64 | `nole-windows-arm64.exe` |

If this matrix changes, update `scripts/check-release-builds.sh`, CI and this document together.

## Checksums and signing

Two integrity layers, only the first is mandatory:

- **`SHA256SUMS` is the mandatory integrity floor.** Every release ships it; the
  installers verify the downloaded asset against it and fail closed on any
  mismatch. This needs only `sha256sum`/`shasum` (or PowerShell `Get-FileHash`),
  so it works on the zero-dependency path with no extra tooling.
- **GitHub build-provenance attestations are an additive second factor**
  (since v0.10.0). `release.yml` generates keyless, Sigstore-backed attestations
  via `actions/attest-build-provenance` (OIDC identity, no repo Secret) for both
  the per-binary artifacts and the `SHA256SUMS` file. Attestations are resolved by
  artifact digest through the GitHub attestation API by `gh attestation verify` —
  they are **NOT uploaded as release assets**.

Verification contract (`install.sh` today; the same model is planned for the
forthcoming `install.ps1` and `nole self-update`):

- SHA256 is checked first and always; a mismatch fails closed.
- The attestation is then verified **only when a usable `gh` is present**
  (`gh >= 2.93.0`, which carries the CVE-2026-48501 token-leak fix), hardened to
  the exact release-workflow signer identity. Absence of `gh` is a graceful skip
  with a clear log line — the zero-dependency curl|bash path still installs.
- A genuine attestation mismatch fails closed; a missing attestation on a
  KNOWN-signed release (resolved version `>= SIGNED_SINCE`, currently `v0.10.0`)
  while the API is reachable also fails closed (downgrade defense); a missing
  attestation on a pre-signing release, or an unreachable/anonymous API,
  soft-skips.
- `NOLE_INSTALL_VERIFY=auto|require|off` (default `auto`) controls the optional
  gate. `require` turns every soft-skip into a hard error (for supply-chain-strict
  installs); `off` skips the attestation entirely (SHA256 still mandatory).

State precisely that SHA256 is mandatory and the attestation is additive /
best-effort. Do not imply the attestation closes a full on-path MITM in `auto`
mode (an attacker who replaces both the binary and `SHA256SUMS` and strips the
attestation is only stopped in `require` mode, or for versions `>= SIGNED_SINCE`
when the API is reachable).

## Install script (`scripts/install.sh`)

`scripts/install.sh` is the user-facing installer for the published release
binaries. It must stay in sync with the artifact matrix above:

- detects OS via `uname -s` (Linux/Darwin; Windows is rejected with a manual
  `.exe` download message) and arch via `uname -m` (amd64/arm64), resolving the
  asset name `nole-<os>-<arch>` exactly as `check-release-builds.sh` produces it;
- resolves the latest release tag from the GitHub API (or honours
  `NOLE_INSTALL_VERSION`), downloads the asset and `SHA256SUMS` into a temp dir;
- **verifies the SHA256 checksum BEFORE installing and fails closed on any
  mismatch** — SHA256 is the mandatory integrity floor, so the installer never
  installs an unverified binary;
- then runs the **additive, optional** `gh attestation verify` gate described in
  "Checksums and signing" above (soft-skips when `gh` is absent so the
  zero-dependency path is preserved; fails closed on a real mismatch);
- installs to `~/.local/bin` (overridable via `NOLE_INSTALL_DIR`) via
  stage-in-place + atomic `rename(2)` (Apple-Silicon-safe — `rename` never writes
  into the existing signed Mach-O inode — and non-destructive: the old binary
  survives a failed install), touching no secrets and sending no telemetry.

It hits the network, so CI lints it with `bash -n` (in `audit.sh`) rather than
running it end-to-end; a `go test` harness exercises the download + checksum +
install path against an httptest server. When the artifact matrix changes, update
the OS/arch mapping here and in the script together.

## GitHub Release assets

An approved tag-triggered release uploads:

- one binary per target in the artifact matrix;
- `SHA256SUMS`.

It also generates build-provenance attestations (via
`actions/attest-build-provenance`, keyless OIDC) for the binaries and for
`SHA256SUMS`. These are resolved at verify time through the GitHub attestation
API — they are **not** uploaded as release assets.

Asset upload happens inside the GitHub Release workflow for approved semantic
version tags. Do not push a tag until the release version and changelog scope are
accepted.

## Automated GitHub Release workflow

`.github/workflows/release.yml` runs when a tag matching `v*.*.*` is pushed, or
when a maintainer manually dispatches it for an existing tag.

The workflow:

1. verifies the tag looks like `vMAJOR.MINOR.PATCH` or a prerelease variant;
2. checks out the exact tag;
3. runs `scripts/audit.sh --ci`;
4. runs `scripts/secret-scan.sh`;
5. builds the Linux, macOS and Windows binary matrix with
   `scripts/check-release-builds.sh`;
6. generates build-provenance attestations for the binaries and `SHA256SUMS`
   (keyless OIDC via `actions/attest-build-provenance`; no repo Secret added);
7. creates a GitHub Release with the assets, `SHA256SUMS`, the standard release
   preamble and GitHub-generated release notes.

It uses only GitHub-hosted runner tooling, official checkout/setup-go/attest
actions and the GitHub CLI with `GITHUB_TOKEN`. The attest step needs the added
`id-token: write` and `attestations: write` permissions; signing is keyless
(Sigstore/OIDC), so no signing key or repo Secret is configured.

## Homebrew

Homebrew is a future optional channel.

Safe prep before publication:

- draft formula metadata locally;
- point to an approved GitHub Release URL only after it exists;
- verify checksum from the published asset;
- test install locally when possible.

Do not publish or submit a formula without explicit Homebrew publication approval.

## Scoop

Scoop is a future optional Windows channel.

Safe prep before publication:

- draft a manifest only after Windows release assets exist;
- use published asset URLs and SHA256 values;
- test install in a Windows environment if possible.

Do not publish a manifest without explicit Scoop publication approval.

## npm wrapper

An npm package would be a convenience wrapper, not the core v0.1 artifact.

Do not publish npm unless there is a clear wrapper design for:

- downloading or locating the binary;
- platform detection;
- checksum verification;
- no hidden telemetry;
- no hosted proxy behavior.

npm publication requires explicit approval.

## Docker

Docker is optional and should not imply Nólë is a hosted SaaS.

Safe prep before publication:

- keep default bind addresses local;
- document that HTTP/REST is experimental;
- avoid baking provider keys into images;
- use runtime environment variables or secrets managers only.

Do not publish images without explicit Docker publication approval.

## Release workflow outline

Approval-gated flow:

1. Sync clean `main`.
2. Run full local gates.
3. Confirm latest `main` CI is green.
4. Run non-publishing dry-run build/checksums.
5. Confirm public release checklist.
6. Create and push the approved semantic version tag.
7. Let the release workflow publish the GitHub Release and assets.
8. Verify published artifacts and checksums.
9. Publish approved package channels, if any.

Steps 6 and 9 require explicit approval. Steps 1-5 are safe prep.
