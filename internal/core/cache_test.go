package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type countingProvider struct {
	fakeProvider
	searchCalls  int
	extractCalls int
}

func (p *countingProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	p.searchCalls++
	return p.fakeProvider.Search(ctx, req)
}

func (p *countingProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	p.extractCalls++
	return p.fakeProvider.Extract(ctx, req)
}

func TestServiceSearchUsesNormalizedTTLCacheAndAnnotatesTrace(t *testing.T) {
	provider := &countingProvider{fakeProvider: fakeProvider{name: "ddgs"}}
	registry := NewRegistry()
	_ = registry.Register(provider)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	cache := NewMemoryResponseCache(1 * time.Hour)
	service := NewService(registry, ledger, RouteMatrix{TaskDocs: {"ddgs"}}, WithResponseCache(cache))

	first, err := service.Search(context.Background(), SearchRequest{Query: "  Cobra   Docs  ", Task: TaskDocs, Limit: 3})
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}
	if provider.searchCalls != 1 {
		t.Fatalf("first search should call provider once, got %d", provider.searchCalls)
	}
	if len(first.RouteTrace) < 2 || first.RouteTrace[0].CacheStatus != CacheStatusMiss || first.RouteTrace[0].Reason != "cache_miss" {
		t.Fatalf("first search should include cache miss trace first, got %#v", first.RouteTrace)
	}
	if !strings.Contains(first.RoutingInsight, "cache miss") {
		t.Fatalf("first routing insight should mention cache miss, got %q", first.RoutingInsight)
	}

	second, err := service.Search(context.Background(), SearchRequest{Query: "cobra docs", Task: TaskDocs, Limit: 3})
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}
	if provider.searchCalls != 1 {
		t.Fatalf("cache hit should not call provider again, got %d calls", provider.searchCalls)
	}
	if len(second.RouteTrace) != 1 || second.RouteTrace[0].CacheStatus != CacheStatusHit || second.RouteTrace[0].Reason != "cache_hit" {
		t.Fatalf("second search should include cache hit trace, got %#v", second.RouteTrace)
	}
	if second.Provider != "ddgs" || len(second.Results) == 0 {
		t.Fatalf("cache hit should return normalized cached response, got %#v", second)
	}
	if !strings.Contains(second.RoutingInsight, "cache hit") {
		t.Fatalf("second routing insight should mention cache hit, got %q", second.RoutingInsight)
	}
}

type flakySearchProvider struct {
	fakeProvider
	calls int
}

func (p *flakySearchProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	p.calls++
	if p.calls == 1 {
		return SearchResponse{}, errors.New("temporary provider failure")
	}
	return p.fakeProvider.Search(ctx, req)
}

func TestServiceDoesNotCacheSearchErrors(t *testing.T) {
	provider := &flakySearchProvider{fakeProvider: fakeProvider{name: "ddgs"}}
	registry := NewRegistry()
	_ = registry.Register(provider)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	cache := NewMemoryResponseCache(1 * time.Hour)
	service := NewService(registry, ledger, RouteMatrix{TaskDocs: {"ddgs"}}, WithResponseCache(cache))

	if _, err := service.Search(context.Background(), SearchRequest{Query: "cache errors", Task: TaskDocs}); err == nil {
		t.Fatal("first provider failure should be returned")
	}
	if provider.calls != 1 {
		t.Fatalf("first call count = %d, want 1", provider.calls)
	}
	second, err := service.Search(context.Background(), SearchRequest{Query: "cache errors", Task: TaskDocs})
	if err != nil {
		t.Fatalf("second search should retry provider after uncached error: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("second search should call provider again instead of returning cached error, got %d calls", provider.calls)
	}
	if len(second.RouteTrace) < 2 || second.RouteTrace[0].CacheStatus != CacheStatusMiss {
		t.Fatalf("second search should still start with cache miss, got %#v", second.RouteTrace)
	}
}

func TestServiceExtractUsesTTLCacheAndExpiresEntries(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	provider := &countingProvider{fakeProvider: fakeProvider{name: "firecrawl"}}
	registry := NewRegistry()
	_ = registry.Register(provider)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassFreeTierBYOK, FreeRemaining: 3})
	cache := NewMemoryResponseCacheWithClock(10*time.Second, clock)
	service := NewService(registry, ledger, RouteMatrix{TaskExtract: {"firecrawl"}}, WithResponseCache(cache))

	if _, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com/page", Format: "markdown"}); err != nil {
		t.Fatalf("first extract failed: %v", err)
	}
	if provider.extractCalls != 1 {
		t.Fatalf("first extract should call provider once, got %d", provider.extractCalls)
	}
	if _, err := service.Extract(context.Background(), ExtractRequest{URL: " https://example.com/page ", Format: "markdown"}); err != nil {
		t.Fatalf("second extract failed: %v", err)
	}
	if provider.extractCalls != 1 {
		t.Fatalf("cache hit should not call provider again, got %d calls", provider.extractCalls)
	}

	now = now.Add(11 * time.Second)
	third, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com/page", Format: "markdown"})
	if err != nil {
		t.Fatalf("third extract failed: %v", err)
	}
	if provider.extractCalls != 2 {
		t.Fatalf("expired cache should call provider again, got %d calls", provider.extractCalls)
	}
	if len(third.RouteTrace) < 2 || third.RouteTrace[0].CacheStatus != CacheStatusMiss {
		t.Fatalf("expired cache should produce a new miss trace, got %#v", third.RouteTrace)
	}
}
