package bench

import (
	"strings"
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
		core.TaskPeople:    false,
		core.TaskSocial:    false,
		core.TaskSemantic:  false,
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

func TestOfflineReportCarriesHonestEvidenceMetadata(t *testing.T) {
	report := RunOffline(DefaultFixtureSet(), core.DefaultRouteMatrix())
	if report.Evidence.ArtifactKind != "deterministic_fixture_eval" {
		t.Fatalf("artifact kind = %q, want deterministic_fixture_eval", report.Evidence.ArtifactKind)
	}
	if report.Evidence.NetworkRequired || report.Evidence.SecretsRequired {
		t.Fatalf("offline evidence must not require network or secrets: %#v", report.Evidence)
	}
	if !report.Evidence.Sanitized {
		t.Fatalf("offline evidence must be marked sanitized: %#v", report.Evidence)
	}
	joinedLimitations := strings.Join(report.Evidence.DoesNotMeasure, "\n")
	for _, want := range []string{"live web result quality", "provider uptime", "actual cost/quota behavior"} {
		if !strings.Contains(joinedLimitations, want) {
			t.Fatalf("offline evidence limitations missing %q: %#v", want, report.Evidence.DoesNotMeasure)
		}
	}
	joinedRepro := strings.Join(report.Evidence.Reproduction, "\n")
	if !strings.Contains(joinedRepro, "nole bench --json") {
		t.Fatalf("offline evidence reproduction should mention nole bench --json: %#v", report.Evidence.Reproduction)
	}
}

func TestMarkdownEvidenceSummaryIsSanitizedAndHonest(t *testing.T) {
	report := RunOffline(DefaultFixtureSet(), core.DefaultRouteMatrix())
	report.GeneratedAt = "2026-05-18T00:00:00Z"
	report.Cases[0].Attempts = append(report.Cases[0].Attempts, Attempt{
		Provider: "brave",
		Status:   "failed",
		Reason:   "Authorization: Bearer SECRET https://private.example/internal",
	})
	md := MarkdownEvidenceSummary(report)
	for _, want := range []string{"# Route evidence summary", "Mode: offline", "Private data: none included", "does not measure live web result quality"} {
		if !strings.Contains(md, want) {
			t.Fatalf("evidence markdown missing %q:\n%s", want, md)
		}
	}
	for _, forbidden := range []string{"SECRET", "Authorization", "Bearer", "private.example"} {
		if strings.Contains(md, forbidden) {
			t.Fatalf("evidence markdown leaked %q:\n%s", forbidden, md)
		}
	}
}

func TestLiveEvidenceMetadataIsExplicitAboutNetworkCostAndOptionalSecrets(t *testing.T) {
	meta := LiveEvidenceMetadata(string(core.CostPolicyFreeFirst), 3)
	if meta.ArtifactKind != "live_smoke_summary" || !meta.NetworkRequired || meta.SecretsRequired {
		t.Fatalf("unexpected live metadata: %#v", meta)
	}
	if !strings.Contains(meta.CostCaveat, "may use configured provider keys") || !strings.Contains(meta.CostCaveat, "consume quota") {
		t.Fatalf("live cost caveat should describe optional keys and quota/cost: %q", meta.CostCaveat)
	}
	if !strings.Contains(strings.Join(meta.DoesNotMeasure, "\n"), "provider ranking") {
		t.Fatalf("live limitations should reject ranking claims: %#v", meta.DoesNotMeasure)
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
