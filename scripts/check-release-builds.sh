#!/usr/bin/env bash
set -euo pipefail

# Build release-shaped artifacts for local validation or the release workflow.
# This script does not create GitHub releases, tags, or uploads by itself.

ROOT=$(git rev-parse --show-toplevel)
OUT_DIR=${NOLE_BUILD_OUT:-"$(mktemp -d)"}
VERSION=${NOLE_BUILD_VERSION:-"dev-build-check"}
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

mkdir -p "$OUT_DIR"
checksum_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

printf 'non-publishing cross-platform build check\n'
printf 'output_dir=%s\n' "$OUT_DIR"

for target in "${TARGETS[@]}"; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  suffix=""
  if [ "$GOOS" = "windows" ]; then
    suffix=".exe"
  fi
  name="nole-${GOOS}-${GOARCH}${suffix}"
  printf 'building %s\n' "$name"
  (
    cd "$ROOT"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/dorukardahan/nole/internal/version.Version=${VERSION}" \
      -o "$OUT_DIR/$name" .
  )
  test -s "$OUT_DIR/$name"
done

(
  cd "$OUT_DIR"
  checksum_cmd nole-* > SHA256SUMS
)

test -s "$OUT_DIR/SHA256SUMS"
printf 'checksums:\n'
sed 's#  .*#  [artifact]#' "$OUT_DIR/SHA256SUMS"
