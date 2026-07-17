#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -gt 1 ]; then
  printf 'usage: %s [repo-root]\n' "$0" >&2
  exit 2
fi

ROOT=${1:-"$(git rev-parse --show-toplevel)"}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

for required in go.mod README.md AGENTS.md .github/workflows/ci.yml .github/workflows/release.yml; do
  [ -f "$ROOT/$required" ] || fail "missing required file: $required"
done

GO_VERSION=$(awk '$1 == "go" { print $2; exit }' "$ROOT/go.mod")
if [[ ! "$GO_VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  fail "could not derive a valid Go version from the go directive in go.mod"
fi
check_readme_minimum() {
  local version_pattern=${GO_VERSION//./\\.}
  local exact_reference="\`go ${GO_VERSION}\`"

  if ! grep -E "^[[:space:]]*-[[:space:]]+Go[[:space:]]+${version_pattern}\\+[[:space:]]+for building from source" "$ROOT/README.md" \
    | grep -F "$exact_reference" \
    | grep -Fq '`go.mod`'; then
    fail "README.md must document a Go ${GO_VERSION}+ minimum for building from source and reference the exact go ${GO_VERSION} directive from go.mod"
  fi
}

check_agents_minimum() {
  local version_pattern=${GO_VERSION//./\\.}
  local exact_reference="\`go ${GO_VERSION}\`"

  if ! grep -E "[Ii]nstall[[:space:]]+Go[[:space:]]+${version_pattern}\\+([^0-9.]|$)" "$ROOT/AGENTS.md" \
    | grep -F "$exact_reference" \
    | grep -Fq '`go.mod`'; then
    fail "AGENTS.md must document a Go ${GO_VERSION}+ install minimum and reference the exact go ${GO_VERSION} directive from go.mod"
  fi
}

check_workflow_go_version() {
  local file=$1
  local declarations
  local matching
  local version_pattern=${GO_VERSION//./\\.}

  declarations=$(grep -Ec '^[[:space:]]*go-version(-file)?:' "$ROOT/$file" || true)
  matching=$(grep -Ec "^[[:space:]]*go-version:[[:space:]]*['\"]?${version_pattern}['\"]?[[:space:]]*(#.*)?$|^[[:space:]]*go-version-file:[[:space:]]*['\"]?go\\.mod['\"]?[[:space:]]*(#.*)?$" "$ROOT/$file" || true)

  if [ "$declarations" -eq 0 ]; then
    fail "$file must configure its Go version from go.mod or pin go ${GO_VERSION}"
  fi
  if [ "$declarations" -ne "$matching" ]; then
    fail "$file has a Go version that does not match the go ${GO_VERSION} directive in go.mod"
  fi
}

printf 'checking documented Go version requirements (go %s)\n' "$GO_VERSION"
check_readme_minimum
check_agents_minimum
check_workflow_go_version .github/workflows/ci.yml
check_workflow_go_version .github/workflows/release.yml
