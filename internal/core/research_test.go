package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// Ctrl-C / SIGTERM during research must surface as a cancellation, not be
// swallowed into a partial report with a success exit. (Moved from cli when the
// pipeline moved into core in v0.6.0.)
func TestResearchSurfacesCancellation(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{name: "p"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "p", FreeRemaining: 10})
	svc := NewService(registry, ledger, RouteMatrix{
		TaskGeneral: {"p"}, TaskResearch: {"p"}, TaskDocs: {"p"}, TaskNews: {"p"}, TaskExtract: {"p"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Research(ctx, "anything", 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("Research with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestClassifiedResearchTasksDrivesFanOut(t *testing.T) {
	if got := classifiedResearchTasks("latest AI news this week"); len(got) == 0 || got[0] != TaskNews {
		t.Fatalf("recency question should lead with news, got %v", got)
	}
	if got := classifiedResearchTasks("Go net/http documentation reference"); len(got) == 0 || got[0] != TaskDocs {
		t.Fatalf("docs question should lead with docs, got %v", got)
	}
	gen := classifiedResearchTasks("jaguar")
	if len(gen) == 0 || gen[0] != TaskGeneral {
		t.Fatalf("no-signal question should lead with general, got %v", gen)
	}
	seen := map[TaskType]bool{}
	for _, task := range gen {
		if seen[task] {
			t.Fatalf("fan-out contains a duplicate task: %v", gen)
		}
		seen[task] = true
	}
	if !seen[TaskResearch] {
		t.Fatalf("fan-out should include research for breadth, got %v", gen)
	}
}

// max_steps caps the number of sources extracted, not just the search fan-out,
// so a small budget on the agent-facing research surface does not burn extra
// extract quota.
func TestResearchMaxStepsBoundsExtracts(t *testing.T) {
	newSvc := func() *Service {
		p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, goodIP2, goodIP3}}
		registry := NewRegistry()
		_ = registry.Register(p)
		ledger := NewMemoryQuotaLedger()
		ledger.Set(QuotaEntry{Provider: "p", FreeRemaining: 100})
		return NewService(registry, ledger, RouteMatrix{
			TaskGeneral: {"p"}, TaskResearch: {"p"}, TaskDocs: {"p"}, TaskExtract: {"p"},
		})
	}

	r1, err := newSvc().Research(context.Background(), "jaguar facts", 1)
	if err != nil {
		t.Fatalf("research maxSteps=1: %v", err)
	}
	if len(r1.Extracts) != 1 {
		t.Fatalf("max_steps=1 must cap extracts at 1, got %d", len(r1.Extracts))
	}

	r3, err := newSvc().Research(context.Background(), "jaguar facts", 3)
	if err != nil {
		t.Fatalf("research maxSteps=3: %v", err)
	}
	if len(r3.Extracts) != 3 {
		t.Fatalf("max_steps=3 should allow up to 3 extracts (3 unique sources), got %d", len(r3.Extracts))
	}
}

// The research report returns evidence only — never a composed summary/answer.
func TestResearchReportHasNoSummaryKey(t *testing.T) {
	report := &ResearchReport{
		Question:  "what is the capital of France",
		Sources:   []ResearchSource{{Title: "t", URL: "u", From: "p"}},
		Providers: []string{"p"},
		Steps:     1,
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["summary"]; ok {
		t.Fatalf("ResearchReport must not carry a 'summary' key: %s", b)
	}
	for _, want := range []string{"question", "sources", "providers_used", "steps"} {
		if _, ok := m[want]; !ok {
			t.Fatalf("ResearchReport missing %q key: %s", want, b)
		}
	}
}
