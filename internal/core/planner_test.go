package core

import "testing"

func TestClassifyQueryRuleBasedIntents(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantTask  TaskType
		wantLabel string
	}{
		{name: "docs", query: "Go net/http Client Timeout documentation", wantTask: TaskDocs, wantLabel: "docs"},
		{name: "pricing", query: "Cloudflare Workers pricing limits", wantTask: TaskPricing, wantLabel: "pricing"},
		{name: "academic", query: "arXiv paper retrieval augmented generation survey", wantTask: TaskAcademic, wantLabel: "academic"},
		{name: "news", query: "latest OpenAI announcement today", wantTask: TaskNews, wantLabel: "news"},
		{name: "code", query: "GitHub cobra command implementation example", wantTask: TaskCode, wantLabel: "code"},
		{name: "community", query: "Reddit discussions about best local LLM router", wantTask: TaskSocial, wantLabel: "community"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyQuery(tc.query, PlanOptions{})
			if got.PrimaryTask != tc.wantTask {
				t.Fatalf("primary task = %q, want %q; classification=%#v", got.PrimaryTask, tc.wantTask, got)
			}
			if len(got.Intents) == 0 || got.Intents[0].Label != tc.wantLabel {
				t.Fatalf("top label = %#v, want %q", got.Intents, tc.wantLabel)
			}
			if got.RuleVersion == "" || got.Intents[0].Reason == "" || len(got.Intents[0].Signals) == 0 {
				t.Fatalf("classification should include version, reason, and signals: %#v", got)
			}
		})
	}
}

func TestClassifyQueryAmbiguousFallback(t *testing.T) {
	got := ClassifyQuery("jaguar", PlanOptions{})
	if !got.Ambiguous {
		t.Fatalf("expected ambiguous fallback: %#v", got)
	}
	if got.PrimaryTask != TaskGeneral {
		t.Fatalf("primary task = %q, want general", got.PrimaryTask)
	}
	if len(got.Intents) != 1 || got.Intents[0].Task != TaskGeneral {
		t.Fatalf("expected one general fallback intent: %#v", got.Intents)
	}
}

func TestBuildRoutePlanMultiIntentAndTrace(t *testing.T) {
	plan := BuildRoutePlan("OpenAI API docs pricing and latest changelog", DefaultRouteMatrix(), PlanOptions{})
	want := map[TaskType]bool{TaskDocs: false, TaskPricing: false, TaskNews: false}
	for _, route := range plan.Routes {
		if _, ok := want[route.Task]; ok {
			want[route.Task] = true
		}
		if len(route.Route) == 0 {
			t.Fatalf("route for %s is empty", route.Task)
		}
	}
	for task, seen := range want {
		if !seen {
			t.Fatalf("missing planned route for %s in %#v", task, plan.Routes)
		}
	}
	if len(plan.RouteTrace) == 0 {
		t.Fatal("expected planned route trace")
	}
	for _, attempt := range plan.RouteTrace {
		if attempt.Status != "planned" || attempt.Provider == "" || attempt.Reason == "" {
			t.Fatalf("unexpected trace attempt: %#v", attempt)
		}
	}
}

func TestBuildRoutePlanOverrides(t *testing.T) {
	plan := BuildRoutePlan("jaguar", DefaultRouteMatrix(), PlanOptions{TaskOverride: TaskDocs, ProviderOverride: []string{"ddgs", "firecrawl"}})
	if plan.PrimaryTask != TaskDocs || plan.Ambiguous {
		t.Fatalf("override should force docs and clear ambiguity: %#v", plan)
	}
	if len(plan.Routes) != 1 {
		t.Fatalf("expected one override route: %#v", plan.Routes)
	}
	if got := plan.Routes[0].Route; len(got) != 2 || got[0] != "ddgs" || got[1] != "firecrawl" {
		t.Fatalf("provider override route = %#v", got)
	}
	for _, attempt := range plan.RouteTrace {
		if attempt.Reason != "provider_override" {
			t.Fatalf("expected provider_override trace, got %#v", attempt)
		}
	}
}
