package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
)

// Ctrl-C / SIGTERM during `nole research` must surface as a cancellation error,
// not be swallowed into a partial report with a success exit. researchPipeline
// treats genuine provider errors as recoverable (log + continue) but must bail
// on a cancelled context.
func TestResearchPipelineSurfacesCancellation(t *testing.T) {
	registry := core.NewRegistry()
	if err := registry.Register(mock.New("mock")); err != nil {
		t.Fatalf("register mock: %v", err)
	}
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "mock", CostClass: core.CostClassKeylessFree, KeylessFree: true})
	svc := core.NewService(registry, ledger, core.RouteMatrix{
		core.TaskGeneral:  {"mock"},
		core.TaskResearch: {"mock"},
		core.TaskDocs:     {"mock"},
		core.TaskExtract:  {"mock"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := researchPipeline(ctx, svc, "anything", 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("researchPipeline with a cancelled context = %v, want context.Canceled", err)
	}
}

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
