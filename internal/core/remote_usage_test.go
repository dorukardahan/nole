package core

import (
	"context"
	"errors"
	"testing"
)

type remoteUsageFakeProvider struct {
	name        string
	caps        []Capability
	results     []SearchResult
	usage       ProviderUsage
	usageErr    error
	usageCalls  int
	searchCalls int
}

func (p *remoteUsageFakeProvider) Name() string               { return p.name }
func (p *remoteUsageFakeProvider) Capabilities() []Capability { return p.caps }
func (p *remoteUsageFakeProvider) Status(ctx context.Context) ProviderStatus {
	return ProviderStatus{Name: p.name, Available: true, Capabilities: p.caps}
}
func (p *remoteUsageFakeProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	p.searchCalls++
	results := p.results
	if results == nil {
		results = []SearchResult{{Title: "result", URL: "https://example.com", Snippet: "snippet", Provider: p.name}}
	}
	return SearchResponse{Query: req.Query, Provider: p.name, Results: results}, nil
}
func (p *remoteUsageFakeProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: "content"}, nil
}
func (p *remoteUsageFakeProvider) Usage(ctx context.Context) (ProviderUsage, error) {
	p.usageCalls++
	if p.usageErr != nil {
		return ProviderUsage{}, p.usageErr
	}
	return p.usage, nil
}

func TestProviderStatusWithLiveUsageSyncsLedger(t *testing.T) {
	remaining := 0
	limit := 250
	registry := NewRegistry()
	_ = registry.Register(&remoteUsageFakeProvider{
		name:  "firecrawl",
		caps:  []Capability{CapabilitySearch, CapabilityStatus},
		usage: ProviderUsage{Provider: "firecrawl", Source: "test_remote", RemainingCalls: &remaining, LimitCalls: &limit},
	})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassFreeTierBYOK, FreeRemaining: 250, FreeQuota: 250, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	svc := NewService(registry, ledger, DefaultRouteMatrix())

	resp := svc.ProviderStatusWithOptions(context.Background(), ProviderStatusOptions{LiveUsage: true, SyncLedger: true})
	if len(resp.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(resp.Providers))
	}
	status := resp.Providers[0]
	if status.RemoteUsage == nil || status.RemoteUsage.RemainingCalls == nil || *status.RemoteUsage.RemainingCalls != 0 {
		t.Fatalf("remote usage not surfaced: %#v", status.RemoteUsage)
	}
	if status.FreeRemaining != 0 || status.PolicyReason != "free_quota_exhausted" || status.AllowedByPolicy {
		t.Fatalf("status did not use synced ledger: %#v", status)
	}
	if decision := ledger.Decide("firecrawl"); decision.Allowed || decision.FreeRemaining != 0 {
		t.Fatalf("ledger not synced to remote exhaustion: %#v", decision)
	}
}

func TestRouteSyncsRemoteUsageBeforeProviderCall(t *testing.T) {
	remaining := 0
	limit := 250
	registry := NewRegistry()
	blocked := &remoteUsageFakeProvider{
		name:  "firecrawl",
		caps:  []Capability{CapabilitySearch, CapabilityStatus},
		usage: ProviderUsage{Provider: "firecrawl", Source: "test_remote", RemainingCalls: &remaining, LimitCalls: &limit},
	}
	_ = registry.Register(blocked)
	_ = registry.Register(&remoteUsageFakeProvider{name: "ddgs", caps: []Capability{CapabilitySearch, CapabilityStatus}, results: []SearchResult{{Title: "Fallback", URL: "https://example.com", Snippet: "ok", Provider: "ddgs"}}})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassFreeTierBYOK, FreeRemaining: 250, FreeQuota: 250, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	svc := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"firecrawl", "ddgs"}})

	resp, err := svc.Search(context.Background(), SearchRequest{Query: "nole", Task: TaskGeneral, Limit: 1})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if blocked.searchCalls != 0 {
		t.Fatalf("remote-exhausted provider was called %d times", blocked.searchCalls)
	}
	if resp.Provider != "ddgs" {
		t.Fatalf("provider = %q, want ddgs fallback", resp.Provider)
	}
	if resp.RouteTrace[0].Provider != "firecrawl" || resp.RouteTrace[0].Reason != "free_quota_exhausted" {
		t.Fatalf("expected firecrawl skipped as exhausted, trace=%#v", resp.RouteTrace)
	}
}

func TestRouteDoesNotPreSyncPremiumProviderBeforePolicySkip(t *testing.T) {
	remaining := 0
	limit := 250
	registry := NewRegistry()
	premium := &remoteUsageFakeProvider{
		name:  "firecrawl",
		caps:  []Capability{CapabilitySearch, CapabilityStatus},
		usage: ProviderUsage{Provider: "firecrawl", Source: "test_remote", RemainingCalls: &remaining, LimitCalls: &limit},
	}
	fallback := &remoteUsageFakeProvider{name: "ddgs", caps: []Capability{CapabilitySearch, CapabilityStatus}, results: []SearchResult{{Title: "Fallback", URL: "https://example.com", Snippet: "ok", Provider: "ddgs"}}}
	_ = registry.Register(premium)
	_ = registry.Register(fallback)
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyFreeFirst})
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})
	ledger.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	svc := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"firecrawl", "ddgs"}})

	resp, err := svc.Search(context.Background(), SearchRequest{Query: "nole", Task: TaskGeneral, Limit: 1})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if premium.usageCalls != 0 {
		t.Fatalf("premium provider pre-route usage queried %d times before local policy skip", premium.usageCalls)
	}
	if premium.searchCalls != 0 {
		t.Fatalf("premium provider was searched despite free-first policy: %d", premium.searchCalls)
	}
	if resp.Provider != "ddgs" {
		t.Fatalf("provider = %q, want ddgs fallback", resp.Provider)
	}
	if resp.RouteTrace[0].Reason != "premium_blocked_free_first" {
		t.Fatalf("expected premium skip trace, got %#v", resp.RouteTrace)
	}
}

func TestProviderStatusLiveUsageFailureIsAdvisory(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&remoteUsageFakeProvider{
		name:     "firecrawl",
		caps:     []Capability{CapabilitySearch, CapabilityStatus},
		usageErr: errors.New("remote usage unavailable"),
	})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassFreeTierBYOK, FreeRemaining: 3, FreeQuota: 250, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	svc := NewService(registry, ledger, DefaultRouteMatrix())

	resp := svc.ProviderStatusWithOptions(context.Background(), ProviderStatusOptions{LiveUsage: true, SyncLedger: true})
	status := resp.Providers[0]
	if status.RemoteUsageError == "" {
		t.Fatalf("expected advisory remote usage error, got %#v", status)
	}
	if status.FreeRemaining != 3 || !status.AllowedByPolicy {
		t.Fatalf("usage failure should not mutate/block local ledger: %#v", status)
	}
}

func TestProviderStatusLiveUsageSkipsNonBYOKProviders(t *testing.T) {
	registry := NewRegistry()
	keyless := &remoteUsageFakeProvider{
		name:     "firecrawl",
		caps:     []Capability{CapabilitySearch, CapabilityStatus},
		usageErr: errors.New("should not be called"),
	}
	_ = registry.Register(keyless)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassKeylessFree, KeylessFree: true})
	svc := NewService(registry, ledger, DefaultRouteMatrix())

	resp := svc.ProviderStatusWithOptions(context.Background(), ProviderStatusOptions{LiveUsage: true, SyncLedger: true})
	status := resp.Providers[0]
	if keyless.usageCalls != 0 {
		t.Fatalf("keyless provider usage queried %d times", keyless.usageCalls)
	}
	if status.RemoteUsageError != "" || status.RemoteUsage != nil {
		t.Fatalf("keyless provider should not surface remote usage fields: %#v", status)
	}
	if status.RemoteUsageStrategy != RemoteUsageStrategyNotApplicable {
		t.Fatalf("keyless strategy = %q, want %q", status.RemoteUsageStrategy, RemoteUsageStrategyNotApplicable)
	}
}

func TestProviderStatusUsageStrategyCoversEveryRegisteredProvider(t *testing.T) {
	remaining := 9
	limit := 100
	registry := NewRegistry()
	for _, provider := range []Provider{
		fakeProvider{name: "arxiv"},
		fakeProvider{name: "brave"},
		fakeProvider{name: "ddgs"},
		&remoteUsageFakeProvider{name: "firecrawl", caps: []Capability{CapabilitySearch, CapabilityExtract, CapabilityStatus}, usage: ProviderUsage{Provider: "firecrawl", Source: "test_remote", RemainingCalls: &remaining, LimitCalls: &limit}},
		fakeProvider{name: "httpfetch"},
		fakeProvider{name: "scrapling"},
		&remoteUsageFakeProvider{name: "tavily", caps: []Capability{CapabilitySearch, CapabilityExtract, CapabilityStatus}, usage: ProviderUsage{Provider: "tavily", Source: "test_remote", RemainingCalls: &remaining, LimitCalls: &limit}},
		fakeProvider{name: "wikipedia"},
	} {
		if err := registry.Register(provider); err != nil {
			t.Fatalf("register %s: %v", provider.Name(), err)
		}
	}
	ledger := NewMemoryQuotaLedger()
	for _, name := range []string{"arxiv", "ddgs", "httpfetch", "scrapling", "wikipedia"} {
		ledger.Set(QuotaEntry{Provider: name, CostClass: CostClassKeylessFree, KeylessFree: true})
	}
	for _, name := range []string{"brave", "firecrawl", "tavily"} {
		ledger.Set(QuotaEntry{Provider: name, CostClass: CostClassFreeTierBYOK, FreeRemaining: 10, FreeQuota: 10, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	}
	svc := NewService(registry, ledger, DefaultRouteMatrix())

	resp := svc.ProviderStatusWithOptions(context.Background(), ProviderStatusOptions{LiveUsage: true, SyncLedger: true})
	got := map[string]ProviderStatus{}
	for _, status := range resp.Providers {
		got[status.Name] = status
		if status.RemoteUsageStrategy == "" {
			t.Fatalf("provider %s has empty remote usage strategy: %#v", status.Name, status)
		}
	}
	expected := map[string]string{
		"arxiv":     RemoteUsageStrategyNotApplicable,
		"brave":     RemoteUsageStrategyResponseHeaders,
		"ddgs":      RemoteUsageStrategyNotApplicable,
		"firecrawl": RemoteUsageStrategyAccountEndpoint,
		"httpfetch": RemoteUsageStrategyNotApplicable,
		"scrapling": RemoteUsageStrategyNotApplicable,
		"tavily":    RemoteUsageStrategyAccountEndpoint,
		"wikipedia": RemoteUsageStrategyNotApplicable,
	}
	for name, strategy := range expected {
		status, ok := got[name]
		if !ok {
			t.Fatalf("missing provider status for %s", name)
		}
		if status.RemoteUsageStrategy != strategy {
			t.Fatalf("%s strategy = %q, want %q (status=%#v)", name, status.RemoteUsageStrategy, strategy, status)
		}
	}
	if got["firecrawl"].RemoteUsage == nil || got["tavily"].RemoteUsage == nil {
		t.Fatalf("account-backed providers should surface live remote usage: firecrawl=%#v tavily=%#v", got["firecrawl"].RemoteUsage, got["tavily"].RemoteUsage)
	}
	if got["brave"].RemoteUsage != nil || got["brave"].RemoteUsageError != "" {
		t.Fatalf("brave should advertise header-only usage without preflight query: %#v", got["brave"])
	}
}
