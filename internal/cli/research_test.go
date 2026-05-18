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
}
