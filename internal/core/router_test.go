package core

import "testing"

func TestRouterGeneralPrefersTavilyThenBraveThenFirecrawl(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, route, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "tavily" {
		t.Fatalf("expected tavily, got %q", provider.Name())
	}
	if len(route) == 0 || route[0] != "tavily" {
		t.Fatalf("expected route to start with tavily, got %#v", route)
	}
}

func TestRouterFallsBackWhenQuotaExhausted(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "brave"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 0})
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "brave" {
		t.Fatalf("expected brave fallback, got %q", provider.Name())
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

func TestRouterExtractPrefersTavilyThenFirecrawl(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "jina"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "jina", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskExtract, CapabilityExtract)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "tavily" {
		t.Fatalf("expected tavily, got %q", provider.Name())
	}
}

func TestRouterNewsPrefersDDGS(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "ddgs"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())
	provider, _, err := router.Select(TaskNews, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "ddgs" {
		t.Fatalf("expected ddgs for news, got %q", provider.Name())
	}
}
