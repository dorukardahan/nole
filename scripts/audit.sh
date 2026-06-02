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
