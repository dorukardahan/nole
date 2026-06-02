#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CI_MODE=0
REQUIRE_CLAWPATCH="${REQUIRE_CLAWPATCH:-0}"

for arg in "$@"; do
  case "$arg" in
    --ci) CI_MODE=1 ;;
    --clawpatch) REQUIRE_CLAWPATCH=1 ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

run() {
  echo "+ $*"
  "$@"
}

gofmt_files="$(gofmt -l $(find . -name '*.go' -not -path './.git/*'))"
if [ -n "$gofmt_files" ]; then
  echo "gofmt needed:" >&2
  echo "$gofmt_files" >&2
  exit 1
fi

# install.sh downloads release assets over the network, so it cannot run
# end-to-end in CI; lint its syntax here (a functional test exercises it against
# an httptest server in `go test`).
run bash -n scripts/install.sh

# install.ps1 is the Windows installer. Parse-check it with pwsh when available
# (GitHub-hosted Linux runners have pwsh preinstalled; a local macOS dev host may
# not). The parse-only check needs no Windows and no extra PowerShell module.
if command -v pwsh >/dev/null 2>&1; then
  run pwsh -NoProfile -NonInteractive -Command '
    $errs = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path scripts/install.ps1).Path, [ref]$null, [ref]$errs)
    if ($errs) { $errs | ForEach-Object { Write-Error $_.ToString() }; exit 1 }
    Write-Output "install.ps1 parse OK"
  '
else
  echo "pwsh not found; skipping install.ps1 parse check (CI Linux runners have pwsh)"
fi

# The Homebrew formula template is the source of truth the release workflow renders
# and pushes to the tap. Render it with a dummy version + dummy hashes and run
# `brew style` (which enforces FormulaAudit/ComponentsOrder) when brew is on PATH —
# macOS dev hosts have it; GitHub's Linux CI runners generally do NOT, so this is a
# local-dev gate that skips on CI — catching a stanza-order/style regression before
# it can ship a broken formula. (`brew audit` by path is disabled in recent brew;
# `brew style` is the available-by-path lint and catches the ordering.)
if command -v brew >/dev/null 2>&1; then
  hb_tap="$(mktemp -d)"
  mkdir -p "$hb_tap/Formula"
  hb_dummy="$(printf '0%.0s' $(seq 1 64))"
  sed -e "s/__VERSION__/0.0.0/g" \
      -e "s/__SHA_DARWIN_ARM64__/${hb_dummy}/g" -e "s/__SHA_DARWIN_AMD64__/${hb_dummy}/g" \
      -e "s/__SHA_LINUX_ARM64__/${hb_dummy}/g"  -e "s/__SHA_LINUX_AMD64__/${hb_dummy}/g" \
      packaging/homebrew/nole.rb.tmpl > "$hb_tap/Formula/nole.rb"
  if grep -q '__' "$hb_tap/Formula/nole.rb"; then
    echo "rendered Homebrew formula still contains __ placeholders" >&2
    rm -rf "$hb_tap"
    exit 1
  fi
  HOMEBREW_NO_AUTO_UPDATE=1 run brew style "$hb_tap/Formula/nole.rb"
  rm -rf "$hb_tap"
else
  echo "brew not found; skipping Homebrew formula style check (local-dev gate — run it on a host with brew)"
fi

# The Scoop manifest template is the source of truth the release workflow renders
# and pushes to the bucket. Render it with a dummy version + dummy hashes, assert no
# placeholders survive, then validate JSON shape with jq (no Windows host needed — the
# .exe is never executed; we only check the manifest is well-formed + schema-shaped).
if command -v jq >/dev/null 2>&1; then
  sc_dir="$(mktemp -d)"
  sc_dummy="$(printf '0%.0s' $(seq 1 64))"
  sed -e "s/__VERSION__/0.0.0/g" \
      -e "s/__SHA_WIN_AMD64__/${sc_dummy}/g" \
      -e "s/__SHA_WIN_ARM64__/${sc_dummy}/g" \
      packaging/scoop/nole.json.tmpl > "$sc_dir/nole.json"
  if grep -q '__' "$sc_dir/nole.json"; then
    echo "rendered Scoop manifest still contains __ placeholders" >&2
    rm -rf "$sc_dir"
    exit 1
  fi
  # (1) well-formed JSON  (2) required top-level fields (schema 'required' =
  # version, homepage, license)  (3) both arch keys present  (4) every top-level
  # arch hash is a bare 64-hex sha256 (no prefix) so it matches SHA256SUMS  (5) the
  # arch URLs point at the windows .exe assets.
  run jq -e '
    (.version and .homepage and .license)
    and (.architecture["64bit"] and .architecture["arm64"])
    and ([.architecture[].hash] | all(test("^[a-f0-9]{64}$")))
    and ([.architecture[].url]  | all(test("nole-windows-(amd64|arm64)\\.exe$")))
  ' "$sc_dir/nole.json" >/dev/null
  rm -rf "$sc_dir"
  echo "Scoop manifest render + shape OK"
else
  echo "jq not found; skipping Scoop manifest shape check"
fi

run ./scripts/check-docs-framing.sh
run ./scripts/check-benchmark-claims.sh
run ./scripts/check-integration-evidence.sh
run go test ./...
run go vet ./...
run go run . doctor
run go run . doctor --mcp
run go run . bench --json
run go run . bench --evidence-md
run go run . providers --json
run go run . config dump
run go run . config dump --json
run go run . doctor --json

tmp_evidence="$(mktemp)"
trap 'rm -f "$tmp_evidence"' EXIT
./scripts/verify-integration-evidence.sh > "$tmp_evidence"
run ./scripts/check-integration-evidence.sh "$tmp_evidence"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  if [ "$CI_MODE" -eq 1 ]; then
    if [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ] && [ -n "${GITHUB_BASE_REF:-}" ] && git rev-parse "origin/${GITHUB_BASE_REF}" >/dev/null 2>&1; then
      run git diff --check "origin/${GITHUB_BASE_REF}...HEAD"
    elif git rev-parse --verify HEAD^ >/dev/null 2>&1; then
      run git show --check --format=fuller --no-renames HEAD
    else
      run git diff --check
    fi
  else
    run git diff --check
  fi
fi

CLAWPATCH_BIN="${CLAWPATCH_BIN:-/tmp/clawpatch-main/dist/cli.js}"
if [ "$REQUIRE_CLAWPATCH" = "1" ]; then
  if [ ! -f "$CLAWPATCH_BIN" ]; then
    echo "clawpatch binary not found: $CLAWPATCH_BIN" >&2
    exit 1
  fi
  STATE_DIR="${CLAWPATCH_STATE_DIR:-/tmp/nole-clawpatch-smoke-state}"
  MAP_OUT="${CLAWPATCH_MAP_OUT:-/tmp/nole-clawpatch-smoke-map.json}"
  STATUS_OUT="${CLAWPATCH_STATUS_OUT:-/tmp/nole-clawpatch-smoke-status.json}"
  rm -rf "$STATE_DIR"
  run node "$CLAWPATCH_BIN" --root "$ROOT" --state-dir "$STATE_DIR" --no-color --no-input init
  echo "+ node $CLAWPATCH_BIN --root $ROOT --state-dir $STATE_DIR --no-color --no-input map --source heuristic --json > $MAP_OUT"
  node "$CLAWPATCH_BIN" --root "$ROOT" --state-dir "$STATE_DIR" --no-color --no-input map --source heuristic --json > "$MAP_OUT"
  echo "+ node $CLAWPATCH_BIN --root $ROOT --state-dir $STATE_DIR --no-color --no-input status --json > $STATUS_OUT"
  node "$CLAWPATCH_BIN" --root "$ROOT" --state-dir "$STATE_DIR" --no-color --no-input status --json > "$STATUS_OUT"
  echo "clawpatch smoke outputs: $MAP_OUT $STATUS_OUT"
else
  echo "clawpatch smoke skipped; pass --clawpatch or REQUIRE_CLAWPATCH=1 to enable"
fi
