package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileQuotaLedgerPersistsPremiumSpendAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5}
	seed := []QuotaEntry{{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	if err := ledger.Record("tavily"); err != nil {
		t.Fatalf("record first premium attempt: %v", err)
	}

	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if got := reloaded.BudgetStatus().SpentCents; got != 2 {
		t.Fatalf("reloaded spent cents = %d, want 2", got)
	}
	if decision := reloaded.Decide("tavily"); !decision.Allowed || decision.Reason != "within_cost_cap" {
		t.Fatalf("second premium attempt should still be inside cap, got %#v", decision)
	}
	if err := reloaded.Record("tavily"); err != nil {
		t.Fatalf("record second premium attempt: %v", err)
	}

	afterSecond, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload after second record: %v", err)
	}
	if got := afterSecond.BudgetStatus().SpentCents; got != 4 {
		t.Fatalf("spent cents after second reload = %d, want 4", got)
	}
	if decision := afterSecond.Decide("tavily"); decision.Allowed || decision.Reason != "cost_cap_exceeded" {
		t.Fatalf("third premium attempt should exceed cap, got %#v", decision)
	}
}

func TestFileQuotaLedgerRecordUsesLatestDiskSpendAcrossStaleInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 3}
	seed := []QuotaEntry{{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 3}}

	ledgerA, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger A: %v", err)
	}
	ledgerB, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger B: %v", err)
	}

	if err := ledgerA.Record("tavily"); err != nil {
		t.Fatalf("first stale instance should record inside cap: %v", err)
	}
	if err := ledgerB.Record("tavily"); err == nil || !strings.Contains(err.Error(), "cost_cap_exceeded") {
		t.Fatalf("second stale instance should re-read disk and fail cap, got err=%v", err)
	}
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if got := reloaded.BudgetStatus().SpentCents; got != 3 {
		t.Fatalf("spent cents after stale-instance race = %d, want 3", got)
	}
}

func TestFileQuotaLedgerRecordUsesLatestDiskFreeQuotaAcrossStaleInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}
	seed := []QuotaEntry{{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 1}}

	ledgerA, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger A: %v", err)
	}
	ledgerB, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger B: %v", err)
	}

	if err := ledgerA.Record("brave"); err != nil {
		t.Fatalf("first stale instance should consume free quota: %v", err)
	}
	if err := ledgerB.Record("brave"); err == nil || !strings.Contains(err.Error(), "free_quota_exhausted") {
		t.Fatalf("second stale instance should re-read disk and fail free quota, got err=%v", err)
	}
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	decision := reloaded.Decide("brave")
	if decision.Allowed || decision.Reason != "free_quota_exhausted" || decision.FreeRemaining != 0 {
		t.Fatalf("free quota should remain exhausted after stale-instance race, got %#v", decision)
	}
}

func TestFileQuotaLedgerPreservesKnownExhaustedFreeTierAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}
	seed := []QuotaEntry{{Provider: "brave", CostClass: CostClassFreeTierBYOK, FreeRemaining: 1}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	if err := ledger.Record("brave"); err != nil {
		t.Fatalf("record free-tier attempt: %v", err)
	}
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	decision := reloaded.Decide("brave")
	if decision.Allowed || decision.Reason != "free_quota_exhausted" || decision.FreeRemaining != 0 {
		t.Fatalf("known exhausted free quota should fail closed, got %#v", decision)
	}
}

func TestFileQuotaLedgerCorruptBackupWarningIsPathless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}
	oldBackup := backupCorruptLedger
	backupCorruptLedger = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	t.Cleanup(func() { backupCorruptLedger = oldBackup })

	ledger, err := NewFileQuotaLedgerWithPolicy(path, QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 1}, []QuotaEntry{{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1}})
	if err != nil {
		t.Fatalf("corrupt ledger with failed backup should still recover fail-closed: %v", err)
	}
	warning := ledger.BudgetStatus().LedgerWarning
	if warning == "" || !strings.Contains(warning, "backup failed") {
		t.Fatalf("expected pathless backup failure warning, got %q", warning)
	}
	if strings.Contains(warning, path) || strings.Contains(warning, filepath.Base(path)) || strings.Contains(warning, "permission") {
		t.Fatalf("warning leaked private path or raw OS error: %q", warning)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovered ledger: %v", err)
	}
	if strings.Contains(string(payload), path) || strings.Contains(string(payload), filepath.Base(path)) || strings.Contains(string(payload), "permission") {
		t.Fatalf("persisted warning leaked private path or raw OS error: %s", payload)
	}
}

func TestFileQuotaLedgerCorruptionFailsClosedAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quota-ledger.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}
	policy := QuotaPolicy{Policy: CostPolicyCostCapped, HardCapCents: 5}
	seed := []QuotaEntry{
		{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 1},
		{Provider: "ddgs", CostClass: CostClassKeylessFree, KeylessFree: true},
	}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("corrupt ledger should recover with fail-closed marker, got error: %v", err)
	}
	budget := ledger.BudgetStatus()
	if budget.LedgerState != LedgerStateRecoveredCorrupt || budget.LedgerWarning == "" {
		t.Fatalf("expected recovered corrupt ledger warning, got %#v", budget)
	}
	if decision := ledger.Decide("tavily"); decision.Allowed || decision.Reason != "ledger_corrupt_fail_closed" {
		t.Fatalf("corrupt ledger should fail closed for premium providers, got %#v", decision)
	}
	if decision := ledger.Decide("ddgs"); !decision.Allowed || decision.Reason != "keyless_free" {
		t.Fatalf("corrupt ledger should still allow keyless-free provider, got %#v", decision)
	}
	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one corrupt backup, backups=%#v err=%v", backups, err)
	}

	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload recovered ledger: %v", err)
	}
	if decision := reloaded.Decide("tavily"); decision.Allowed || decision.Reason != "ledger_corrupt_fail_closed" {
		t.Fatalf("fail-closed marker should persist across reloads, got %#v", decision)
	}
}
