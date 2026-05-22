package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestFileQuotaLedgerMonthlyRefreshRefillsFreeRemaining(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}

	// Seed with a stale PeriodStart so the next reload triggers a refresh.
	stalePeriod := "2026-01"
	seed := []QuotaEntry{{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 0,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   stalePeriod,
	}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	// Drain to zero remaining via direct Set, simulating a prior month's full
	// consumption that already persisted to disk.
	ledger.Set(QuotaEntry{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 0,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   stalePeriod,
	})

	// Pin a known "current" month to force the refresh predicate.
	prevNow := nowUTC
	defer func() { nowUTC = prevNow }()
	current := "2026-05"
	nowUTC = func() time.Time {
		t, _ := time.Parse("2006-01", current)
		return t
	}

	reloaded, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	got, ok := reloaded.Get("tavily")
	if !ok {
		t.Fatalf("tavily entry missing after reload")
	}
	if got.FreeRemaining != 1000 || got.PeriodStart != current {
		t.Fatalf("refresh should refill FreeRemaining + bump PeriodStart, got %#v", got)
	}
	if decision := reloaded.Decide("tavily"); !decision.Allowed || decision.Reason != "free_tier_available" {
		t.Fatalf("refreshed tavily should be allowed under free-first, got %#v", decision)
	}
}

func TestFileQuotaLedgerSchemaV1MigratesToV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}

	// Hand-craft a v1 ledger on disk: schema_version=1, no refresh_window,
	// no free_quota, no period_start, but key fields like free_remaining set.
	v1Payload := `{
  "schema_version": 1,
  "policy": {"policy": "free-first", "hard_cap_cents": 0},
  "entries": [
    {"provider": "tavily", "cost_class": "free-tier-BYOK", "free_remaining": 500, "keyless_free": false, "unknown": false}
  ],
  "updated_at": "2026-04-01T00:00:00Z"
}`
	if err := os.WriteFile(path, []byte(v1Payload), 0o600); err != nil {
		t.Fatalf("write v1 ledger: %v", err)
	}

	// Pin current month so the migration doesn't trigger a refresh against a
	// natural "now".
	prevNow := nowUTC
	defer func() { nowUTC = prevNow }()
	current := "2026-05"
	nowUTC = func() time.Time {
		t, _ := time.Parse("2006-01", current)
		return t
	}

	// Seed carries v2 fields so the merge produces a v2 entry.
	seed := []QuotaEntry{{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 1000,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   current,
	}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload v1 ledger: %v", err)
	}

	// On-disk FreeRemaining (500) wins over seed (1000) — that's the v1 user's
	// existing usage being preserved.
	got, ok := ledger.Get("tavily")
	if !ok {
		t.Fatalf("tavily entry missing after migration")
	}
	if got.FreeRemaining != 500 {
		t.Fatalf("v1 FreeRemaining should be preserved across migration, got %d", got.FreeRemaining)
	}
	if got.FreeQuota != 1000 || got.RefreshWindow != RefreshMonthly {
		t.Fatalf("v2 fields should be inferred from seed, got %#v", got)
	}

	// Verify the file was rewritten as v2.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated ledger: %v", err)
	}
	if !strings.Contains(string(raw), `"schema_version": 2`) {
		t.Fatalf("migrated ledger should be v2 on disk, got: %s", raw)
	}
	if !strings.Contains(string(raw), `"refresh_window": "monthly"`) {
		t.Fatalf("migrated ledger should carry refresh_window, got: %s", raw)
	}
}

func TestFileQuotaLedgerCostClassTransitionResetsFreeRemaining(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}

	// Pre-existing v1 ledger where tavily was classified premium-capable
	// (the pre-Phase-B default for BYOK keys). FreeRemaining was always 0
	// for that class — it's meaningless data.
	v1Payload := `{
  "schema_version": 1,
  "policy": {"policy": "free-first", "hard_cap_cents": 0},
  "entries": [
    {"provider": "tavily", "cost_class": "premium-capable", "free_remaining": 0, "estimated_cost_cents": 0, "spent_cents": 0},
    {"provider": "jina", "cost_class": "premium-capable", "free_remaining": 0}
  ],
  "updated_at": "2026-04-01T00:00:00Z"
}`
	if err := os.WriteFile(path, []byte(v1Payload), 0o600); err != nil {
		t.Fatalf("write v1 ledger: %v", err)
	}

	prevNow := nowUTC
	defer func() { nowUTC = prevNow }()
	nowUTC = func() time.Time {
		t, _ := time.Parse("2006-01", "2026-05")
		return t
	}

	// Seed: the new free-tier-BYOK class with FreeQuota=1000.
	seed := []QuotaEntry{{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 1000,
		FreeQuota:     1000,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   "2026-05",
	}}

	ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if err != nil {
		t.Fatalf("reload v1 ledger: %v", err)
	}
	got, ok := ledger.Get("tavily")
	if !ok {
		t.Fatalf("tavily entry missing")
	}
	// Class transition: seed wins, FreeRemaining starts fresh at 1000.
	if got.FreeRemaining != 1000 {
		t.Fatalf("class transition should reset FreeRemaining to seed quota, got %d", got.FreeRemaining)
	}
	if got.CostClass != CostClassFreeTierBYOK {
		t.Fatalf("class should be free-tier-BYOK after migration, got %s", got.CostClass)
	}
	// Orphan jina should be dropped now that it's not in the seed list.
	if _, ok := ledger.Get("jina"); ok {
		t.Fatalf("orphan jina entry should be dropped after migration")
	}
}

func TestFileQuotaLedgerRejectsFutureSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}

	// Pretend a future v999 ledger landed on disk (newer than this build
	// understands). Must be rejected as corrupt + fail-closed.
	futurePayload := `{
  "schema_version": 999,
  "policy": {"policy": "free-first", "hard_cap_cents": 0},
  "entries": [{"provider": "tavily", "cost_class": "premium-capable"}],
  "updated_at": "2099-01-01T00:00:00Z"
}`
	if err := os.WriteFile(path, []byte(futurePayload), 0o600); err != nil {
		t.Fatalf("write future ledger: %v", err)
	}

	seed := []QuotaEntry{{Provider: "tavily", CostClass: CostClassPremiumCapable, EstimatedCostCents: 2}}
	ledger, _ := NewFileQuotaLedgerWithPolicy(path, policy, seed)
	if ledger == nil {
		t.Fatal("expected ledger instance even when fail-closed")
	}
	if decision := ledger.Decide("tavily"); decision.Allowed || decision.Reason != "ledger_corrupt_fail_closed" {
		t.Fatalf("future schema version should fail closed, got %#v", decision)
	}
}
