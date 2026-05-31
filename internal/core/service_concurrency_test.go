package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// instrumentedProvider counts Search/Extract invocations and (optionally) blocks
// each call until release is closed, so a test can hold the leader in-flight
// while concurrent callers pile up behind singleflight. Pointer receiver: it
// carries atomics and a channel.
type instrumentedProvider struct {
	name      string
	calls     atomic.Int64
	lastLimit atomic.Int64
	release   chan struct{}
	resultsN  int
}

func (p *instrumentedProvider) Name() string { return p.name }

func (p *instrumentedProvider) Capabilities() []Capability {
	return []Capability{CapabilitySearch, CapabilityExtract, CapabilityStatus}
}

func (p *instrumentedProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	p.calls.Add(1)
	p.lastLimit.Store(int64(req.Limit))
	if p.release != nil {
		<-p.release
	}
	n := p.resultsN
	if n == 0 {
		n = 1
	}
	results := make([]SearchResult, 0, n)
	for i := 0; i < n; i++ {
		results = append(results, SearchResult{Title: "t", URL: "https://example.com", Snippet: "s", Provider: p.name})
	}
	return SearchResponse{Query: req.Query, Task: req.Task, Provider: p.name, Results: results}, nil
}

func (p *instrumentedProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	p.calls.Add(1)
	if p.release != nil {
		<-p.release
	}
	return ExtractResponse{URL: req.URL, Provider: p.name, Content: "content"}, nil
}

func (p *instrumentedProvider) Status(ctx context.Context) ProviderStatus {
	return ProviderStatus{Name: p.name, Available: true, Capabilities: p.Capabilities()}
}

func freeTierLedger(provider string, free int) *MemoryQuotaLedger {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{
		Provider:      provider,
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: free,
		FreeQuota:     free,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   CurrentMonthISO(),
	})
	return ledger
}

func TestServiceSearchCoalescesConcurrentIdenticalQueries(t *testing.T) {
	// Cache-miss stampede guard: N goroutines issuing the same query must
	// collapse to a single upstream fetch and a single quota debit, instead of
	// N fetches + N debits that defeat the free-tier cap.
	prov := &instrumentedProvider{name: "tavily", release: make(chan struct{}), resultsN: 2}
	registry := NewRegistry()
	_ = registry.Register(prov)
	ledger := freeTierLedger("tavily", 10)
	cache := NewMemoryResponseCache(5 * time.Minute)
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"tavily"}}, WithResponseCache(cache))

	const N = 16
	var wg sync.WaitGroup
	errs := make([]error, N)
	provs := make([]string, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			resp, err := service.Search(context.Background(), SearchRequest{Query: "same query", Task: TaskGeneral, Limit: 5})
			errs[i] = err
			provs[i] = resp.Provider
		}(i)
	}
	// Wait for the leader to enter the provider, then let followers queue
	// behind singleflight before releasing the leader.
	deadline := time.Now().Add(2 * time.Second)
	for prov.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(prov.release)
	wg.Wait()

	if got := prov.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 provider call for %d concurrent identical queries, got %d", N, got)
	}
	entry, _ := ledger.Get("tavily")
	if entry.FreeRemaining != 9 {
		t.Fatalf("expected exactly 1 quota debit (FreeRemaining 10->9), got %d", entry.FreeRemaining)
	}
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d errored: %v", i, errs[i])
		}
		if provs[i] != "tavily" {
			t.Fatalf("goroutine %d got provider %q, want tavily", i, provs[i])
		}
	}
}

func TestServiceSearchDistinctQueriesNotCoalesced(t *testing.T) {
	// Distinct queries must each run: singleflight keys on the cache key, so it
	// must not over-collapse different work.
	prov := &instrumentedProvider{name: "tavily", resultsN: 1}
	registry := NewRegistry()
	_ = registry.Register(prov)
	service := NewService(registry, freeTierLedger("tavily", 10), RouteMatrix{TaskGeneral: {"tavily"}})

	for i := 0; i < 3; i++ {
		if _, err := service.Search(context.Background(), SearchRequest{Query: fmt.Sprintf("q%d", i), Task: TaskGeneral, Limit: 5}); err != nil {
			t.Fatalf("search %d failed: %v", i, err)
		}
	}
	if got := prov.calls.Load(); got != 3 {
		t.Fatalf("expected 3 provider calls for 3 distinct queries, got %d", got)
	}
}

func TestServiceSearchShortCircuitsCancelledContext(t *testing.T) {
	// A cancelled caller must surface context.Canceled and never probe a
	// provider, instead of walking the whole route and returning the last
	// provider's context error.
	prov := &instrumentedProvider{name: "tavily", resultsN: 1}
	registry := NewRegistry()
	_ = registry.Register(prov)
	service := NewService(registry, freeTierLedger("tavily", 10), RouteMatrix{TaskGeneral: {"tavily"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Search(ctx, SearchRequest{Query: "x", Task: TaskGeneral, Limit: 5})
	if err == nil {
		t.Fatal("expected a context error on a cancelled request")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := prov.calls.Load(); got != 0 {
		t.Fatalf("expected no provider calls on cancelled context, got %d", got)
	}
}

func TestServiceExtractShortCircuitsCancelledContext(t *testing.T) {
	prov := &instrumentedProvider{name: "tavily", resultsN: 1}
	registry := NewRegistry()
	_ = registry.Register(prov)
	service := NewService(registry, freeTierLedger("tavily", 10), RouteMatrix{TaskExtract: {"tavily"}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Extract(ctx, ExtractRequest{URL: "https://example.com"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := prov.calls.Load(); got != 0 {
		t.Fatalf("expected no provider calls on cancelled context, got %d", got)
	}
}

// gateProvider blocks the leader inside Search until release is closed, then
// fails — so the route falls through to the next provider. entered is closed
// once, when the (single) leader goroutine enters Search.
type gateProvider struct {
	name    string
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (p *gateProvider) Name() string { return p.name }
func (p *gateProvider) Capabilities() []Capability {
	return []Capability{CapabilitySearch, CapabilityStatus}
}
func (p *gateProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if p.calls.Add(1) == 1 {
		close(p.entered)
	}
	<-p.release
	return SearchResponse{}, errors.New("gate provider failed")
}
func (p *gateProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{}, errors.New("gate provider has no extract")
}
func (p *gateProvider) Status(ctx context.Context) ProviderStatus {
	return ProviderStatus{Name: p.name, Available: true, Capabilities: p.Capabilities()}
}

func TestServiceSearchCoalescedFollowerSurvivesLeaderCancel(t *testing.T) {
	// Regression for the coalescing availability bug: when the leader of a
	// coalesced query cancels mid-flight, a follower with a live context must
	// still get a successful result (its own request did not cancel), not the
	// leader's context.Canceled. Without DoChan + context.WithoutCancel the
	// leader's cancellation was broadcast to every coalesced follower.
	gate := &gateProvider{name: "gate", entered: make(chan struct{}), release: make(chan struct{})}
	ok := &instrumentedProvider{name: "ok", resultsN: 1}
	registry := NewRegistry()
	_ = registry.Register(gate)
	_ = registry.Register(ok)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "gate", FreeRemaining: 5})
	ledger.Set(QuotaEntry{Provider: "ok", CostClass: CostClassFreeTierBYOK, FreeRemaining: 5, FreeQuota: 5, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"gate", "ok"}}, WithResponseCache(NewMemoryResponseCache(5*time.Minute)))

	type result struct {
		resp SearchResponse
		err  error
	}
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderCh := make(chan result, 1)
	go func() {
		r, e := service.Search(leaderCtx, SearchRequest{Query: "hot", Task: TaskGeneral, Limit: 5})
		leaderCh <- result{r, e}
	}()
	<-gate.entered // leader is inside the gate provider; the singleflight key is registered

	followerCh := make(chan result, 1)
	go func() {
		r, e := service.Search(context.Background(), SearchRequest{Query: "hot", Task: TaskGeneral, Limit: 5})
		followerCh <- result{r, e}
	}()
	time.Sleep(50 * time.Millisecond) // let the follower coalesce behind the in-flight leader

	cancelLeader()      // leader disconnects mid-flight
	close(gate.release) // gate fails -> detached fetch falls through to "ok"

	select {
	case fr := <-followerCh:
		if fr.err != nil {
			t.Fatalf("coalesced follower must not inherit the leader's cancellation; got err %v", fr.err)
		}
		if fr.resp.Provider != "ok" {
			t.Fatalf("follower should have fallen through to ok, got provider %q", fr.resp.Provider)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follower did not complete within 5s")
	}
	if ok.calls.Load() < 1 {
		t.Fatalf("ok provider should have been tried by the detached fetch, calls=%d", ok.calls.Load())
	}
	select {
	case lr := <-leaderCh:
		if !errors.Is(lr.err, context.Canceled) {
			t.Fatalf("leader should observe its own cancellation, got %v", lr.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not return within 5s")
	}
}

func TestServiceSearchClampsLimit(t *testing.T) {
	// limit is clamped centrally to [1,maxSearchLimit] so an over-large value
	// can't force a guaranteed provider 422 and a non-positive value can't leak
	// through as "no limit".
	prov := &instrumentedProvider{name: "tavily", resultsN: 1}
	registry := NewRegistry()
	_ = registry.Register(prov)
	service := NewService(registry, freeTierLedger("tavily", 10), RouteMatrix{TaskGeneral: {"tavily"}})

	if _, err := service.Search(context.Background(), SearchRequest{Query: "a", Task: TaskGeneral, Limit: 50}); err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if got := prov.lastLimit.Load(); got != maxSearchLimit {
		t.Fatalf("expected limit clamped to %d, provider saw %d", maxSearchLimit, got)
	}
	if _, err := service.Search(context.Background(), SearchRequest{Query: "b", Task: TaskGeneral, Limit: 0}); err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if got := prov.lastLimit.Load(); got != 5 {
		t.Fatalf("expected non-positive limit defaulted to 5, provider saw %d", got)
	}
}
