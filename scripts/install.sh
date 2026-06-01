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
# INTEGRITY MODEL (two layers, only the first is mandatory):
#   1. SHA256 is the MANDATORY integrity floor. The installer downloads
#      SHA256SUMS, verifies the asset against it, and FAILS CLOSED on any
#      mismatch — it never installs an unverified binary. This needs only
#      curl-or-wget + sha256sum-or-shasum, so the zero-dependency path always
#      works.
#   2. GitHub build-provenance attestation (Sigstore-backed, via
#      `gh attestation verify`) is an ADDITIVE, best-effort second gate. It runs
#      only when a usable `gh` is present, FAILS CLOSED on a real verification
#      mismatch, and is SKIPPED with a clear log line otherwise (no gh, old gh,
#      offline, anonymous, or a pre-signing release) so the zero-dependency
#      curl|bash path still installs on SHA256 alone. See NOLE_INSTALL_VERIFY.
#
# Overrides (all optional):
#   NOLE_INSTALL_VERSION   pin a release tag (e.g. v0.10.0); default: latest
#   NOLE_INSTALL_DIR       install directory; default: $HOME/.local/bin
#   NOLE_INSTALL_REPO      owner/repo; default: dorukardahan/nole
#   NOLE_INSTALL_API_URL   releases API base; default: https://api.github.com
#   NOLE_INSTALL_DOWNLOAD_URL  asset download base; default: https://github.com
#   NOLE_INSTALL_VERIFY    auto|require|off  (default: auto)
#       auto     verify the attestation when a usable gh is present; soft-skip
#                (install on SHA256 alone) when it is not — the zero-dep default.
#       require  treat an unusable/absent verifier, or an unverifiable
#                attestation, as a hard error. For supply-chain-strict installs.
#       off      skip the attestation gate entirely (SHA256 still mandatory).

REPO="${NOLE_INSTALL_REPO:-dorukardahan/nole}"
API_URL="${NOLE_INSTALL_API_URL:-https://api.github.com}"
DOWNLOAD_URL="${NOLE_INSTALL_DOWNLOAD_URL:-https://github.com}"
INSTALL_DIR="${NOLE_INSTALL_DIR:-$HOME/.local/bin}"
VERIFY_MODE="${NOLE_INSTALL_VERIFY:-auto}"

# The first Nólë release whose assets carry a GitHub build-provenance
# attestation. install.sh is always fetched fresh from main, so this constant is
# current. For a resolved version >= SIGNED_SINCE, a MISSING attestation when the
# attestation API IS reachable is treated as tampering and FAILS CLOSED; older
# (pre-signing) versions, an unreachable API, or an absent verifier soft-skip.
SIGNED_SINCE="v0.10.0"

# Minimum gh free of CVE-2026-48501 (gh attestation leaked the host auth token to
# Sigstore TUF mirrors before 2.93.0). Older gh is treated as "no verifier" so the
# installer never invokes a token-leaking binary.
GH_MIN_VERSION="2.93.0"

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

# --- version comparison (pure shell, set -e safe) ---
# ver_ge A B  -> 0 (true) if version A >= B. Both are MAJOR.MINOR.PATCH; a leading
# 'v' and any -prerelease / +build metadata are stripped. A non-release-shaped
# value (dev build, garbage, empty) yields false so it never trips a fail-closed
# path — mirrors internal/selfupdate's parseRelease semantics.
ver_ge() {
  local a="$1" b="$2" a1 a2 a3 b1 b2 b3 seg
  a="${a#v}"; a="${a%%+*}"; a="${a%%-*}"
  b="${b#v}"; b="${b%%+*}"; b="${b%%-*}"
  # IFS=. splits on dots; `set -f` disables pathname expansion so a version that
  # contains a glob metachar (e.g. "v*.0.0") is NOT expanded against the cwd
  # before the digit check — without it `set -- $a` would glob.
  local IFS=. noglob_was_set=0
  case $- in *f*) noglob_was_set=1 ;; esac
  set -f
  # shellcheck disable=SC2086
  set -- $a; a1="${1:-}"; a2="${2:-}"; a3="${3:-}"
  # shellcheck disable=SC2086
  set -- $b; b1="${1:-}"; b2="${2:-}"; b3="${3:-}"
  [ "$noglob_was_set" -eq 1 ] || set +f
  unset IFS
  for seg in "$a1" "$a2" "$a3" "$b1" "$b2" "$b3"; do
    case "$seg" in ''|*[!0-9]*) return 1 ;; esac
  done
  if [ "$a1" -ne "$b1" ]; then
    if [ "$a1" -gt "$b1" ]; then return 0; else return 1; fi
  fi
  if [ "$a2" -ne "$b2" ]; then
    if [ "$a2" -gt "$b2" ]; then return 0; else return 1; fi
  fi
  if [ "$a3" -ge "$b3" ]; then return 0; else return 1; fi
}

# is_clean_release <version> -> 0 if it strips to exactly three numeric segments
# (a well-formed MAJOR.MINOR.PATCH, optionally v-prefixed / -pre / +build).
is_clean_release() {
  local core="$1" s noglob_was_set=0
  core="${core#v}"; core="${core%%+*}"; core="${core%%-*}"
  case "$core" in ''|*[!0-9.]*) return 1 ;; esac
  local IFS=.
  case $- in *f*) noglob_was_set=1 ;; esac
  set -f
  # shellcheck disable=SC2086
  set -- $core
  [ "$noglob_was_set" -eq 1 ] || set +f
  unset IFS
  [ "$#" -eq 3 ] || return 1
  for s in "$1" "$2" "$3"; do
    case "$s" in '') return 1 ;; esac
  done
  return 0
}

# looks_like_release_tag <version> -> 0 if it is SHAPED like a release tag
# (optional 'v' then a digit). Lets a MALFORMED release tag (e.g. "v0.10" served
# by an untrusted releases API on the unpinned latest path) be told apart from a
# genuine non-release ref ("dev", "main"): the former biases to fail-closed, the
# latter soft-skips.
looks_like_release_tag() {
  case "$1" in
    v[0-9]*|[0-9]*) return 0 ;;
    *) return 1 ;;
  esac
}

# version_is_signed <version>  -> 0 if the resolved release is at/after the
# signing cutover (SIGNED_SINCE), i.e. it is KNOWN to carry an attestation.
version_is_signed() { ver_ge "$1" "$SIGNED_SINCE"; }

# gh_version_ok -> 0 if the installed gh is >= GH_MIN_VERSION (CVE-2026-48501 fix).
gh_version_ok() {
  local out ver
  # Capture ALL of `gh --version` (it prints 2+ lines), then take the first line
  # via sed's `1s`. Do NOT pipe gh into `head -n1`: head closes the pipe after
  # line 1, gh's 2nd write then takes SIGPIPE, and under `set -o pipefail` that
  # would make this function flakily report "gh too old" (a real, timing-
  # dependent bug, not just a test artifact).
  out="$(gh --version 2>/dev/null)" || return 1
  # "gh version 2.93.0 (2026-04-01)" -> 2.93.0
  ver="$(printf '%s\n' "$out" | sed -n '1s/^gh version \([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')"
  [ -n "$ver" ] || return 1
  ver_ge "$ver" "$GH_MIN_VERSION"
}

# --- optional, additive attestation verification (GitHub build provenance) ---
# PRECONDITION: SHA256 has ALREADY passed before this runs. This is the second,
# OPTIONAL gate with a deliberate three-way fail taxonomy:
#   - verifier unusable (gh absent / too old / pre-CVE-fix) or VERIFY_MODE=off
#         -> soft skip (clear log), return 0   [protects the zero-dep floor]
#   - attestation fetched but INVALID, OR provably absent on a KNOWN-signed
#     version (API reachable)                  -> fail closed (die)  [tampering]
#   - API unreachable / anonymous / pre-signing release
#         -> soft skip (clear log), return 0   [can't-verify != tampering]
# In `require` mode every soft-skip becomes a hard error.
attest_verify() {
  # attest_verify <file> <asset-name> <resolved-version>
  local file="$1" asset="$2" version="$3"

  if [ "$VERIFY_MODE" = "off" ]; then
    log "attestation check disabled (NOLE_INSTALL_VERIFY=off) — SHA256 already verified"
    return 0
  fi

  # Capability probe: need gh, the `attestation verify` subcommand, and a gh new
  # enough to be free of CVE-2026-48501 (token leak to TUF mirrors).
  local reason=""
  if ! have gh; then
    reason="gh not installed"
  elif ! gh attestation verify --help >/dev/null 2>&1; then
    reason="installed gh lacks 'attestation verify'"
  elif ! gh_version_ok; then
    reason="gh < ${GH_MIN_VERSION} (CVE-2026-48501)"
  fi
  if [ -n "$reason" ]; then
    if [ "$VERIFY_MODE" = "require" ]; then
      die "NOLE_INSTALL_VERIFY=require but no usable attestation verifier: ${reason}. Install GitHub CLI gh >= ${GH_MIN_VERSION}, or unset NOLE_INSTALL_VERIFY."
    fi
    log "signature verifier unavailable (${reason}) — skipping attestation check (SHA256 already verified)"
    return 0
  fi

  # gh is present and adequate. Verify the downloaded binary against the repo's
  # build-provenance attestation, hardened to the exact release-workflow identity.
  # install.sh passes NO token of its own; gh uses whatever auth the host already
  # carries. An anonymous host hits the public-repo auth limit (cli/cli #11803) and
  # lands in the 'unreachable' soft-skip branch below — NOT a failure. Installing
  # from a fork (NOLE_INSTALL_REPO) requires that fork to carry its own attestation
  # signed by its own release.yml, else this fails closed (the secure outcome).
  #
  # NOTE: keep this assignment SEPARATE from `local`. `local out=$(...)` would make
  # the `local` builtin's exit (always 0) MASK gh's real exit, silently passing an
  # INVALID attestation. The if/else capture reads gh's true exit under `set -e`
  # (a bare `out=$(gh ...)` would instead ABORT the script on any non-zero gh exit,
  # turning every intended soft-skip into a brick).
  local out rc
  if out="$(gh attestation verify "$file" --repo "$REPO" \
             --signer-workflow "${REPO}/.github/workflows/release.yml" 2>&1)"; then
    rc=0
  else
    rc=$?
  fi
  if [ "$rc" -eq 0 ]; then
    log "attestation verified (build provenance, ${REPO})"
    return 0
  fi

  # rc != 0. Two outcomes matter, and the SIGNED_SINCE version floor — not fragile
  # message parsing — decides the security-relevant case:
  #   - We could NOT verify (offline / anonymous / API unreachable): NOT evidence
  #     of tampering -> soft-skip (or die in require mode).
  #   - We COULD reach the API and the artifact did NOT verify (invalid signature,
  #     identity mismatch, OR no attestation for the digest): on a KNOWN-signed
  #     version (>= SIGNED_SINCE) that is tampering -> fail closed; on a pre-signing
  #     version there is nothing to verify -> soft-skip.
  # Only the unreachable/auth set is matched by substring, and it is kept
  # CONSERVATIVE on purpose: a missed pattern falls through to the (safe)
  # fail-closed-on-signed arm rather than soft-skipping a genuine verification
  # failure. This bounds the message-fragility risk to "an offline install of a
  # signed version might fail closed" (annoying, safe) and never to "a tampered
  # signed binary is soft-skipped" (insecure).
  case "$out" in
    *"HTTP 401"*|*"HTTP 403"*|*"authentication"*|*"Unauthorized"*|*"log in"*|*"gh auth"*|\
    *"rate limit"*|*"connection refused"*|*"no such host"*|*"i/o timeout"*|*"deadline exceeded"*|\
    *"dial tcp"*|*"lookup "*|*"network is unreachable"*|*"no route to host"*|*"TLS handshake"*|*"server misbehaving"*)
      if [ "$VERIFY_MODE" = "require" ]; then
        die "NOLE_INSTALL_VERIFY=require: could not reach/authenticate to the attestation API to verify ${asset} — ${out}"
      fi
      log "attestation API unreachable/unauthenticated — skipping attestation check (SHA256 already verified)"
      return 0
      ;;
    *)
      # Reachable, but the artifact did NOT verify. The SIGNED_SINCE floor decides
      # whether that is tampering or an expected pre-signing release:
      if is_clean_release "$version"; then
        if version_is_signed "$version"; then
          die "attestation verification FAILED for ${asset} (${version}) — refusing to install (possible tampering; set NOLE_INSTALL_VERIFY=off to override): ${out}"
        fi
        # A well-formed release BELOW the cutover -> genuinely pre-signing.
      elif looks_like_release_tag "$version"; then
        # Version-SHAPED but MALFORMED (e.g. an attacker-served "v0.10" on the
        # unpinned latest path, where the releases API is the same channel that
        # served the asset). We cannot confirm it predates signing, so bias to
        # fail-closed rather than soft-skipping a possibly-tampered artifact.
        die "attestation verification FAILED for ${asset}: malformed release tag '${version}' could not be confirmed pre-signing — refusing to install (set NOLE_INSTALL_VERIFY=off to override): ${out}"
      fi
      # Pre-signing clean release, or a non-release ref (dev/branch) -> soft-skip.
      if [ "$VERIFY_MODE" = "require" ]; then
        die "NOLE_INSTALL_VERIFY=require but ${asset} (${version}) has no verifiable attestation (predates signing or is not a release tag)"
      fi
      log "no verifiable attestation for ${version} (pre-signing release) — skipping attestation check (SHA256 already verified)"
      return 0
      ;;
  esac
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
  # grep -m1 stops after the first match itself (no `| head -n1` that would SIGPIPE
  # grep under `set -o pipefail`); sed's `1s` then keeps only the first line.
  tag="$(printf '%s' "$json" | grep -o -m1 '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed -E '1s/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/')"
  [ -n "$tag" ] || die "could not parse a release tag from the API response"
  printf '%s' "$tag"
}

main() {
  local os arch asset version base
  # Reject an unknown NOLE_INSTALL_VERIFY early. Without this, a typo such as
  # `required` or `REQUIRE` would fall through to `auto` semantics and silently
  # weaken the very policy the user was trying to strengthen — fail loud instead.
  case "$VERIFY_MODE" in
    auto|require|off) : ;;
    *) die "invalid NOLE_INSTALL_VERIFY='${VERIFY_MODE}' (expected one of: auto, require, off)" ;;
  esac
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

  # ADDITIVE second gate. SHA256 (mandatory) already passed; attestation is
  # optional and runs BEFORE the stage+atomic-rename, so a verification failure
  # leaves any existing install untouched (same guarantee as the checksum path).
  attest_verify "${TMP_DIR}/${asset}" "${asset}" "${version}"

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
