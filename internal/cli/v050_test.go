package cli

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

// resolveCLITask must distinguish an omitted --task (→ empty, so the service
// auto-classifies) from an explicit one. This guards the BLOCKER: parseTask("")
// collapses to general, which would silently disable detection from the CLI.
func TestResolveCLITaskOmittedVsExplicit(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		var raw string
		c.Flags().StringVar(&raw, "task", "", "")
		return c
	}

	if got := resolveCLITask(newCmd(), ""); got != "" {
		t.Fatalf("unset --task should forward empty TaskType, got %q", got)
	}

	explicitNews := newCmd()
	_ = explicitNews.Flags().Set("task", "news")
	if got := resolveCLITask(explicitNews, "news"); got != core.TaskNews {
		t.Fatalf("explicit --task news = %q, want news", got)
	}

	explicitGeneral := newCmd()
	_ = explicitGeneral.Flags().Set("task", "general")
	if got := resolveCLITask(explicitGeneral, "general"); got != core.TaskGeneral {
		t.Fatalf("explicit --task general = %q, want general (supplied, not empty)", got)
	}
}

func TestRESTSearchSurfacesTaskSource(t *testing.T) {
	h := newTestHTTPHandler(t)

	decode := func(body []byte) core.SearchResponse {
		t.Helper()
		rec := doREST(t, h, http.MethodPost, "/api/search", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /api/search %s = %d: %s", body, rec.Code, rec.Body.String())
		}
		var resp core.SearchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	// Omitted task + recency query → auto-detected as news.
	if resp := decode([]byte(`{"query":"latest AI news this week"}`)); resp.Task != core.TaskNews || resp.TaskSource != core.TaskSourceDetected {
		t.Fatalf("no-task news query → task=%q source=%q, want news/detected", resp.Task, resp.TaskSource)
	}
	// Omitted task + no signal → general default.
	if resp := decode([]byte(`{"query":"hello"}`)); resp.Task != core.TaskGeneral || resp.TaskSource != core.TaskSourceDefault {
		t.Fatalf("no-signal query → task=%q source=%q, want general/default", resp.Task, resp.TaskSource)
	}
	// Explicit task → supplied.
	if resp := decode([]byte(`{"query":"x","task":"docs"}`)); resp.Task != core.TaskDocs || resp.TaskSource != core.TaskSourceSupplied {
		t.Fatalf("explicit docs → task=%q source=%q, want docs/supplied", resp.Task, resp.TaskSource)
	}
	// Bogus task is lenient: no error, falls through to classification.
	if resp := decode([]byte(`{"query":"hello","task":"newz"}`)); resp.TaskSource == core.TaskSourceSupplied {
		t.Fatalf("bogus task must not be 'supplied', got %q", resp.TaskSource)
	}
}

// classifiedResearchTasks must drive the research fan-out from the question's
// classification (replacing the old hardcoded [general,research,docs]), be
// deterministic, de-duplicated, and top up with broad coverage.
func TestClassifiedResearchTasksDrivesFanOut(t *testing.T) {
	if got := classifiedResearchTasks("latest AI news this week"); len(got) == 0 || got[0] != core.TaskNews {
		t.Fatalf("recency question should lead with news, got %v", got)
	}
	if got := classifiedResearchTasks("Go net/http documentation reference"); len(got) == 0 || got[0] != core.TaskDocs {
		t.Fatalf("docs question should lead with docs, got %v", got)
	}

	gen := classifiedResearchTasks("jaguar")
	if len(gen) == 0 || gen[0] != core.TaskGeneral {
		t.Fatalf("no-signal question should lead with general, got %v", gen)
	}
	seen := map[core.TaskType]bool{}
	for _, task := range gen {
		if seen[task] {
			t.Fatalf("fan-out contains a duplicate task: %v", gen)
		}
		seen[task] = true
	}
	if !seen[core.TaskResearch] {
		t.Fatalf("fan-out should include research for breadth, got %v", gen)
	}
}

// REST must normalize task aliases like MCP does (not raw-cast), so an explicit
// alias routes correctly and reports task_source=supplied instead of misrouting
// to general/default. Guards the must-fix REST/MCP divergence.
func TestRESTSearchNormalizesTaskAliases(t *testing.T) {
	h := newTestHTTPHandler(t)
	check := func(body string, wantTask core.TaskType, wantSource core.TaskSource) {
		t.Helper()
		rec := doREST(t, h, http.MethodPost, "/api/search", []byte(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", body, rec.Code, rec.Body.String())
		}
		var resp core.SearchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Task != wantTask || resp.TaskSource != wantSource {
			t.Fatalf("%s → task=%q source=%q, want %q/%q", body, resp.Task, resp.TaskSource, wantTask, wantSource)
		}
	}
	check(`{"query":"x","task":"community"}`, core.TaskSocial, core.TaskSourceSupplied)
	check(`{"query":"x","task":"technical-docs"}`, core.TaskDocs, core.TaskSourceSupplied)
}
