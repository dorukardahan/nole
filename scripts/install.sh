#!/usr/bin/env bash
set -euo pipefail

# Nólë installer: download a prebuilt release binary, verify its SHA256 checksum,
# and install it to ~/.local/bin. It is read-only toward your environment: it
# touches NO secrets, sends NO telemetry, and only writes the single binary.
#
# Recommended (supply-chain-cautious) usage — download, inspect, then run:
#
#   curl -fsSLO https://raw.githubusercontent.com/dorukardahan/nole/main/scripts/install.sh
#   less install.sh        # read it first
#   bash install.sh
#
# Pipe-to-bash also works if you trust the source:
#
#   curl -fsSL https://raw.githubusercontent.com/dorukardahan/nole/main/scripts/install.sh | bash
#
# SHA256 is the ONLY integrity check today (the release assets are not signed),
# and the installer fails closed on any checksum mismatch.
#
# Overrides (all optional):
#   NOLE_INSTALL_VERSION   pin a release tag (e.g. v0.9.0); default: latest
#   NOLE_INSTALL_DIR       install directory; default: $HOME/.local/bin
#   NOLE_INSTALL_REPO      owner/repo; default: dorukardahan/nole
#   NOLE_INSTALL_API_URL   releases API base; default: https://api.github.com
#   NOLE_INSTALL_DOWNLOAD_URL  asset download base; default: https://github.com

REPO="${NOLE_INSTALL_REPO:-dorukardahan/nole}"
API_URL="${NOLE_INSTALL_API_URL:-https://api.github.com}"
DOWNLOAD_URL="${NOLE_INSTALL_DOWNLOAD_URL:-https://github.com}"
INSTALL_DIR="${NOLE_INSTALL_DIR:-$HOME/.local/bin}"

# Temp dir, cleaned on exit. TMP_DIR is GLOBAL so the EXIT trap can still see it
# after main() returns — a function-local would be unbound under `set -u` and the
# trap would fail spuriously even on a successful install.
TMP_DIR=""
STAGED=""
cleanup() {
  if [ -n "${TMP_DIR:-}" ]; then rm -rf "$TMP_DIR"; fi
  # STAGED is removed here only if the atomic rename never consumed it (i.e. an
  # install failure); on success it has already been renamed to its final name.
  if [ -n "${STAGED:-}" ]; then rm -f "$STAGED"; fi
}
trap cleanup EXIT

log()  { printf 'nole-install: %s\n' "$*"; }
die()  { printf 'nole-install: error: %s\n' "$*" >&2; exit 1; }

# --- detect OS/arch -> release asset name (matches scripts/check-release-builds.sh) ---
detect_os() {
  local uname_s
  uname_s="$(uname -s)"
  case "$uname_s" in
    Linux)  printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
      die "Windows is not supported by this bash installer. Download the nole-windows-<arch>.exe asset from https://github.com/${REPO}/releases and place it on your PATH." ;;
    *)
      die "unsupported OS '${uname_s}'. Download a binary manually from https://github.com/${REPO}/releases." ;;
  esac
}

detect_arch() {
  local uname_m
  uname_m="$(uname -m)"
  case "$uname_m" in
    x86_64|amd64)  printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *)
      die "unsupported architecture '${uname_m}'. Download a binary manually from https://github.com/${REPO}/releases." ;;
  esac
}

# --- download helper (curl or wget) ---
have() { command -v "$1" >/dev/null 2>&1; }

fetch() {
  # fetch <url> <output-file>
  local url="$1" out="$2"
  if have curl; then
    curl -fsSL "$url" -o "$out"
  elif have wget; then
    wget -qO "$out" "$url"
  else
    die "need curl or wget to download release assets"
  fi
}

fetch_stdout() {
  # fetch_stdout <url>  -> stdout
  local url="$1"
  if have curl; then
    curl -fsSL "$url"
  elif have wget; then
    wget -qO- "$url"
  else
    die "need curl or wget to query the releases API"
  fi
}

# --- checksum helper (sha256sum or shasum -a 256), matching check-release-builds.sh ---
checksum_check() {
  # checksum_check <sums-file>  (run from inside the directory holding the asset)
  if have sha256sum; then
    sha256sum -c "$1"
  elif have shasum; then
    shasum -a 256 -c "$1"
  else
    die "need sha256sum or shasum to verify the download"
  fi
}

resolve_version() {
  if [ -n "${NOLE_INSTALL_VERSION:-}" ]; then
    printf '%s' "$NOLE_INSTALL_VERSION"
    return
  fi
  # Parse tag_name from the releases/latest JSON without jq (zero-dep).
  local json tag
  json="$(fetch_stdout "${API_URL%/}/repos/${REPO}/releases/latest")" \
    || die "could not query the latest release (are you online?)"
  tag="$(printf '%s' "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/')"
  [ -n "$tag" ] || die "could not parse a release tag from the API response"
  printf '%s' "$tag"
}

main() {
  local os arch asset version base
  os="$(detect_os)"
  arch="$(detect_arch)"
  asset="nole-${os}-${arch}"
  version="$(resolve_version)"
  base="${DOWNLOAD_URL%/}/${REPO}/releases/download/${version}"

  log "installing ${asset} ${version}"

  TMP_DIR="$(mktemp -d)" # cleaned by the EXIT trap

  fetch "${base}/${asset}" "${TMP_DIR}/${asset}" \
    || die "could not download ${asset} for ${version}"
  fetch "${base}/SHA256SUMS" "${TMP_DIR}/SHA256SUMS" \
    || die "could not download SHA256SUMS for ${version}"

  # SHA256SUMS uses relative filenames ("<hash>  nole-<os>-<arch>", two spaces),
  # so verification must run from inside the temp dir. Filter to exactly our
  # asset's line (two-space separator, anchored to end) before checking, so an
  # unrelated/extra asset can never satisfy the check.
  grep -E "  ${asset}\$" "${TMP_DIR}/SHA256SUMS" > "${TMP_DIR}/expected.sums" \
    || die "SHA256SUMS has no entry for ${asset}"
  ( cd "$TMP_DIR" && checksum_check expected.sums >/dev/null ) \
    || die "checksum verification FAILED for ${asset} — refusing to install"
  log "checksum verified"

  mkdir -p "$INSTALL_DIR"
  # Install via stage-in-place + atomic rename, NOT rm-first. rename(2) replaces
  # the directory entry without writing into the existing binary's inode, so it is
  # Apple-Silicon-safe (the SIGKILL gotcha is about `cp` writing over a mapped,
  # signed Mach-O — which never happens here) AND never leaves the user with no
  # binary: the old one stays until the rename succeeds, and any failure (ENOSPC,
  # unwritable dir) leaves it untouched. Staging inside INSTALL_DIR keeps the
  # final step a same-filesystem (atomic) rename.
  STAGED="${INSTALL_DIR}/.nole.install.$$"
  cp "${TMP_DIR}/${asset}" "$STAGED" \
    || die "could not stage the new binary in ${INSTALL_DIR} (existing install left untouched)"
  chmod +x "$STAGED"
  mv -f "$STAGED" "${INSTALL_DIR}/nole" \
    || die "could not move the new binary into place (existing install left untouched)"
  log "installed to ${INSTALL_DIR}/nole"

  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) : ;;
    *) log "note: ${INSTALL_DIR} is not on your PATH — add: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
  esac

  log "Nólë works with ZERO keys: keyless DDGS web search out of the box (run 'nole setup --local-extract' to add keyless local URL extraction). Provider keys are optional and only unlock higher-quality/extract routes."
  log "next: run 'nole doctor' to verify the install."
}

main "$@"
