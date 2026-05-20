#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "benchmark claims check failed: $*" >&2
  exit 1
}

[[ -f docs/BENCHMARKS.md ]] || fail "missing docs/BENCHMARKS.md"
[[ -f docs/ROUTE-EVIDENCE.md ]] || fail "missing docs/ROUTE-EVIDENCE.md"
[[ -f docs/LIVE-BENCHMARK-PLAN.md ]] || fail "missing docs/LIVE-BENCHMARK-PLAN.md"
[[ -f docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md ]] || fail "missing docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md"

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
grep -Fq "docs/LIVE-BENCHMARK-PLAN.md" docs/BENCHMARKS.md \
  || fail "benchmark docs must link the controlled live benchmark plan"
grep -Fq "docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md" docs/BENCHMARKS.md \
  || fail "benchmark docs must link the live benchmark summary template"
grep -Fiq "explicit maintainer approval" docs/LIVE-BENCHMARK-PLAN.md \
  || fail "live benchmark plan must require explicit maintainer approval"
grep -Fiq "provider-key inventory" docs/LIVE-BENCHMARK-PLAN.md \
  || fail "live benchmark plan must document provider-key inventory rules"
grep -Fiq "Stop conditions" docs/LIVE-BENCHMARK-PLAN.md \
  || fail "live benchmark plan must document stop conditions"
grep -Fiq "No raw provider payloads" docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md \
  || fail "live benchmark summary template must include raw-payload redaction checklist"

# Benchmark/evidence docs should not make unsupported provider-ranking or speed claims.
# Keep this focused on benchmark artifacts so general product phrasing such as
# "make search better" does not create false positives.
unsupported_claim_re='(^|[^[:alnum:]_])(# ?1|fastest|faster[[:space:]]+than|cheapest|cheaper[[:space:]]+than|best[[:space:]]+provider|beats|outperforms|superior|guaranteed)([^[:alnum:]_]|$)'
unsupported_live_claim_re='(^|[^[:alnum:]_])((global[[:space:]-]+)?top[[:space:]-]+choice|lowest[[:space:]-]+latency|lowest[[:space:]-]+cost|categorically[[:space:]-]+preferable|always[[:space:]-]+works|benchmark-primary|benchmark[[:space:]-]+primary)([^[:alnum:]_]|$)'
unsupported_ddgs_primary_re='ddgs.*(primary.*benchmark|benchmark.*primary|primary.*docs|docs.*primary)'
benchmark_docs=(
  docs/BENCHMARKS.md
  docs/ROUTE-EVIDENCE.md
  docs/LIVE-BENCHMARK-PLAN.md
  docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md
)
scanable_claim_lines=$(
  grep -EinH "$unsupported_claim_re|$unsupported_live_claim_re|$unsupported_ddgs_primary_re" "${benchmark_docs[@]}" \
    | grep -Eiv 'do not|does not|cannot|must not|not intended|not as|never|non-claims?' \
    || true
)
if [[ -n "$scanable_claim_lines" ]]; then
  printf '%s\n' "$scanable_claim_lines" >&2
  fail "benchmark/evidence docs contain unsupported ranking/speed language"
fi

echo "benchmark claims check passed"
