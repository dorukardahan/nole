package core

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// recordLocked snapshots the entry before mutating and restores it if the
// atomic persist fails, then fails the ledger closed. These regression tests
// inject a persist failure via the atomicWriteFile seam and assert that:
//   - Record surfaces the "persist quota ledger: unavailable" error,
//   - the in-memory counter is rolled back (no over- or under-count),
//   - the ledger is fail-closed (denies the provider until a clean reload),
//   - disk never received the mutation (a fresh reload matches the rolled-back
//     value — memory matched disk).
func TestFileQuotaLedgerRecordRollsBackFreeRemainingOnPersistFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := DefaultQuotaPolicy()
	seed := []QuotaEntry{{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 5, FreeQuota: 5, RefreshWindow: RefreshMonthly, PeriodStart: CurrentMonthISO()}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	if got, _ := ledger.Get("brave"); got.FreeRemaining != 5 {
		t.Fatalf("seeded FreeRemaining = %d, want 5", got.FreeRemaining)
	}

	restore := atomicWriteFile
	t.Cleanup(func() { atomicWriteFile = restore })
	atomicWriteFile = func(string, []byte) error { return errors.New("disk full") }

	err = ledger.Record("brave")
	if err == nil || !strings.Contains(err.Error(), "persist quota ledger: unavailable") {
		t.Fatalf("Record() err = %v, want 'persist quota ledger: unavailable'", err)
	}
	if got, _ := ledger.Get("brave"); got.FreeRemaining != 5 {
		t.Fatalf("after failed persist, FreeRemaining = %d, want 5 (rolled back, not 4)", got.FreeRemaining)
	}
	if d := ledger.Decide("brave"); d.Allowed || d.Reason != "ledger_unavailable_fail_closed" {
		t.Fatalf("after failed persist, Decide = %#v, want denied with ledger_unavailable_fail_closed", d)
	}

	atomicWriteFile = restore
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if got, _ := reloaded.Get("brave"); got.FreeRemaining != 5 {
		t.Fatalf("reloaded FreeRemaining = %d, want 5 (disk never decremented)", got.FreeRemaining)
	}
}

func TestFileQuotaLedgerRecordRollsBackSpentCentsOnPersistFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 100}
	seed := []QuotaEntry{{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	if got, _ := ledger.Get("tavily"); got.SpentCents != 0 {
		t.Fatalf("seeded SpentCents = %d, want 0", got.SpentCents)
	}

	restore := atomicWriteFile
	t.Cleanup(func() { atomicWriteFile = restore })
	atomicWriteFile = func(string, []byte) error { return errors.New("disk full") }

	err = ledger.Record("tavily")
	if err == nil || !strings.Contains(err.Error(), "persist quota ledger: unavailable") {
		t.Fatalf("Record() err = %v, want 'persist quota ledger: unavailable'", err)
	}
	if got, _ := ledger.Get("tavily"); got.SpentCents != 0 {
		t.Fatalf("after failed persist, SpentCents = %d, want 0 (rolled back, not 2)", got.SpentCents)
	}
	if d := ledger.Decide("tavily"); d.Allowed || d.Reason != "ledger_unavailable_fail_closed" {
		t.Fatalf("after failed persist, Decide = %#v, want denied with ledger_unavailable_fail_closed", d)
	}

	atomicWriteFile = restore
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if got, _ := reloaded.Get("tavily"); got.SpentCents != 0 {
		t.Fatalf("reloaded SpentCents = %d, want 0 (disk never charged)", got.SpentCents)
	}
}
