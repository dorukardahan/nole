package core

import (
	"sync"
	"testing"
)

func TestQuotaAllowsProviderWithRemainingFreeCalls(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 1})
	if !ledger.Allow("brave") {
		t.Fatal("expected brave to be allowed")
	}
}

func TestQuotaBlocksProviderWithZeroRemainingFreeCalls(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 0})
	if ledger.Allow("brave") {
		t.Fatal("expected brave to be blocked")
	}
}

func TestQuotaAllowsKeylessFreeProviderWhenUnknown(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})
	if !ledger.Allow("ddgs") {
		t.Fatal("expected keyless free provider to be allowed")
	}
}

func TestQuotaBlocksUnknownNonKeylessProvider(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	if ledger.Allow("brave") {
		t.Fatal("expected unknown paid provider to be blocked")
	}
}

func TestQuotaRecordDecrementsFreeCalls(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", FreeRemaining: 2})
	if err := ledger.Record("brave"); err != nil {
		t.Fatalf("record failed: %v", err)
	}
	entry, ok := ledger.Get("brave")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.FreeRemaining != 1 {
		t.Fatalf("expected 1 remaining, got %d", entry.FreeRemaining)
	}
}

func TestCostClassStringsMatchPublicContract(t *testing.T) {
	cases := map[ProviderCostClass]string{
		CostClassKeylessFree:    "keyless-free",
		CostClassFreeTierBYOK:   "free-tier-BYOK",
		CostClassPremiumCapable: "premium-capable",
		CostClassUnknownCost:    "unknown-cost",
		CostClassDisabledNoKey:  "disabled-no-key",
	}
	for class, want := range cases {
		if string(class) != want {
			t.Fatalf("cost class %q = %q, want %q", class, string(class), want)
		}
	}
}

func TestQuotaDefaultPolicyIsFreeFirst(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	status := ledger.BudgetStatus()
	if status.Policy != CostPolicyFreeFirst {
		t.Fatalf("default policy = %q, want %q", status.Policy, CostPolicyFreeFirst)
	}
	if !status.NoHiddenPaidSpend {
		t.Fatalf("free-first budget status should advertise no hidden paid spend: %#v", status)
	}
}

func TestQuotaFreeFirstBlocksPremiumCapableProvider(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})

	decision := ledger.Decide("tavily")
	if decision.Allowed {
		t.Fatalf("free-first should not allow premium-capable provider merely because a key exists: %#v", decision)
	}
	if decision.Policy != CostPolicyFreeFirst || decision.CostClass != CostClassPremiumCapable || decision.Reason != "premium_blocked_free_first" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestQuotaFreeFirstAllowsFreeTierWithRemainingQuota(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 1})

	decision := ledger.Decide("brave")
	if !decision.Allowed || decision.Reason != "free_tier_available" {
		t.Fatalf("expected free tier with quota to be allowed, got %#v", decision)
	}
}

func TestQuotaFreeFirstBlocksUnknownCost(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassUnknownCost})

	decision := ledger.Decide("tavily")
	if decision.Allowed || decision.Reason != "unknown_cost_blocked" {
		t.Fatalf("expected unknown cost to fail closed under free-first, got %#v", decision)
	}
}

func TestQuotaCostCappedAllowsPremiumWithinCap(t *testing.T) {
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5})
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2, SpentCents: 2})

	decision := ledger.Decide("firecrawl")
	if !decision.Allowed || decision.Reason != "within_cost_cap" {
		t.Fatalf("expected premium provider within cap to be allowed, got %#v", decision)
	}
	if err := ledger.Record("firecrawl"); err != nil {
		t.Fatalf("record premium usage: %v", err)
	}
	entry, _ := ledger.Get("firecrawl")
	if entry.SpentCents != 4 {
		t.Fatalf("spent cents after record = %d, want 4", entry.SpentCents)
	}
}

func TestQuotaCostCappedBlocksPremiumOverCap(t *testing.T) {
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 3})
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2, SpentCents: 2})

	decision := ledger.Decide("firecrawl")
	if decision.Allowed || decision.Reason != "cost_cap_exceeded" {
		t.Fatalf("expected premium provider over cap to be blocked, got %#v", decision)
	}
}

func TestQuotaCostCappedUsesGlobalSpentAcrossProviders(t *testing.T) {
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 3})
	ledger.Set(QuotaEntry{Provider: "firecrawl", CostClass: CostClassPremiumCapable, EstimatedCostCents: 3})

	if err := ledger.Record("tavily"); err != nil {
		t.Fatalf("record tavily: %v", err)
	}
	decision := ledger.Decide("firecrawl")
	if decision.Allowed || decision.Reason != "cost_cap_exceeded" {
		t.Fatalf("expected global cap to block second provider, got %#v", decision)
	}
	budget := ledger.BudgetStatus()
	if budget.SpentCents != 3 {
		t.Fatalf("spent cents = %d, want 3", budget.SpentCents)
	}
}

func TestQuotaCostCappedRecordIsAtomic(t *testing.T) {
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})

	var wg sync.WaitGroup
	successes := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ledger.Record("tavily"); err == nil {
				successes <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(successes)

	successCount := 0
	for range successes {
		successCount++
	}
	if successCount != 5 {
		t.Fatalf("successful records = %d, want 5", successCount)
	}
	if budget := ledger.BudgetStatus(); budget.SpentCents != 5 {
		t.Fatalf("spent cents = %d, want 5", budget.SpentCents)
	}
}

func TestQuotaQualityFirstAllowsPremiumExplicitly(t *testing.T) {
	ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyQualityFirst})
	ledger.Set(QuotaEntry{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1})

	decision := ledger.Decide("tavily")
	if !decision.Allowed || decision.Reason != "quality_first_allows_premium" {
		t.Fatalf("expected explicit quality-first policy to allow premium-capable provider, got %#v", decision)
	}
}
