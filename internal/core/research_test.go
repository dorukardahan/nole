package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

type researchEvidenceFake struct {
	fakeProvider
	urls       []string
	searchErr  error
	extractErr map[string]error
}

func (p *researchEvidenceFake) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if p.searchErr != nil {
		return SearchResponse{}, p.searchErr
	}
	results := make([]SearchResult, 0, len(p.urls))
	for i, u := range p.urls {
		results = append(results, SearchResult{Title: fmt.Sprintf("r%d", i), URL: u, Snippet: "s", Provider: p.name})
	}
	return SearchResponse{Query: req.Query, Task: req.Task, Provider: p.name, Results: results}, nil
}

func (p *researchEvidenceFake) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	if err := p.extractErr[req.URL]; err != nil {
		return ExtractResponse{}, err
	}
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: "content for " + req.URL}, nil
}

func newResearchEvidenceService(p Provider) *Service {
	registry := NewRegistry()
	_ = registry.Register(p)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: p.Name(), FreeRemaining: 1000})
	return NewService(registry, ledger, RouteMatrix{
		TaskGeneral:  {p.Name()},
		TaskResearch: {p.Name()},
		TaskAcademic: {p.Name()},
		TaskDocs:     {p.Name()},
		TaskExtract:  {p.Name()},
	})
}

func TestResearchEvidenceStepsRecordSearchExtractAndSkips(t *testing.T) {
	pdfURL := "http://1.1.1.1/paper.pdf"
	redditURL := "https://www.reddit.com/r/nole/comments/1/example"
	p := &researchEvidenceFake{fakeProvider: fakeProvider{name: "p"}, urls: []string{goodIP1, pdfURL, redditURL}}
	report, err := newResearchEvidenceService(p).Research(context.Background(), "jaguar", 3)
	if err != nil {
		t.Fatalf("research: %v", err)
	}

	assertEvidenceStep(t, report.EvidenceSteps, ResearchEvidenceStep{Kind: "search", Task: TaskGeneral, Provider: "p", Status: "success", ResultCount: 3})
	assertEvidenceStep(t, report.EvidenceSteps, ResearchEvidenceStep{Kind: "extract", Provider: "p", URL: goodIP1, Status: "success", ContentPresent: true})
	assertEvidenceStep(t, report.EvidenceSteps, ResearchEvidenceStep{Kind: "skip", Provider: "p", URL: pdfURL, Status: "skipped", SkipReason: "pdf_source"})
	assertEvidenceStep(t, report.EvidenceSteps, ResearchEvidenceStep{Kind: "skip", Provider: "p", URL: redditURL, Status: "skipped", SkipReason: "reddit_source"})

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if !strings.Contains(string(b), `"evidence_steps"`) {
		t.Fatalf("research JSON should expose evidence_steps, got %s", b)
	}
	if strings.Contains(string(b), `"summary"`) || strings.Contains(string(b), `"answer"`) {
		t.Fatalf("research evidence must not introduce answer synthesis keys: %s", b)
	}
}

func TestResearchEvidenceStepsRecordSearchFailureWithSanitizedError(t *testing.T) {
	p := &researchEvidenceFake{
		fakeProvider: fakeProvider{name: "p"},
		searchErr:    errors.New("api_key=FAKE-research-secret https://private.example/path"),
	}
	report, err := newResearchEvidenceService(p).Research(context.Background(), "jaguar", 1)
	if err != nil {
		t.Fatalf("research search failure should be non-fatal: %v", err)
	}
	if len(report.Sources) != 0 || len(report.Extracts) != 0 {
		t.Fatalf("failed search should not fabricate sources/extracts: %#v", report)
	}
	step := findEvidenceStep(report.EvidenceSteps, ResearchEvidenceStep{Kind: "search", Task: TaskGeneral, Provider: "p", Status: "failed"})
	if step == nil {
		t.Fatalf("missing failed search evidence step: %#v", report.EvidenceSteps)
	}
	assertSanitizedEvidenceError(t, step.Error)
}

func TestResearchEvidenceStepsRecordExtractFailureWithSanitizedError(t *testing.T) {
	p := &researchEvidenceFake{
		fakeProvider: fakeProvider{name: "p"},
		urls:         []string{goodIP1},
		extractErr:   map[string]error{goodIP1: errors.New("token=FAKE-research-secret https://private.example/path")},
	}
	report, err := newResearchEvidenceService(p).Research(context.Background(), "jaguar", 1)
	if err != nil {
		t.Fatalf("research extract failure should be non-fatal: %v", err)
	}
	if len(report.Extracts) != 0 {
		t.Fatalf("failed extract should not fabricate content: %#v", report.Extracts)
	}
	step := findEvidenceStep(report.EvidenceSteps, ResearchEvidenceStep{Kind: "extract", Provider: "p", URL: goodIP1, Status: "failed"})
	if step == nil {
		t.Fatalf("missing failed extract evidence step: %#v", report.EvidenceSteps)
	}
	assertSanitizedEvidenceError(t, step.Error)
}

func TestResearchEvidenceProviderPrefersFailedAttempt(t *testing.T) {
	got := researchEvidenceProvider("", []RouteAttempt{
		{Provider: "bad", Status: "failed", Reason: "provider_error"},
		{Provider: "blocked", Status: "skipped", Reason: "free_quota_exhausted"},
	})
	if got != "bad" {
		t.Fatalf("provider = %q, want failed provider", got)
	}
}

func assertEvidenceStep(t *testing.T, steps []ResearchEvidenceStep, want ResearchEvidenceStep) {
	t.Helper()
	if got := findEvidenceStep(steps, want); got == nil {
		t.Fatalf("missing evidence step %#v in %#v", want, steps)
	}
}

func findEvidenceStep(steps []ResearchEvidenceStep, want ResearchEvidenceStep) *ResearchEvidenceStep {
	for i := range steps {
		step := &steps[i]
		if want.Kind != "" && step.Kind != want.Kind {
			continue
		}
		if want.Task != "" && step.Task != want.Task {
			continue
		}
		if want.Provider != "" && step.Provider != want.Provider {
			continue
		}
		if want.URL != "" && step.URL != want.URL {
			continue
		}
		if want.Status != "" && step.Status != want.Status {
			continue
		}
		if want.ResultCount != 0 && step.ResultCount != want.ResultCount {
			continue
		}
		if want.ContentPresent && !step.ContentPresent {
			continue
		}
		if want.SkipReason != "" && step.SkipReason != want.SkipReason {
			continue
		}
		return step
	}
	return nil
}

func assertSanitizedEvidenceError(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Fatal("expected sanitized error, got empty")
	}
	for _, forbidden := range []string{"FAKE-research-secret", "private.example"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("evidence error leaked %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker in evidence error, got %q", got)
	}
}
