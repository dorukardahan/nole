package core

import (
	"context"
	"errors"
	"testing"
)

type failingProvider struct{ fakeProvider }

func (f failingProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, errors.New("provider failed")
}

func TestServiceSearchCallsSelectedProvider(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral, Limit: 3})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "brave" {
		t.Fatalf("expected brave, got %q", resp.Provider)
	}
	if len(resp.Route) == 0 || resp.Route[0] != "brave" {
		t.Fatalf("expected route starting with brave, got %#v", resp.Route)
	}
}

func TestServiceSearchFallsBackOnProviderError(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingProvider{fakeProvider{name: "brave"}})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "firecrawl" {
		t.Fatalf("expected firecrawl fallback, got %q", resp.Provider)
	}
}

func TestServiceExtractUsesExtractRoute(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "tavily" {
		t.Fatalf("expected tavily, got %q", resp.Provider)
	}
}

func TestServiceExtractBlocksSSRF(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())

	blockedURLs := []string{
		"http://localhost:8080/secret",
		"http://127.0.0.1/admin",
		"http://10.0.0.1/internal",
		"http://192.168.1.1/router",
		"http://169.254.169.254/metadata",
		"file:///etc/passwd",
	}
	for _, u := range blockedURLs {
		_, err := service.Extract(context.Background(), ExtractRequest{URL: u})
		if err == nil {
			t.Errorf("expected SSRF URL %q to be blocked", u)
		}
	}
}
