package core

import "testing"

// A future-dated PeriodStart (clock skew, or a ledger copied from a host whose
// clock was ahead) must self-heal: the monthly refresh previously fired only
// when PeriodStart < now, so a future PeriodStart left a provider stranded as
// permanently exhausted. Refresh must also fire when PeriodStart is in the
// future, resetting to the current period.
func TestDecideHealsFutureDatedPeriodStart(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 0,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   "2999-12", // far future relative to the real clock
	})

	decision := ledger.Decide("tavily")
	if !decision.Allowed {
		t.Fatalf("provider should be allowed after self-healing a future-dated PeriodStart, got %+v", decision)
	}
	got, _ := ledger.Get("tavily")
	if got.FreeRemaining != 1000 {
		t.Fatalf("future-dated PeriodStart should refresh to full quota, FreeRemaining = %d, want 1000", got.FreeRemaining)
	}
}

// Regression guard: an entry already in the current period must NOT be reset.
func TestDecideDoesNotResetCurrentPeriod(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 3,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   CurrentMonthISO(),
	})
	_ = ledger.Decide("tavily")
	got, _ := ledger.Get("tavily")
	if got.FreeRemaining != 3 {
		t.Fatalf("current-period entry must not be refreshed, FreeRemaining = %d, want 3", got.FreeRemaining)
	}
}
