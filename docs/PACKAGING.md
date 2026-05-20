# Packaging and distribution prep

This document describes non-publishing packaging prep for Nólë. It does not create tags, publish GitHub Releases, upload assets, publish packages, deploy endpoints or change repository visibility.

## Current packaging stance

- Primary artifact shape: Go single binary named `nole`.
- First public artifact channel, if approved later: GitHub Release binaries plus `SHA256SUMS`.
- Package managers are optional follow-ups after a published release exists.
- All publication actions are approval-gated.

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

v0.1 prep currently validates `SHA256SUMS`. Signing is not yet configured.

Before a public release, decide whether to add:

- signed checksum files;
- provenance/SLSA attestations;
- cosign signatures;
- GitHub artifact attestations.

Do not claim signed artifacts exist until the signing mechanism is implemented and verified.

## GitHub Release assets

A future approved release may upload:

- one binary per target in the artifact matrix;
- `SHA256SUMS`;
- optional signatures/attestations if implemented.

Asset upload requires separate approval from release tag creation and GitHub Release publication.

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

Approval-gated future flow:

1. Sync clean `main`.
2. Run full local gates.
3. Confirm latest `main` CI is green.
4. Run non-publishing dry-run build/checksums.
5. Confirm public release checklist.
6. Create approved tag.
7. Publish approved GitHub Release.
8. Upload approved assets.
9. Publish approved package channels, if any.
10. Verify published artifacts and checksums.

Steps 6-9 require explicit approval. Steps 1-5 are safe prep.
