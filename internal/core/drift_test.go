package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

// errSearchProvider is a search-capable provider whose Search always fails with
// a fixed error, so we can drive the service's error path deterministically.
type errSearchProvider struct {
	fakeProvider
	err error
}

func (p errSearchProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, p.err
}

func newDriftService(p Provider, freeRemaining int) (*Service, *MemoryQuotaLedger) {
	registry := NewRegistry()
	_ = registry.Register(p)
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{
		Provider:      p.Name(),
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: freeRemaining,
		FreeQuota:     1000,
		EstimateOnly:  true,
	})
	return NewService(registry, ledger, RouteMatrix{TaskGeneral: {p.Name()}}), ledger
}

func wrapped429() error {
	return fmt.Errorf("p: search request failed: %w", providerhttp.NewHTTPStatusError("p", "search", 429, nil))
}

func TestIsQuotaExhausted(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare 429", providerhttp.NewHTTPStatusError("p", "search", 429, nil), true},
		{"wrapped 429", wrapped429(), true},
		{"500 is not quota", providerhttp.NewHTTPStatusError("p", "search", 500, nil), false},
		{"transport error", errors.New("dial tcp: connection refused"), false},
		// Once repeated 429s trip the breaker, calls short-circuit with
		// ErrCircuitOpen (not a 429), so drift correctly stops firing.
		{"circuit open is not a 429", providerhttp.ErrCircuitOpen, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isQuotaExhausted(c.err); got != c.want {
				t.Fatalf("isQuotaExhausted(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestSearch429WithRoomRecordsDriftNoDebit(t *testing.T) {
	p := errSearchProvider{fakeProvider: fakeProvider{name: "p"}, err: wrapped429()}
	svc, ledger := newDriftService(p, 500)

	if _, err := svc.Search(context.Background(), SearchRequest{Query: "q", Task: TaskGeneral, Limit: 3}); err == nil {
		t.Fatal("expected search to fail: the only provider 429s")
	}

	b := ledger.BudgetStatus()
	if !b.HasDrift || len(b.DriftSignals) != 1 || b.DriftSignals[0].Provider != "p" {
		t.Fatalf("expected one drift signal for p, got HasDrift=%v signals=%#v", b.HasDrift, b.DriftSignals)
	}
	if got, _ := ledger.Get("p"); got.FreeRemaining != 500 {
		t.Fatalf("a 429 must NOT debit free quota: FreeRemaining=%d, want 500", got.FreeRemaining)
	}
}

func TestSearch429AfterExhaustionNoDrift(t *testing.T) {
	p := errSearchProvider{fakeProvider: fakeProvider{name: "p"}, err: wrapped429()}
	svc, ledger := newDriftService(p, 0) // exhausted → Decide blocks before the call

	_, _ = svc.Search(context.Background(), SearchRequest{Query: "q", Task: TaskGeneral, Limit: 3})

	if ledger.BudgetStatus().HasDrift {
		t.Fatal("an exhausted counter blocks the call before it runs; no drift expected")
	}
}

func TestSearchCircuitOpenRecordsNoDrift(t *testing.T) {
	p := errSearchProvider{fakeProvider: fakeProvider{name: "p"}, err: providerhttp.ErrCircuitOpen}
	svc, ledger := newDriftService(p, 500)

	_, _ = svc.Search(context.Background(), SearchRequest{Query: "q", Task: TaskGeneral, Limit: 3})

	if ledger.BudgetStatus().HasDrift {
		t.Fatal("ErrCircuitOpen is not a 429; drift must not fire once the breaker takes over")
	}
}

func TestProviderStatusShowsDriftWarning(t *testing.T) {
	p := errSearchProvider{fakeProvider: fakeProvider{name: "p"}, err: wrapped429()}
	svc, _ := newDriftService(p, 500)
	_, _ = svc.Search(context.Background(), SearchRequest{Query: "q", Task: TaskGeneral, Limit: 3})

	resp := svc.ProviderStatus(context.Background())
	var found bool
	for _, ps := range resp.Providers {
		if ps.Name == "p" {
			found = true
			if ps.DriftWarning == "" {
				t.Fatalf("expected DriftWarning on provider p, got empty")
			}
		}
	}
	if !found {
		t.Fatal("provider p not present in provider_status")
	}
}

// Drift must never feed the routing gate. The Router consults ledger.Decide;
// recording drift must leave that decision (Allowed + FreeRemaining) untouched.
func TestDriftDoesNotAffectQuotaDecision(t *testing.T) {
	l := NewMemoryQuotaLedger()
	l.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000})

	before := l.Decide("brave")
	l.RecordDrift("brave", "429 while room")
	after := l.Decide("brave")

	if before.Allowed != after.Allowed || before.FreeRemaining != after.FreeRemaining {
		t.Fatalf("drift changed the quota decision the router consults: before=%+v after=%+v", before, after)
	}
}

func TestDriftSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	seeds := []QuotaEntry{{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000}}

	l1, err := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seeds)
	if err != nil {
		t.Fatalf("ledger 1: %v", err)
	}
	l1.RecordDrift("brave", "provider returned 429 while local free_remaining > 0")

	l2, err := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seeds)
	if err != nil {
		t.Fatalf("ledger 2: %v", err)
	}
	b := l2.BudgetStatus()
	if !b.HasDrift || len(b.DriftSignals) != 1 || b.DriftSignals[0].Provider != "brave" {
		t.Fatalf("drift signal did not survive reload: %#v", b.DriftSignals)
	}
}

// Two processes recording drift for DIFFERENT providers must not clobber each
// other: the reload-merge unions on-disk and in-memory signals (R6).
func TestDriftUnionMergeAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	seeds := []QuotaEntry{
		{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000},
		{Provider: "tavily", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000},
	}
	l1, _ := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seeds)
	l1.RecordDrift("brave", "429 brave")

	l2, _ := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seeds)
	l2.RecordDrift("tavily", "429 tavily") // reload-merges brave, adds tavily

	l3, _ := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seeds)
	b := l3.BudgetStatus()
	if len(b.DriftSignals) != 2 {
		t.Fatalf("expected both providers' drift signals to survive the union-merge, got %#v", b.DriftSignals)
	}
}

func TestDriftAgesOut24h(t *testing.T) {
	prev := nowUTC
	defer func() { nowUTC = prev }()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	nowUTC = func() time.Time { return base }

	l := NewMemoryQuotaLedger()
	l.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000})
	l.RecordDrift("brave", "429")

	if !l.BudgetStatus().HasDrift {
		t.Fatal("a fresh drift signal should be surfaced")
	}
	nowUTC = func() time.Time { return base.Add(25 * time.Hour) }
	if l.BudgetStatus().HasDrift {
		t.Fatal("a 25h-old drift signal must age out of output")
	}
}

func TestRecordDriftConcurrent(t *testing.T) {
	l := NewMemoryQuotaLedger()
	l.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000})
	l.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassFreeTierBYOK, FreeRemaining: 500, FreeQuota: 1000})

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "brave"
			if i%2 == 1 {
				name = "tavily"
			}
			l.RecordDrift(name, "429")
		}(i)
	}
	wg.Wait()

	b := l.BudgetStatus()
	if !b.HasDrift || len(b.DriftSignals) != 2 {
		t.Fatalf("expected 2 drift signals (keyed by provider) after concurrent writes, got %#v", b.DriftSignals)
	}
}
