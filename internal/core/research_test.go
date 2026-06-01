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
	// Seven distinct public literal-IP sources (net-free), so the absolute cap is
	// observable separately from the source count.
	urls := []string{goodIP1, goodIP2, goodIP3, "http://9.9.9.9/", "http://8.8.4.4/", "http://1.0.0.1/", "http://208.67.222.222/"}
	newSvc := func() *Service {
		p := &saeFake{fakeProvider: fakeProvider{name: "p"}, urls: urls}
		registry := NewRegistry()
		_ = registry.Register(p)
		ledger := NewMemoryQuotaLedger()
		ledger.Set(QuotaEntry{Provider: "p", FreeRemaining: 1000})
		return NewService(registry, ledger, RouteMatrix{
			TaskGeneral: {"p"}, TaskResearch: {"p"}, TaskDocs: {"p"}, TaskExtract: {"p"},
		})
	}

	cases := []struct {
		maxSteps   int
		wantExtras int
	}{
		{1, 1},                    // small budget → few extracts
		{3, 3},                    // default
		{99, maxResearchExtracts}, // large budget → clamped at the absolute ceiling (5)
	}
	for _, tc := range cases {
		r, err := newSvc().Research(context.Background(), "jaguar facts", tc.maxSteps)
		if err != nil {
			t.Fatalf("research maxSteps=%d: %v", tc.maxSteps, err)
		}
		if len(r.Extracts) != tc.wantExtras {
			t.Fatalf("max_steps=%d: extracts = %d, want %d", tc.maxSteps, len(r.Extracts), tc.wantExtras)
		}
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
