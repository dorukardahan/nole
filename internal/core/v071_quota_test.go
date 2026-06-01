package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v0.7.1 lowered the tavily/firecrawl free floor 1000 -> 500 (credit-vs-call
// correction). An EXISTING user already has a persisted current-month ledger
// entry sized for the old 1000 floor. The seed alone does not fix the live
// counter: mergeLedgerEntries inherits the loaded FreeRemaining for a
// same-cost-class entry, so a mid-month ledger would keep reporting up to ~1000
// remaining until the next rollover — exactly the over-read this release exists
// to eliminate. The merge must re-base the loaded counter on calls already
// consumed against the NEW floor so the correction takes effect on first load.
func TestFileQuotaLedgerClampsLoadedRemainingToLoweredFloor(t *testing.T) {
	prevNow := nowUTC
	defer func() { nowUTC = prevNow }()
	current := "2026-06"
	nowUTC = func() time.Time {
		ts, _ := time.Parse("2006-01", current)
		return ts
	}

	writeLedger := func(t *testing.T, freeRemaining, freeQuota int) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "quota-ledger.json")
		payload := `{
  "schema_version": 2,
  "policy": {"policy": "free-first", "hard_cap_cents": 0},
  "entries": [
    {"provider": "tavily", "cost_class": "free-tier-BYOK", "free_remaining": ` +
			itoa(freeRemaining) + `, "free_quota": ` + itoa(freeQuota) +
			`, "refresh_window": "monthly", "period_start": "2026-06", "keyless_free": false, "unknown": false}
  ],
  "updated_at": "2026-06-01T00:00:00Z"
}`
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatalf("write ledger: %v", err)
		}
		return path
	}

	// The v0.7.1 seed: tavily floor is now 500.
	seed := []QuotaEntry{{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 500,
		FreeQuota:     500,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   current,
	}}
	policy := QuotaPolicy{Policy: CostPolicyFreeFirst}

	cases := []struct {
		name              string
		loadedRemaining   int
		loadedQuota       int
		wantFreeRemaining int
		wantFreeQuota     int
	}{
		// Consumed 150 calls under the old 1000 floor -> 500-150 = 350 under new floor.
		{"midmonth_partial_use", 850, 1000, 350, 500},
		// Consumed 900 -> 500-900 clamps to 0 (never negative).
		{"heavy_use_clamps_to_zero", 100, 1000, 0, 500},
		// Untouched this month (consumed 0) -> 500-0 = 500 (full new floor, not 1000).
		{"no_use_drops_to_new_floor", 1000, 1000, 500, 500},
		// Already on the v0.7.1 floor on disk -> idempotent, loaded value preserved.
		{"idempotent_when_already_migrated", 300, 500, 300, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLedger(t, tc.loadedRemaining, tc.loadedQuota)
			ledger, err := NewFileQuotaLedgerWithPolicy(path, policy, seed)
			if err != nil {
				t.Fatalf("construct ledger: %v", err)
			}
			got, ok := ledger.Get("tavily")
			if !ok {
				t.Fatalf("tavily entry missing")
			}
			if got.FreeQuota != tc.wantFreeQuota {
				t.Errorf("FreeQuota = %d, want %d (the lowered floor)", got.FreeQuota, tc.wantFreeQuota)
			}
			if got.FreeRemaining != tc.wantFreeRemaining {
				t.Errorf("FreeRemaining = %d, want %d (never over-read the new floor)", got.FreeRemaining, tc.wantFreeRemaining)
			}
			if got.FreeRemaining > got.FreeQuota {
				t.Errorf("invariant violated: FreeRemaining %d > FreeQuota %d", got.FreeRemaining, got.FreeQuota)
			}
			// When the floor was lowered, the correction must be PERSISTED to disk
			// on load (self-heal), not just held in memory — otherwise a reader of
			// the ledger file (or a tool that inspects it) would still see the old
			// over-stated counter.
			if tc.loadedQuota > tc.wantFreeQuota {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read ledger back: %v", err)
				}
				s := string(raw)
				if !strings.Contains(s, `"free_quota": `+itoa(tc.wantFreeQuota)) {
					t.Errorf("disk not self-healed: want free_quota %d persisted\n%s", tc.wantFreeQuota, s)
				}
				if !strings.Contains(s, `"free_remaining": `+itoa(tc.wantFreeRemaining)) {
					t.Errorf("disk not self-healed: want free_remaining %d persisted\n%s", tc.wantFreeRemaining, s)
				}
			}
		})
	}
}

// Codex P2: the rebase must also fire across a cost-class transition. A v0.7.0
// user who toggled NOLE_TAVILY_PAID=1 ends up with a premium-capable entry that
// CARRIED the old free-tier accounting (FreeQuota=1000). When they later disable
// paid mode, the free-tier seed (FreeQuota=500) must enforce the lowered floor on
// that carried counter instead of inheriting the stale 1000-call headroom.
func TestFileQuotaLedgerClampsLoweredFloorAcrossPaidToggle(t *testing.T) {
	prevNow := nowUTC
	defer func() { nowUTC = prevNow }()
	nowUTC = func() time.Time {
		ts, _ := time.Parse("2006-01", "2026-06")
		return ts
	}

	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	// Disk state after a paid run that carried the old free-tier accounting:
	// premium-capable, but FreeQuota=1000 / FreeRemaining=850 preserved.
	payload := `{
  "schema_version": 2,
  "policy": {"policy": "free-first", "hard_cap_cents": 0},
  "entries": [
    {"provider": "tavily", "cost_class": "premium-capable", "free_remaining": 850, "free_quota": 1000, "refresh_window": "monthly", "period_start": "2026-06", "keyless_free": false, "unknown": false}
  ],
  "updated_at": "2026-06-01T00:00:00Z"
}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	// Seed: paid mode now OFF -> free-tier-BYOK at the v0.7.1 floor of 500.
	seed := []QuotaEntry{{
		Provider:      "tavily",
		CostClass:     CostClassFreeTierBYOK,
		FreeRemaining: 500,
		FreeQuota:     500,
		RefreshWindow: RefreshMonthly,
		PeriodStart:   "2026-06",
	}}
	ledger, err := NewFileQuotaLedgerWithPolicy(path, QuotaPolicy{Policy: CostPolicyFreeFirst}, seed)
	if err != nil {
		t.Fatalf("construct ledger: %v", err)
	}
	got, ok := ledger.Get("tavily")
	if !ok {
		t.Fatalf("tavily entry missing")
	}
	if got.CostClass != CostClassFreeTierBYOK {
		t.Errorf("class = %s, want free-tier-BYOK after paid toggle off", got.CostClass)
	}
	if got.FreeQuota != 500 {
		t.Errorf("FreeQuota = %d, want 500 (lowered floor enforced across transition)", got.FreeQuota)
	}
	// consumed 150 (1000-850) carried over -> 500-150 = 350, NOT the stale 850.
	if got.FreeRemaining != 350 {
		t.Errorf("FreeRemaining = %d, want 350 (consumed preserved, floor enforced; never 850)", got.FreeRemaining)
	}
	if got.FreeRemaining > got.FreeQuota {
		t.Errorf("invariant violated: FreeRemaining %d > FreeQuota %d", got.FreeRemaining, got.FreeQuota)
	}
}

// itoa avoids importing strconv into the test for a single helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
