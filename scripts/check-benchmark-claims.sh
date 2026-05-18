#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "benchmark claims check failed: $*" >&2
  exit 1
}

[[ -f docs/BENCHMARKS.md ]] || fail "missing docs/BENCHMARKS.md"
[[ -f docs/ROUTE-EVIDENCE.md ]] || fail "missing docs/ROUTE-EVIDENCE.md"

grep -Fiq "does not measure live web quality" docs/BENCHMARKS.md \
  || fail "benchmark docs must state offline mode does not measure live web quality"
grep -Fiq "does not measure live web result quality" docs/ROUTE-EVIDENCE.md \
  || fail "route evidence summary must state offline mode does not measure live web result quality"
grep -Fq "Private data: none included" docs/ROUTE-EVIDENCE.md \
  || fail "route evidence summary must state that private data is not included"
grep -Fiq "raw artifact policy" docs/ROUTE-EVIDENCE.md \
  || fail "route evidence summary must document raw artifact policy"
grep -Fiq "Route matrix changes require evidence" docs/BENCHMARKS.md \
  || fail "benchmark docs must keep route matrix evidence rule"

# Benchmark/evidence docs should not make unsupported provider-ranking or speed claims.
# Keep this focused on benchmark artifacts so general product phrasing such as
# "make search better" does not create false positives.
unsupported_claim_re='(^|[^[:alnum:]_])(# ?1|fastest|faster[[:space:]]+than|cheapest|cheaper[[:space:]]+than|best[[:space:]]+provider|beats|outperforms|superior|guaranteed)([^[:alnum:]_]|$)'
if grep -Eiq "$unsupported_claim_re" docs/BENCHMARKS.md docs/ROUTE-EVIDENCE.md; then
  fail "benchmark/evidence docs contain unsupported ranking/speed language"
fi

echo "benchmark claims check passed"
