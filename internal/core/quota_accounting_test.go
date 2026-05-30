package core

import (
	"context"
	"testing"
)

// When an empty-results provider precedes a successful one, ONLY the
// successful provider's free-tier quota should be debited; the empty provider
// gave no value and must not be charged. The existing fallback test checks the
// route/trace but not the per-provider ledger state — this closes that gap.
func TestServiceFallbackDebitsOnlySuccessfulProviderAfterEmpty(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(emptySearchProvider{fakeProvider{name: "empty"}})
	_ = registry.Register(fakeProvider{name: "brave"})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "empty", CostClass: CostClassFreeTierBYOK, FreeRemaining: 5, FreeQuota: 5, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	ledger.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 5, FreeQuota: 5, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()})
	service := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"empty", "brave"}})

	resp, err := service.Search(context.Background(), SearchRequest{Query: "mcp", Task: TaskGeneral})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "brave" {
		t.Fatalf("expected brave fallback, got %q", resp.Provider)
	}

	gotEmpty, _ := ledger.Get("empty")
	if gotEmpty.FreeRemaining != 5 {
		t.Fatalf("empty-results provider must not be debited, FreeRemaining = %d, want 5", gotEmpty.FreeRemaining)
	}
	gotBrave, _ := ledger.Get("brave")
	if gotBrave.FreeRemaining != 4 {
		t.Fatalf("successful provider must be debited exactly once, FreeRemaining = %d, want 4", gotBrave.FreeRemaining)
	}
}
