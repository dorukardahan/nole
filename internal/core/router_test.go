package core

import "testing"

type denyNoReasonLedger struct{ *MemoryQuotaLedger }

func (d denyNoReasonLedger) Allow(string) bool { return false }
func (d denyNoReasonLedger) Decide(string) QuotaDecision {
	return QuotaDecision{Allowed: false}
}

type countingLedger struct {
	*MemoryQuotaLedger
	decisions []string
}

func (c *countingLedger) Decide(provider string) QuotaDecision {
	c.decisions = append(c.decisions, provider)
	return c.MemoryQuotaLedger.Decide(provider)
}

type denyFirstNoReasonLedger struct{ *MemoryQuotaLedger }

func (d denyFirstNoReasonLedger) Allow(provider string) bool { return d.Decide(provider).Allowed }
func (d denyFirstNoReasonLedger) Decide(provider string) QuotaDecision {
	if provider == "blocked" {
		return QuotaDecision{Provider: provider, Allowed: false}
	}
	return d.MemoryQuotaLedger.Decide(provider)
}

func TestRouterSelectDoesNotDecidePastFirstRoutableProvider(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := &countingLedger{MemoryQuotaLedger: NewMemoryQuotaLedger()}
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	router := NewRouter(registry, ledger, RouteMatrix{TaskGeneral: {"brave", "tavily"}})

	provider, _, err := router.Select(TaskGeneral, CapabilitySearch)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "brave" {
		t.Fatalf("expected brave, got %q", provider.Name())
	}
	if len(ledger.decisions) != 1 || ledger.decisions[0] != "brave" {
		t.Fatalf("Select should decide lazily until first routable provider, got decisions %#v", ledger.decisions)
	}
}

func TestServiceSearchDoesNotDecidePastSuccessfulProvider(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "tavily"})
	ledger := &countingLedger{MemoryQuotaLedger: NewMemoryQuotaLedger()}
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	ledger.Set(QuotaEntry{Provider: "tavily", FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"brave", "tavily"}})

	resp, err := service.Search(t.Context(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "brave" {
		t.Fatalf("expected brave, got %q", resp.Provider)
	}
	if len(ledger.decisions) != 1 || ledger.decisions[0] != "brave" {
		t.Fatalf("Service should decide lazily until the successful provider, got decisions %#v", ledger.decisions)
	}
}

func TestServiceExtractDoesNotDecidePastSuccessfulProvider(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "scrapling"})
	_ = registry.Register(fakeProvider{name: "firecrawl"})
	ledger := &countingLedger{MemoryQuotaLedger: NewMemoryQuotaLedger()}
	ledger.Set(QuotaEntry{Provider: "scrapling", KeylessFree: true, Unknown: true})
	ledger.Set(QuotaEntry{Provider: "firecrawl", FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskExtract: {"scrapling", "firecrawl"}})

	resp, err := service.Extract(t.Context(), ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "scrapling" {
		t.Fatalf("expected scrapling, got %q", resp.Provider)
	}
	if len(ledger.decisions) != 1 || ledger.decisions[0] != "scrapling" {
		t.Fatalf("Service Extract should decide lazily until the successful provider, got decisions %#v", ledger.decisions)
	}
}

func TestServiceSearchPreservesEmptyQuotaDenyReason(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "blocked"})
	_ = registry.Register(fakeProvider{name: "ok"})
	ledger := denyFirstNoReasonLedger{MemoryQuotaLedger: NewMemoryQuotaLedger()}
	ledger.Set(QuotaEntry{Provider: "ok", FreeRemaining: 1})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"blocked", "ok"}})

	resp, err := service.Search(t.Context(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "ok" || len(resp.RouteTrace) < 2 {
		t.Fatalf("expected ok fallback with skipped trace, got %#v", resp)
	}
	if got := resp.RouteTrace[0]; got.Provider != "blocked" || got.Status != "skipped" || got.Reason != "" {
		t.Fatalf("empty quota deny reason should be preserved in route trace, got %#v", got)
	}
}

func TestRouterCandidatesDoNotTreatEmptyDenyReasonAsRoutable(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "blocked"})
	router := NewRouter(registry, denyNoReasonLedger{NewMemoryQuotaLedger()}, RouteMatrix{TaskGeneral: {"blocked"}})

	candidates := router.Candidates(TaskGeneral, CapabilitySearch)
	if len(candidates) != 1 {
		t.Fatalf("candidates len=%d, want 1", len(candidates))
	}
	if got := candidates[0]; got.Routable || !got.DecisionChecked || got.Decision.Allowed {
		t.Fatalf("denied provider must stay skipped even when ledger omits a reason: %#v", got)
	}
	if provider, _, err := router.Select(TaskGeneral, CapabilitySearch); err == nil || provider != nil || !IsNoFreeQuota(err) {
		t.Fatalf("Select must not return denied provider; provider=%v err=%v", provider, err)
	}
}

func TestRouterCandidatesExposeRouteGates(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "brave"})
	_ = registry.Register(fakeProvider{name: "ddgs"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 0})
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	router := NewRouter(registry, ledger, RouteMatrix{TaskGeneral: {"missing", "brave", "ddgs"}})

	candidates := router.Candidates(TaskDocs, CapabilitySearch)
	if len(candidates) != 3 {
		t.Fatalf("candidates len=%d, want 3 (%#v)", len(candidates), candidates)
	}
	if got := candidates[0]; got.Name != "missing" || got.Provider != nil || got.SkipReason != "not_registered" || got.DecisionChecked || got.Routable {
		t.Fatalf("unexpected missing-provider candidate: %#v", got)
	}
	if got := candidates[1]; got.Name != "brave" || got.Provider == nil || got.SkipReason != "free_quota_exhausted" || !got.DecisionChecked || got.Decision.Allowed || got.Routable {
		t.Fatalf("unexpected quota-blocked candidate: %#v", got)
	}
	if got := candidates[2]; got.Name != "ddgs" || got.Provider == nil || got.SkipReason != "" || !got.DecisionChecked || !got.Decision.Allowed || !got.Routable {
		t.Fatalf("unexpected routable candidate: %#v", got)
	}
}

func TestRouterRouteReturnsDefensiveFallbackCopy(t *testing.T) {
	router := NewRouter(NewRegistry(), NewMemoryQuotaLedger(), RouteMatrix{TaskGeneral: {"brave", "ddgs"}})
	route := router.Route(TaskDocs)
	if len(route) != 2 || route[0] != "brave" || route[1] != "ddgs" {
		t.Fatalf("unknown task should fall back to general route, got %#v", route)
	}
	route[0] = "mutated"
	again := router.Route(TaskDocs)
	if again[0] != "brave" {
		t.Fatalf("Route must return a defensive copy, got %#v after caller mutation", again)
	}
}

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
	if len(route) != 4 || route[0] != "scrapling" || route[1] != "firecrawl" || route[2] != "tavily" || route[3] != "httpfetch" {
		t.Fatalf("extract route should prefer local Scrapling, then remote fallbacks, then the keyless httpfetch backstop, got %#v", route)
	}
}

func TestRouterExtractFallsBackToKeylessHTTPFetch(t *testing.T) {
	// With no local Scrapling and no keyed remote extractors registered, extract
	// must still resolve — to the keyless httpfetch last-resort backstop. This is
	// the gateway's "extract works out of the box" guarantee.
	registry := NewRegistry()
	_ = registry.Register(fakeProvider{name: "httpfetch"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "httpfetch", CostClass: CostClassKeylessFree, KeylessFree: true})
	router := NewRouter(registry, ledger, DefaultRouteMatrix())

	provider, route, err := router.Select(TaskExtract, CapabilityExtract)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if provider.Name() != "httpfetch" {
		t.Fatalf("expected the keyless httpfetch backstop, got %q with route %#v", provider.Name(), route)
	}
	// httpfetch is last in the route — reached only after the better providers.
	if route[len(route)-1] != "httpfetch" {
		t.Fatalf("httpfetch must be the LAST-resort extract provider, got route %#v", route)
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
		TaskAcademic:  {"tavily", "firecrawl", "brave", "arxiv", "wikipedia", "ddgs"},
		TaskFactcheck: {"firecrawl", "tavily", "brave", "wikipedia", "ddgs"},
		TaskSemantic:  {"tavily", "brave", "firecrawl", "ddgs"},
		TaskCode:      {"tavily", "firecrawl", "brave", "ddgs"},
		TaskSocial:    {"firecrawl", "tavily", "brave", "ddgs"},
		TaskPeople:    {"firecrawl", "brave", "tavily", "wikipedia", "ddgs"},
		TaskPricing:   {"firecrawl", "brave", "tavily", "ddgs"},
		TaskResearch:  {"firecrawl", "tavily", "brave", "ddgs"},
		TaskExtract:   {"scrapling", "firecrawl", "tavily", "httpfetch"},
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

func TestRouterAllSearchRoutesEndWithDDGS(t *testing.T) {
	matrix := DefaultRouteMatrix()
	knownTasks := TaskTypes()
	seen := make(map[TaskType]bool, len(knownTasks))

	for _, task := range knownTasks {
		if seen[task] {
			t.Fatalf("TaskTypes contains duplicate task %q", task)
		}
		seen[task] = true

		route, ok := matrix[task]
		if !ok {
			t.Fatalf("known task %q is missing from the default route catalog", task)
		}
		if task == TaskExtract {
			// Extraction has its own httpfetch backstop and is intentionally outside
			// the DDGS invariant that applies to search routes.
			continue
		}

		ddgsCount := 0
		for _, provider := range route {
			if provider == "ddgs" {
				ddgsCount++
			}
		}
		if ddgsCount != 1 {
			t.Errorf("search route %q contains DDGS %d times, want exactly once: %#v", task, ddgsCount, route)
			continue
		}
		if len(route) == 0 || route[len(route)-1] != "ddgs" {
			t.Errorf("search route %q must end with DDGS: %#v", task, route)
		}
	}

	for task := range matrix {
		if !seen[task] {
			t.Errorf("default route catalog contains task %q missing from TaskTypes", task)
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
