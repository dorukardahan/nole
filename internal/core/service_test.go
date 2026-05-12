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
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral, Limit: 3})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "brave" {
		t.Fatalf("expected brave, got %q", resp.Provider)
	}
	if len(resp.Route) == 0 || resp.Route[0] != "brave" {
		t.Fatalf("expected route in response, got %#v", resp.Route)
	}
}

func TestServiceSearchFallsBackOnProviderError(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingProvider{fakeProvider{name: "brave"}})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "tavily" {
		t.Fatalf("expected tavily fallback, got %q", resp.Provider)
	}
}

func TestServiceExtractUsesExtractRoute(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "jina"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "jina", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "jina" {
		t.Fatalf("expected jina, got %q", resp.Provider)
	}
}
