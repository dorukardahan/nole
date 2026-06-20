package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestResearchHelpQualifiesCostPolicyInsteadOfClaimingNoPaidRequests(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"research", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("research help failed: %v", err)
	}

	help := out.String()
	if strings.Contains(help, "No paid requests") {
		t.Fatalf("research help should not make absolute no-paid-requests claims: %s", help)
	}
	for _, want := range []string{
		"Defaults to free-first/no-hidden-paid-spend routing",
		"Explicit cost policy settings can allow premium-capable providers",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("research help missing truthful cost policy qualifier %q: %s", want, help)
		}
	}
	// v0.6.0: research returns evidence, not a composed summary — the old
	// summary-promising wording must be gone.
	for _, gone := range []string{"Synthesizes a cited summary", "synthesis with citations"} {
		if strings.Contains(help, gone) {
			t.Fatalf("research help still promises a composed summary (%q): %s", gone, help)
		}
	}
}

func TestResearchCommandHasSearchOptionsFlags(t *testing.T) {
	cmd := newResearchCommand()
	for _, name := range []string{"country", "search-lang", "ui-lang", "safesearch", "freshness"} {
		if flag := cmd.Flags().Lookup(name); flag == nil {
			t.Fatalf("research command missing --%s flag", name)
		}
	}
}
