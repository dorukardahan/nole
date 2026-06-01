package core

import (
	"strings"
	"testing"
)

// Every BYOK provider must carry honest, populated metering metadata so
// budget_status can be explicit about HOW each provider meters and that the
// local count is an estimate. These are static literals (never runtime-derived).
func TestByokMetadataFieldsPopulated(t *testing.T) {
	for _, p := range BYOKProviders() {
		if p.MeteringModel == "" {
			t.Errorf("%s: MeteringModel must be set", p.Name)
		}
		switch p.MeteringModel {
		case "call-count", "credit-based", "one-time-grant":
		default:
			t.Errorf("%s: unexpected MeteringModel %q", p.Name, p.MeteringModel)
		}
		if !p.EstimateOnly {
			t.Errorf("%s: free-tier counts are an estimate; EstimateOnly must be true", p.Name)
		}
		if strings.TrimSpace(p.RateLimitNote) == "" {
			t.Errorf("%s: RateLimitNote must explain the metering caveat", p.Name)
		}
	}
}

func TestBudgetStatusEstimateNoteWhenEstimateOnly(t *testing.T) {
	l := NewMemoryQuotaLedger()
	l.Set(QuotaEntry{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 1000, FreeQuota: 1000, MeteringModel: "credit-based", EstimateOnly: true})
	if note := l.BudgetStatus().EstimateNote; note == "" {
		t.Fatal("expected an estimate note when a BYOK entry is EstimateOnly")
	}
}

func TestBudgetStatusNoEstimateNoteForKeylessOnly(t *testing.T) {
	l := NewMemoryQuotaLedger()
	l.Set(QuotaEntry{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true})
	if note := l.BudgetStatus().EstimateNote; note != "" {
		t.Fatalf("keyless-only ledger should not carry an estimate note, got %q", note)
	}
}

// BudgetStatus surfaces the policy's HardCapSource verbatim so callers can tell
// an explicit cap apart from an unset cost-capped policy.
func TestBudgetStatusSurfacesHardCapSource(t *testing.T) {
	l := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: CostPolicyCostCapped, HardCapSource: "unset"})
	if got := l.BudgetStatus().HardCapSource; got != "unset" {
		t.Fatalf("HardCapSource = %q, want unset", got)
	}
}
