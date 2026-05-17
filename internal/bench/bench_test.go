package bench

import (
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestDefaultFixtureSetIsVersionedAndCoversAgentResearchTasks(t *testing.T) {
	set := DefaultFixtureSet()
	if set.Version == "" {
		t.Fatal("fixture set must be versioned")
	}
	if len(set.Fixtures) < 12 {
		t.Fatalf("expected at least 12 fixtures, got %d", len(set.Fixtures))
	}

	wantTasks := map[core.TaskType]bool{
		core.TaskGeneral:   false,
		core.TaskNews:      false,
		core.TaskDocs:      false,
		core.TaskCode:      false,
		core.TaskAcademic:  false,
		core.TaskFactcheck: false,
		core.TaskPricing:   false,
		core.TaskExtract:   false,
	}
	languages := map[string]bool{}
	for _, fixture := range set.Fixtures {
		if _, ok := wantTasks[fixture.Task]; ok {
			wantTasks[fixture.Task] = true
		}
		languages[fixture.Language] = true
	}
	for task, seen := range wantTasks {
		if !seen {
			t.Fatalf("fixture set missing task %q", task)
		}
	}
	if !languages["en"] || !languages["tr"] {
		t.Fatalf("fixture set must include English and Turkish queries, got %#v", languages)
	}
}

func TestOfflineBenchmarkScoresRouteMatrixDeterministically(t *testing.T) {
	report := RunOffline(DefaultFixtureSet(), core.DefaultRouteMatrix())
	if report.Mode != ModeOffline {
		t.Fatalf("mode = %q, want %q", report.Mode, ModeOffline)
	}
	if report.Summary.TotalCases != len(DefaultFixtureSet().Fixtures) {
		t.Fatalf("total cases = %d, want %d", report.Summary.TotalCases, len(DefaultFixtureSet().Fixtures))
	}
	if report.Summary.AverageScore <= 0 {
		t.Fatalf("average score should be positive, got %.2f", report.Summary.AverageScore)
	}
	if len(report.Cases) != len(DefaultFixtureSet().Fixtures) {
		t.Fatalf("case count = %d", len(report.Cases))
	}
	for _, c := range report.Cases {
		if c.SelectedProvider == "" {
			t.Fatalf("case %s did not record selected provider", c.ID)
		}
		if len(c.Route) == 0 {
			t.Fatalf("case %s did not record route", c.ID)
		}
		if len(c.Attempts) == 0 {
			t.Fatalf("case %s did not record attempts", c.ID)
		}
		if c.Metrics.Relevance < 0 || c.Metrics.Relevance > 1 {
			t.Fatalf("case %s relevance out of range: %.2f", c.ID, c.Metrics.Relevance)
		}
	}
}

func TestOfflineBenchmarkPenalizesEmptyResultAndRewardsFallback(t *testing.T) {
	set := FixtureSet{
		Version: "test",
		Fixtures: []Fixture{
			{ID: "empty-fallback", Task: core.TaskGeneral, Query: "deterministic empty result", Language: "en", Kind: KindSearch},
		},
	}
	matrix := core.RouteMatrix{core.TaskGeneral: []string{"empty", "brave"}}
	report := RunOfflineWithObservations(set, matrix, map[string]map[core.TaskType]Observation{
		"empty": {
			core.TaskGeneral: {Success: true, ResultCount: 0, LatencyMS: 20, EmptyResultBehavior: 0.1},
		},
		"brave": {
			core.TaskGeneral: {Success: true, ResultCount: 5, Relevance: 0.9, Freshness: 0.8, CitationQuality: 0.9, Diversity: 0.8, LatencyMS: 120},
		},
	})
	if len(report.Cases) != 1 {
		t.Fatalf("expected one case, got %d", len(report.Cases))
	}
	c := report.Cases[0]
	if c.SelectedProvider != "brave" {
		t.Fatalf("expected fallback to brave, got %q", c.SelectedProvider)
	}
	if len(c.Attempts) < 2 || c.Attempts[0].Reason != "empty_results" || c.Attempts[1].Reason != "success_after_fallback" {
		t.Fatalf("unexpected attempts: %#v", c.Attempts)
	}
	if c.Metrics.FallbackBehavior <= 0 {
		t.Fatalf("fallback behavior should be rewarded, got %.2f", c.Metrics.FallbackBehavior)
	}
}
