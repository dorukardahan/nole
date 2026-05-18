package core

import (
	"context"
	"strings"
	"testing"
)

func TestServiceSearchAddsCompactRoutingInsight(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true})
	service := NewService(registry, ledger, RouteMatrix{TaskDocs: {"ddgs"}})

	resp, err := service.Search(context.Background(), SearchRequest{Query: "cobra docs", Task: TaskDocs, Limit: 3})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.RoutingInsight == "" {
		t.Fatal("expected compact routing insight")
	}
	for _, want := range []string{"Nólë:", "docs", "ddgs", "free-first", "1 result"} {
		if !strings.Contains(resp.RoutingInsight, want) {
			t.Fatalf("routing insight missing %q: %q", want, resp.RoutingInsight)
		}
	}
	if len(resp.RouteTrace) == 0 {
		t.Fatal("routing insight must not replace full route_trace")
	}
}

func TestServiceExtractAddsCompactRoutingInsight(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassFreeTierBYOK, FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskExtract: {"firecrawl"}})

	resp, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.RoutingInsight == "" {
		t.Fatal("expected compact routing insight")
	}
	for _, want := range []string{"Nólë:", "extract", "firecrawl"} {
		if !strings.Contains(resp.RoutingInsight, want) {
			t.Fatalf("routing insight missing %q: %q", want, resp.RoutingInsight)
		}
	}
	if len(resp.RouteTrace) == 0 {
		t.Fatal("routing insight must not replace full route_trace")
	}
}

func TestRoutePlanAddsCompactRoutingInsight(t *testing.T) {
	plan := BuildRoutePlan("OpenAI API docs pricing and latest changelog", DefaultRouteMatrix(), PlanOptions{})
	if plan.RoutingInsight == "" {
		t.Fatal("expected route-plan routing insight")
	}
	for _, want := range []string{"Nólë:", "route-plan", "planned", "docs"} {
		if !strings.Contains(plan.RoutingInsight, want) {
			t.Fatalf("route-plan insight missing %q: %q", want, plan.RoutingInsight)
		}
	}
	if len(plan.RouteTrace) == 0 {
		t.Fatal("route-plan insight must not replace full route_trace")
	}
}

func TestClassifyAddsCompactRoutingInsight(t *testing.T) {
	classification := ClassifyQuery("Vercel pricing limits", PlanOptions{})
	if classification.RoutingInsight == "" {
		t.Fatal("expected classify routing insight")
	}
	for _, want := range []string{"Nólë:", "classified", "pricing"} {
		if !strings.Contains(classification.RoutingInsight, want) {
			t.Fatalf("classification insight missing %q: %q", want, classification.RoutingInsight)
		}
	}
}

func TestCompactErrorRoutingInsightDoesNotExposeRoutePayloads(t *testing.T) {
	insight := BuildErrorRoutingInsight("search", []string{"https://private.example/path", "token=SECRET"}, []RouteAttempt{
		{Provider: "https://private.example/path", Status: "failed", Reason: "api_key=SECRET_TOKEN"},
	})
	for _, forbidden := range []string{"private.example", "SECRET", "api_key", "token="} {
		if strings.Contains(insight, forbidden) {
			t.Fatalf("compact error insight leaked %q: %q", forbidden, insight)
		}
	}
}
