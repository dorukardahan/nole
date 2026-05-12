package core

import "testing"

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
