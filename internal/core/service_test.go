package core

import (
	"context"
	"errors"
	"testing"
)

type failingProvider struct{ fakeProvider }
type failingExtractProvider struct{ fakeProvider }

type emptySearchProvider struct{ fakeProvider }

type emptyExtractProvider struct{ fakeProvider }

type whitespaceExtractProvider struct{ fakeProvider }

func (f failingProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, errors.New("provider failed")
}

func (f failingExtractProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{}, errors.New("provider failed")
}

func (p emptySearchProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{Query: req.Query, Task: req.Task, Provider: p.name, Results: nil}, nil
}

func (p emptyExtractProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: ""}, nil
}

func (p whitespaceExtractProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: " \n\t "}, nil
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
	if len(resp.RouteTrace) < 2 {
		t.Fatalf("expected route trace for failed provider and fallback, got %#v", resp.RouteTrace)
	}
	if resp.RouteTrace[0].Provider != "brave" || resp.RouteTrace[0].Status != "failed" || resp.RouteTrace[0].Reason == "" {
		t.Fatalf("unexpected first trace entry: %#v", resp.RouteTrace[0])
	}
	if resp.RouteTrace[1].Provider != "firecrawl" || resp.RouteTrace[1].Status != "success" || resp.RouteTrace[1].ResultCount == 0 {
		t.Fatalf("unexpected fallback trace entry: %#v", resp.RouteTrace[1])
	}
}

func TestServiceSearchTraceExplainsSkippedProviders(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 0})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, DefaultRouteMatrix())
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.RouteTrace) < 2 {
		t.Fatalf("expected skip and success trace, got %#v", resp.RouteTrace)
	}
	if resp.RouteTrace[0].Provider != "brave" || resp.RouteTrace[0].Status != "skipped" || resp.RouteTrace[0].Reason != "free_quota_exhausted" {
		t.Fatalf("unexpected skipped trace entry: %#v", resp.RouteTrace[0])
	}
}

func TestServiceSearchTraceExplainsPremiumBlockedByFreeFirst(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"brave", "ddgs"}})

	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "ddgs" {
		t.Fatalf("expected ddgs fallback, got %q", resp.Provider)
	}
	if len(resp.RouteTrace) != 2 {
		t.Fatalf("expected premium skip and keyless success trace, got %#v", resp.RouteTrace)
	}
	if got := resp.RouteTrace[0]; got.Provider != "brave" || got.Status != "skipped" || got.Reason != "premium_blocked_free_first" || got.CostClass != CostClassPremiumCapable || got.CostPolicy != CostPolicyFreeFirst {
		t.Fatalf("unexpected premium blocked trace entry: %#v", got)
	}
	if got := resp.RouteTrace[1]; got.Provider != "ddgs" || got.Status != "success" || got.CostClass != CostClassKeylessFree {
		t.Fatalf("unexpected keyless success trace entry: %#v", got)
	}
}

func TestServiceRecordsPremiumAttemptBeforeProviderError(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingProvider{fakeProvider{name: "tavily"}})
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"tavily"}})

	_, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err == nil {
		t.Fatal("expected provider error")
	}
	budget := ledger.BudgetStatus()
	if budget.SpentCents != 2 {
		t.Fatalf("premium attempt should be recorded before provider call, spent=%d", budget.SpentCents)
	}
}

func TestServiceSearchFallsBackOnEmptyResults(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(emptySearchProvider{fakeProvider{name: "empty"}})
	_ = registry.Register(fakeProvider{name: "brave"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "empty", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"empty", "brave"}})
	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "brave" {
		t.Fatalf("expected brave fallback, got %q", resp.Provider)
	}
	if len(resp.RouteTrace) != 2 {
		t.Fatalf("expected empty and success trace entries, got %#v", resp.RouteTrace)
	}
	if got := resp.RouteTrace[0]; got.Provider != "empty" || got.Status != "failed" || got.Reason != "empty_results" || got.ResultCount != 0 {
		t.Fatalf("unexpected empty trace entry: %#v", got)
	}
	if got := resp.RouteTrace[1]; got.Provider != "brave" || got.Status != "success" {
		t.Fatalf("unexpected fallback trace entry: %#v", got)
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

func TestServiceRecordsPremiumExtractAttemptBeforeProviderError(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingExtractProvider{fakeProvider{name: "tavily"}})
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2})
	service := NewService(registry, ledger, RouteMatrix{TaskExtract: {"tavily"}, TaskGeneral: {"tavily"}})

	_, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected provider error")
	}
	budget := ledger.BudgetStatus()
	if budget.SpentCents != 2 {
		t.Fatalf("premium extract attempt should be recorded before provider call, spent=%d", budget.SpentCents)
	}
}

func TestServiceExtractFallsBackOnEmptyContent(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(whitespaceExtractProvider{fakeProvider{name: "empty"}})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "empty", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskExtract: {"empty", "firecrawl"}, TaskGeneral: {"firecrawl"}})
	resp, err := service.Extract(context.Background(), ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "firecrawl" {
		t.Fatalf("expected firecrawl fallback, got %q", resp.Provider)
	}
	if len(resp.RouteTrace) != 2 {
		t.Fatalf("expected empty and success trace entries, got %#v", resp.RouteTrace)
	}
	if got := resp.RouteTrace[0]; got.Provider != "empty" || got.Status != "failed" || got.Reason != "empty_content" || got.ResultCount != 0 {
		t.Fatalf("unexpected empty trace entry: %#v", got)
	}
	if got := resp.RouteTrace[1]; got.Provider != "firecrawl" || got.Status != "success" {
		t.Fatalf("unexpected fallback trace entry: %#v", got)
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

func TestServiceProviderAndBudgetStatusExposeSafeCostPolicy(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "ddgs"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"tavily", "ddgs"}})

	statuses := service.ProviderStatus(context.Background())
	if len(statuses) != 2 {
		t.Fatalf("expected 2 provider statuses, got %#v", statuses)
	}
	byName := map[string]ProviderStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	if got := byName["ddgs"]; got.CostClass != CostClassKeylessFree || got.CostPolicy != CostPolicyFreeFirst || got.PolicyReason != "keyless_free" {
		t.Fatalf("unexpected ddgs cost status: %#v", got)
	}
	if got := byName["tavily"]; got.CostClass != CostClassPremiumCapable || got.CostPolicy != CostPolicyFreeFirst || got.PolicyReason != "premium_blocked_free_first" || got.AllowedByPolicy {
		t.Fatalf("unexpected tavily cost status: %#v", got)
	}

	budget := service.BudgetStatus()
	if budget.Policy != CostPolicyFreeFirst || !budget.NoHiddenPaidSpend || len(budget.Entries) != 2 {
		t.Fatalf("unexpected budget status: %#v", budget)
	}
}
