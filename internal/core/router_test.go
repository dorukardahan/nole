package core

import "testing"

func TestRouterGeneralPrefersBrave(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, route, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "brave" {
		t.Fatalf("expected brave, got %q", provider.Name())
	}
	if len(route) == 0 || route[0] != "brave" {
		t.Fatalf("expected route to start with brave, got %#v", route)
	}
}

func TestRouterFallsBackWhenQuotaExhausted(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 0})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "firecrawl" {
		t.Fatalf("expected firecrawl fallback, got %q", provider.Name())
	}
}

func TestRouterReturnsNoFreeQuota(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 0})
	router := NewRouter(registry, ledger, RouteMatrix{TaskGeneral: []string{"tavily"}})
	_, _, err := router.Select(TaskGeneral, CapabilitySearch)
	if !IsNoFreeQuota(err) {
		t.Fatalf("expected no_free_quota, got %v", err)
	}
}

func TestRouterExtractFallsBackWhenLocalScraplingUnavailable(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskExtract, CapabilityExtract)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "firecrawl" {
		t.Fatalf("expected firecrawl fallback when scrapling is not registered, got %q", provider.Name())
	}
}

func TestRouterExtractPrefersConfiguredLocalScrapling(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "scrapling"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "scrapling", KeylessFree: true})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())

	provider, route, err := router.Select(TaskExtract, CapabilityExtract)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "scrapling" {
		t.Fatalf("expected evidence-backed scrapling first, got %q with route %#v", provider.Name(), route)
	}
	if len(route) != 3 || route[0] != "scrapling" || route[1] != "firecrawl" || route[2] != "tavily" {
		t.Fatalf("extract route should prefer local Scrapling then remote fallbacks, got %#v", route)
	}
}

func TestRouterNewsPrefersFirecrawl(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskNews, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "firecrawl" {
		t.Fatalf("expected firecrawl for news, got %q", provider.Name())
	}
}

func TestDefaultRouteMatrixMatchesLatestTaskBenchmarkEvidence(t *testing.T) {
	matrix := DefaultRouteMatrix()
	want := map[TaskType][]string{
		TaskGeneral:   {"brave", "tavily", "firecrawl", "ddgs"},
		TaskNews:      {"firecrawl", "tavily", "brave", "ddgs"},
		TaskDocs:      {"firecrawl", "brave", "tavily", "ddgs"},
		TaskAcademic:  {"tavily", "firecrawl", "brave", "wikipedia", "ddgs"},
		TaskFactcheck: {"firecrawl", "tavily", "brave", "wikipedia", "ddgs"},
		TaskSemantic:  {"tavily", "brave", "firecrawl", "ddgs"},
		TaskCode:      {"tavily", "firecrawl", "brave", "ddgs"},
		TaskSocial:    {"firecrawl", "tavily", "brave", "ddgs"},
		TaskPeople:    {"firecrawl", "brave", "tavily", "wikipedia", "ddgs"},
		TaskPricing:   {"firecrawl", "brave", "tavily", "ddgs"},
		TaskResearch:  {"firecrawl", "tavily", "brave", "ddgs"},
		TaskExtract:   {"scrapling", "firecrawl", "tavily"},
	}
	for task, route := range want {
		got := matrix[task]
		if len(got) != len(route) {
			t.Fatalf("%s route length = %d, want %d: %#v", task, len(got), len(route), got)
		}
		for i := range route {
			if got[i] != route[i] {
				t.Fatalf("%s route = %#v, want %#v", task, got, route)
			}
		}
	}
}

func TestRouterFreeFirstSkipsPremiumCapableAndUsesKeylessFallback(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	router := NewRouter(registry, ledger, RouteMatrix{TaskGeneral: []string{"brave", "ddgs"}})

	provider, route, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "ddgs" {
		t.Fatalf("expected keyless fallback instead of premium-capable provider, got %q", provider.Name())
	}
	if len(route) != 2 || route[0] != "brave" || route[1] != "ddgs" {
		t.Fatalf("route should preserve matrix order for traceability, got %#v", route)
	}
}
