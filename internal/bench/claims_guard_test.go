package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestBenchmarkClaimsGuardRejectsUnsupportedClaims(t *testing.T) {
	repoRoot := repoRootForTest(t)
	cases := []string{
		"Brave is #1 provider for benchmark routing.",
		"Brave is faster than Tavily for benchmark routing.",
		"Brave is the best provider for benchmark routing.",
		"Brave outperforms Tavily for benchmark routing.",
		"Brave has guaranteed benchmark routing results.",
		"Brave is the global top choice for benchmark routing.",
		"Brave is the lowest-latency option for benchmark routing.",
		"Brave is the lowest-cost option for benchmark routing.",
		"Brave is categorically preferable for benchmark routing.",
		"Brave always works for benchmark routing.",
		"DDGS is the benchmark-primary docs provider.",
		"DDGS is the primary docs benchmark provider.",
	}
	targets := []string{
		filepath.Join("docs", "BENCHMARKS.md"),
		filepath.Join("docs", "ROUTE-EVIDENCE.md"),
		filepath.Join("docs", "LIVE-BENCHMARK-PLAN.md"),
		filepath.Join("docs", "LIVE-BENCHMARK-SUMMARY-TEMPLATE.md"),
	}
	for _, target := range targets {
		for _, claim := range cases {
			t.Run(target+"/"+claim, func(t *testing.T) {
				root := copyBenchmarkClaimsGuardFixture(t, repoRoot)
				f, err := os.OpenFile(filepath.Join(root, target), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString("\n" + claim + "\n"); err != nil {
					_ = f.Close()
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("bash", "scripts/check-benchmark-claims.sh")
				cmd.Dir = root
				if err := cmd.Run(); err == nil {
					t.Fatalf("claims guard accepted unsupported claim %q in %s", claim, target)
				}
			})
		}
	}
}

func TestBenchmarkClaimsGuardAcceptsHonestFixtureDocs(t *testing.T) {
	repoRoot := repoRootForTest(t)
	root := copyBenchmarkClaimsGuardFixture(t, repoRoot)
	cmd := exec.Command("bash", "scripts/check-benchmark-claims.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claims guard rejected honest fixture docs: %v\n%s", err, out)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func copyBenchmarkClaimsGuardFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "check-benchmark-claims.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "check-benchmark-claims.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	benchDoc := []byte(`# Benchmarks

Deterministic offline harness

The deterministic offline harness does not measure live web quality.

Route matrix changes require evidence.

See docs/LIVE-BENCHMARK-PLAN.md and docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md.
`)
	if err := os.WriteFile(filepath.Join(root, "docs", "BENCHMARKS.md"), benchDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	evidenceDoc := []byte(`# Route evidence summary deterministic-offline

This does not measure live web result quality.

Private data: none included

## Raw artifact policy

No raw provider payloads exist in offline mode.
`)
	if err := os.WriteFile(filepath.Join(root, "docs", "ROUTE-EVIDENCE.md"), evidenceDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	livePlanDoc := []byte(`# Controlled live benchmark plan

Live calls require explicit maintainer approval.

## Provider-key inventory rules

Presence-only; never values.

## Stop conditions

Stop on quota, cost, secret-like output or raw payload leakage risk.

Do not claim one provider is always the top choice or lowest-latency option.
`)
	if err := os.WriteFile(filepath.Join(root, "docs", "LIVE-BENCHMARK-PLAN.md"), livePlanDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	liveTemplateDoc := []byte(`# Live benchmark summary template

## Redaction checklist

- [ ] No raw provider payloads.

Do not claim DDGS is a primary docs benchmark provider.
`)
	if err := os.WriteFile(filepath.Join(root, "docs", "LIVE-BENCHMARK-SUMMARY-TEMPLATE.md"), liveTemplateDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSetupHintStringsAreSanitized(t *testing.T) {
	// Every user-facing string in BYOKProviders flows into provider_status and
	// search_tip output. Reuse the existing banned-words pattern so claims like
	// "Tavily is the best" or "Brave is the fastest" can't sneak in via a
	// metadata edit. Quantitative claims (e.g., "3x faster") would also be
	// caught by the bench's main claims guard once they appear in a markdown
	// summary, but the metadata strings never reach that path — they live
	// only in JSON tool responses. Hence this test.
	bad := []string{"best", "fastest", "always works", "guaranteed", "unbeatable", "outperforms"}
	check := func(field, value string) {
		lower := strings.ToLower(value)
		for _, b := range bad {
			if strings.Contains(lower, b) {
				t.Errorf("%s contains banned word %q: %q", field, b, value)
			}
		}
	}
	for _, p := range core.BYOKProviders() {
		check(p.Name+".FreeTierNote", p.FreeTierNote)
	}
	// Both extract-availability modes so the keyless-backstop workaround string and
	// the no-extract workaround string are each guarded against marketing claims.
	for _, extractAvailable := range []bool{true, false} {
		for _, s := range core.BuildSetupSuggestions(map[string]bool{}, extractAvailable) {
			check(s.MissingKey+".CurrentWorkaround", s.CurrentWorkaround)
			check(s.MissingKey+".FreeTier", s.FreeTier)
		}
	}
}
