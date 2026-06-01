package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type CostPolicy string

const (
	CostPolicyFreeFirst    CostPolicy = "free-first"
	CostPolicyCostCapped   CostPolicy = "cost-capped"
	CostPolicyQualityFirst CostPolicy = "quality-first"
)

type ProviderCostClass string

const (
	CostClassKeylessFree    ProviderCostClass = "keyless-free"
	CostClassFreeTierBYOK   ProviderCostClass = "free-tier-BYOK"
	CostClassPremiumCapable ProviderCostClass = "premium-capable"
	CostClassUnknownCost    ProviderCostClass = "unknown-cost"
	CostClassDisabledNoKey  ProviderCostClass = "disabled-no-key"
)

type QuotaPolicy struct {
	Policy       CostPolicy `json:"policy"`
	HardCapCents int        `json:"hard_cap_cents"`
	// HardCapSource records how HardCapCents was determined: "explicit" (the
	// user set NOLE_HARD_CAP_CENTS) or "unset" (cost-capped policy with no cap
	// configured, so premium stays blocked). Empty for policies that need no
	// cap (free-first, quality-first). Observability only — it keeps an
	// explicit "$5 cap" distinguishable from a defaulted one and lets doctor
	// explain a silently-blocked cost-capped setup. Set by the CLI resolver.
	HardCapSource string `json:"hard_cap_source,omitempty"`
}

type RefreshWindow string

const (
	// RefreshNone means FreeRemaining is never auto-refilled. Suitable for
	// one-time trial quotas or for any entry whose refresh policy is unknown.
	RefreshNone RefreshWindow = ""
	// RefreshMonthly refills FreeRemaining to FreeQuota at the start of each
	// calendar UTC month. PeriodStart is compared lexicographically against
	// the current YYYY-MM stamp.
	RefreshMonthly RefreshWindow = "monthly"
)

type QuotaEntry struct {
	Provider           string            `json:"provider"`
	CostClass          ProviderCostClass `json:"cost_class,omitempty"`
	FreeRemaining      int               `json:"free_remaining"`
	FreeQuota          int               `json:"free_quota,omitempty"`
	RefreshWindow      RefreshWindow     `json:"refresh_window,omitempty"`
	PeriodStart        string            `json:"period_start,omitempty"`
	KeylessFree        bool              `json:"keyless_free"`
	Unknown            bool              `json:"unknown"`
	EstimatedCostCents int               `json:"estimated_cost_cents,omitempty"`
	SpentCents         int               `json:"spent_cents,omitempty"`
	// MeteringModel and EstimateOnly are build-time provider metadata (sourced
	// from byokProviders each startup, never persisted runtime state). They let
	// budget_status be honest about HOW a provider meters and that FreeRemaining
	// is Nólë's own issued-request estimate, not a live dashboard balance.
	MeteringModel string `json:"metering_model,omitempty"`
	EstimateOnly  bool   `json:"estimate_only,omitempty"`
}

type QuotaDecision struct {
	Provider           string            `json:"provider"`
	Allowed            bool              `json:"allowed"`
	Reason             string            `json:"reason"`
	Policy             CostPolicy        `json:"policy"`
	CostClass          ProviderCostClass `json:"cost_class"`
	FreeRemaining      int               `json:"free_remaining"`
	HardCapCents       int               `json:"hard_cap_cents,omitempty"`
	EstimatedCostCents int               `json:"estimated_cost_cents,omitempty"`
	SpentCents         int               `json:"spent_cents,omitempty"`
}

type QuotaLedger interface {
	Allow(provider string) bool
	Decide(provider string) QuotaDecision
	Record(provider string) error
	// RecordDrift notes that a provider rejected a call as over-quota (HTTP 429)
	// while the local counter still showed room. It NEVER debits and NEVER
	// changes routing — it only records an observability signal surfaced in
	// BudgetStatus. Best-effort: failures to persist are swallowed (the signal
	// is advisory). Implementers key by provider so it is bounded.
	RecordDrift(provider, reason string)
	Get(provider string) (QuotaEntry, bool)
	Entries() []QuotaEntry
	BudgetStatus() BudgetStatus
}

type LedgerState string

const (
	LedgerStateMemory           LedgerState = "memory"
	LedgerStateFileOK           LedgerState = "file-ok"
	LedgerStateRecoveredCorrupt LedgerState = "recovered-corrupt"
	LedgerStateUnavailable      LedgerState = "unavailable"
)

const quotaLedgerSchemaVersion = 2

// CurrentMonthISO returns the current UTC calendar month in YYYY-MM form.
// Used by quota refresh logic and free-tier seed timestamps.
var nowUTC = func() time.Time { return time.Now().UTC() }

func CurrentMonthISO() string {
	return nowUTC().Format("2006-01")
}

type quotaLedgerFile struct {
	SchemaVersion int           `json:"schema_version"`
	Policy        QuotaPolicy   `json:"policy"`
	Entries       []QuotaEntry  `json:"entries"`
	DriftSignals  []DriftSignal `json:"drift_signals,omitempty"`
	FailClosed    bool          `json:"fail_closed,omitempty"`
	State         LedgerState   `json:"state,omitempty"`
	Warning       string        `json:"warning,omitempty"`
	UpdatedAt     string        `json:"updated_at"`
}

type MemoryQuotaLedger struct {
	mu               sync.Mutex
	policy           QuotaPolicy
	entries          map[string]QuotaEntry
	driftSignals     map[string]DriftSignal
	path             string
	ledgerState      LedgerState
	ledgerWarning    string
	failClosedReason string
}

// driftSignalTTL bounds how long a drift signal is surfaced in BudgetStatus
// output. A provider that recovered should not keep showing a stale "returned
// 429" warning forever (R7). Signals are aged out of OUTPUT only — never
// deleted on a read path (BudgetStatus stays a pure read). The map is keyed by
// provider so on-disk growth is bounded regardless of TTL.
const driftSignalTTL = 24 * time.Hour

func DefaultQuotaPolicy() QuotaPolicy {
	return QuotaPolicy{Policy: CostPolicyFreeFirst}
}

func ParseCostPolicy(raw string) (CostPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(CostPolicyFreeFirst):
		return CostPolicyFreeFirst, true
	case string(CostPolicyCostCapped):
		return CostPolicyCostCapped, true
	case string(CostPolicyQualityFirst):
		return CostPolicyQualityFirst, true
	default:
		return "", false
	}
}

func NewMemoryQuotaLedger() *MemoryQuotaLedger {
	return NewMemoryQuotaLedgerWithPolicy(DefaultQuotaPolicy())
}

func NewMemoryQuotaLedgerWithPolicy(policy QuotaPolicy) *MemoryQuotaLedger {
	return &MemoryQuotaLedger{policy: normalizeQuotaPolicy(policy), entries: map[string]QuotaEntry{}, driftSignals: map[string]DriftSignal{}, ledgerState: LedgerStateMemory}
}

func (l *MemoryQuotaLedger) Set(entry QuotaEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry = normalizeQuotaEntry(entry)
	apply := func() error {
		if strings.TrimSpace(l.path) != "" {
			if err := l.reloadFromDiskLocked(); err != nil {
				return err
			}
			if existing, ok := l.entries[entry.Provider]; ok {
				entry = mergeLedgerEntries(map[string]QuotaEntry{entry.Provider: entry}, []QuotaEntry{existing})[entry.Provider]
			}
		}
		l.entries[entry.Provider] = entry
		return l.persistLocked()
	}
	var err error
	if strings.TrimSpace(l.path) != "" {
		err = l.withFileLockLocked(apply)
	} else {
		err = apply()
	}
	if err != nil {
		l.markUnavailableLocked(err)
	}
}

func (l *MemoryQuotaLedger) Allow(provider string) bool {
	return l.Decide(provider).Allowed
}

func (l *MemoryQuotaLedger) Decide(provider string) QuotaDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Refresh expired monthly quotas before any policy decision so callers that
	// only ever Decide() (e.g. the router probing every provider when all are
	// blocked) still cross month boundaries correctly. We skip refresh when
	// the ledger is in fail-closed mode since we don't trust the on-disk state
	// there. In-memory refresh isn't persisted from this path; the next
	// Record/Set/reload writes it back.
	if l.failClosedReason == "" {
		l.refreshExpiredEntriesLocked(CurrentMonthISO())
	}
	return l.decideLocked(provider)
}

func (l *MemoryQuotaLedger) decideLocked(provider string) QuotaDecision {
	if l.failClosedReason != "" {
		entry, ok := l.entries[provider]
		if ok {
			entry = normalizeQuotaEntry(entry)
			if entry.CostClass == CostClassKeylessFree {
				return QuotaDecision{
					Provider:           provider,
					Allowed:            true,
					Reason:             "keyless_free",
					Policy:             l.policy.Policy,
					CostClass:          entry.CostClass,
					FreeRemaining:      entry.FreeRemaining,
					HardCapCents:       l.policy.HardCapCents,
					EstimatedCostCents: entry.EstimatedCostCents,
					SpentCents:         entry.SpentCents,
				}
			}
			return QuotaDecision{
				Provider:           provider,
				Allowed:            false,
				Reason:             l.failClosedReason,
				Policy:             l.policy.Policy,
				CostClass:          entry.CostClass,
				FreeRemaining:      entry.FreeRemaining,
				HardCapCents:       l.policy.HardCapCents,
				EstimatedCostCents: entry.EstimatedCostCents,
				SpentCents:         entry.SpentCents,
			}
		}
		return QuotaDecision{
			Provider:     provider,
			Allowed:      false,
			Reason:       l.failClosedReason,
			Policy:       l.policy.Policy,
			CostClass:    CostClassDisabledNoKey,
			HardCapCents: l.policy.HardCapCents,
		}
	}
	entry, ok := l.entries[provider]
	if !ok {
		return QuotaDecision{
			Provider:  provider,
			Allowed:   false,
			Reason:    "disabled_no_key",
			Policy:    l.policy.Policy,
			CostClass: CostClassDisabledNoKey,
		}
	}
	entry = normalizeQuotaEntry(entry)
	decision := QuotaDecision{
		Provider:           provider,
		Policy:             l.policy.Policy,
		CostClass:          entry.CostClass,
		FreeRemaining:      entry.FreeRemaining,
		HardCapCents:       l.policy.HardCapCents,
		EstimatedCostCents: entry.EstimatedCostCents,
		SpentCents:         entry.SpentCents,
	}

	switch entry.CostClass {
	case CostClassDisabledNoKey:
		decision.Reason = "disabled_no_key"
	case CostClassKeylessFree:
		decision.Allowed = true
		decision.Reason = "keyless_free"
	case CostClassFreeTierBYOK:
		if entry.FreeRemaining > 0 {
			decision.Allowed = true
			decision.Reason = "free_tier_available"
		} else {
			decision.Reason = "free_quota_exhausted"
		}
	case CostClassUnknownCost:
		if l.policy.Policy == CostPolicyQualityFirst {
			decision.Allowed = true
			decision.Reason = "quality_first_allows_unknown_cost"
		} else {
			decision.Reason = "unknown_cost_blocked"
		}
	case CostClassPremiumCapable:
		switch l.policy.Policy {
		case CostPolicyFreeFirst:
			decision.Reason = "premium_blocked_free_first"
		case CostPolicyCostCapped:
			decision.Allowed, decision.Reason = premiumWithinCap(entry, l.policy, l.totalSpentLocked())
		case CostPolicyQualityFirst:
			decision.Allowed = true
			decision.Reason = "quality_first_allows_premium"
		default:
			decision.Reason = "unknown_cost_blocked"
		}
	default:
		decision.CostClass = CostClassUnknownCost
		if l.policy.Policy == CostPolicyQualityFirst {
			decision.Allowed = true
			decision.Reason = "quality_first_allows_unknown_cost"
		} else {
			decision.Reason = "unknown_cost_blocked"
		}
	}
	return decision
}

func (l *MemoryQuotaLedger) Record(provider string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.TrimSpace(l.path) != "" {
		return l.withFileLockLocked(func() error {
			if err := l.reloadFromDiskLocked(); err != nil {
				l.markUnavailableLocked(err)
				return fmt.Errorf("refresh quota ledger: unavailable")
			}
			return l.recordLocked(provider)
		})
	}
	return l.recordLocked(provider)
}

func (l *MemoryQuotaLedger) recordLocked(provider string) error {
	refreshed := l.refreshExpiredEntriesLocked(CurrentMonthISO())
	decision := l.decideLocked(provider)
	if !decision.Allowed {
		if refreshed {
			if err := l.persistLocked(); err != nil {
				l.markUnavailableLocked(err)
			}
		}
		return fmt.Errorf("provider %q is blocked by %s policy: %s", provider, decision.Policy, decision.Reason)
	}
	entry, ok := l.entries[provider]
	if !ok {
		return fmt.Errorf("quota entry for provider %q not found", provider)
	}
	entry = normalizeQuotaEntry(entry)
	oldEntry := entry
	changed := refreshed
	switch entry.CostClass {
	case CostClassKeylessFree:
		return nil
	case CostClassFreeTierBYOK:
		if entry.FreeRemaining <= 0 {
			return fmt.Errorf("provider %q is blocked by %s policy: free_quota_exhausted", provider, l.policy.Policy)
		}
		entry.FreeRemaining--
		changed = true
	case CostClassPremiumCapable:
		if entry.EstimatedCostCents > 0 {
			entry.SpentCents += entry.EstimatedCostCents
			changed = true
		}
	}
	l.entries[provider] = entry
	if changed {
		if err := l.persistLocked(); err != nil {
			l.entries[provider] = oldEntry
			l.markUnavailableLocked(err)
			return fmt.Errorf("persist quota ledger: unavailable")
		}
	}
	return nil
}

// RecordDrift notes that `provider` returned an over-quota/rate-limit rejection
// while the local counter still showed room. It does NOT debit and does NOT
// touch FreeRemaining — it only upserts a per-provider observability signal.
// When the ledger is file-backed it reload-merges first so a peer process's
// signal for another provider is not clobbered, then persists. Persistence is
// best-effort: on failure the in-memory signal is still kept (the signal is
// advisory, and a search must never fail because a drift note could not be
// written).
func (l *MemoryQuotaLedger) RecordDrift(provider, reason string) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sig := DriftSignal{Provider: provider, Reason: reason, ObservedAt: nowUTC().Format(time.RFC3339)}
	apply := func() error {
		if strings.TrimSpace(l.path) != "" {
			if err := l.reloadFromDiskLocked(); err != nil {
				return err
			}
		}
		l.setDriftSignalLocked(sig)
		return l.persistLocked()
	}
	var err error
	if strings.TrimSpace(l.path) != "" {
		err = l.withFileLockLocked(apply)
	} else {
		err = apply()
	}
	if err != nil {
		// Persistence failed; keep the signal in memory so this process still
		// surfaces it. Do not mark the ledger unavailable — drift is advisory.
		l.setDriftSignalLocked(sig)
	}
}

func (l *MemoryQuotaLedger) setDriftSignalLocked(sig DriftSignal) {
	if l.driftSignals == nil {
		l.driftSignals = map[string]DriftSignal{}
	}
	l.driftSignals[sig.Provider] = sig
}

// driftSignalsLocked returns every stored signal sorted by provider, for
// persistence. The map is keyed by provider so this is bounded by provider
// count regardless of how often drift fires.
func (l *MemoryQuotaLedger) driftSignalsLocked() []DriftSignal {
	if len(l.driftSignals) == 0 {
		return nil
	}
	out := make([]DriftSignal, 0, len(l.driftSignals))
	for _, sig := range l.driftSignals {
		out = append(out, sig)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// recentDriftSignalsLocked returns signals younger than driftSignalTTL, sorted
// by provider. Aging is applied to OUTPUT only — stale signals are not deleted
// here (BudgetStatus must stay a pure read). A signal with an unparseable
// ObservedAt is dropped defensively.
func (l *MemoryQuotaLedger) recentDriftSignalsLocked() []DriftSignal {
	all := l.driftSignalsLocked()
	if len(all) == 0 {
		return nil
	}
	now := nowUTC()
	out := make([]DriftSignal, 0, len(all))
	for _, sig := range all {
		ts, err := time.Parse(time.RFC3339, sig.ObservedAt)
		if err != nil {
			continue
		}
		if now.Sub(ts.UTC()) <= driftSignalTTL {
			out = append(out, sig)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeDriftSignals unions on-disk and in-memory signals, keeping the most
// recent ObservedAt per provider so two processes recording drift for
// different providers never clobber each other (R6). Unparseable timestamps
// lose to parseable ones; if both are unparseable the loaded one wins (it is
// at least as fresh as what we are about to write back).
func mergeDriftSignals(current map[string]DriftSignal, loaded []DriftSignal) map[string]DriftSignal {
	merged := map[string]DriftSignal{}
	for provider, sig := range current {
		merged[provider] = sig
	}
	for _, sig := range loaded {
		if strings.TrimSpace(sig.Provider) == "" {
			continue
		}
		existing, ok := merged[sig.Provider]
		if !ok || driftMoreRecent(sig, existing) {
			merged[sig.Provider] = sig
		}
	}
	return merged
}

func driftMoreRecent(candidate, existing DriftSignal) bool {
	ct, cerr := time.Parse(time.RFC3339, candidate.ObservedAt)
	et, eerr := time.Parse(time.RFC3339, existing.ObservedAt)
	if cerr != nil {
		return false
	}
	if eerr != nil {
		return true
	}
	return ct.After(et)
}

func (l *MemoryQuotaLedger) Get(provider string) (QuotaEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[provider]
	if !ok {
		return QuotaEntry{}, false
	}
	return normalizeQuotaEntry(entry), true
}

func (l *MemoryQuotaLedger) Entries() []QuotaEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.entriesLocked()
}

func (l *MemoryQuotaLedger) entriesLocked() []QuotaEntry {
	out := make([]QuotaEntry, 0, len(l.entries))
	for _, entry := range l.entries {
		out = append(out, normalizeQuotaEntry(entry))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// budgetEstimateNote is the universal honesty line surfaced in budget_status
// whenever at least one BYOK provider is estimate-only. It makes explicit that
// FreeRemaining is Nólë's own issued-request count, not a live dashboard read.
const budgetEstimateNote = "FreeRemaining is Nólë's own count of issued requests this period, not a live provider-dashboard balance; providers meter differently (see metering_model and any drift_signals) — verify your dashboard for exact figures."

func (l *MemoryQuotaLedger) BudgetStatus() BudgetStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.entriesLocked()
	spent := 0
	estimateOnly := false
	for _, entry := range entries {
		spent += entry.SpentCents
		if entry.EstimateOnly {
			estimateOnly = true
		}
	}
	estimateNote := ""
	if estimateOnly {
		estimateNote = budgetEstimateNote
	}
	signals := l.recentDriftSignalsLocked()
	return BudgetStatus{
		Policy:            l.policy.Policy,
		HardCapCents:      l.policy.HardCapCents,
		HardCapSource:     l.policy.HardCapSource,
		SpentCents:        spent,
		NoHiddenPaidSpend: l.policy.Policy != CostPolicyQualityFirst,
		LedgerState:       l.ledgerState,
		LedgerWarning:     l.ledgerWarning,
		EstimateNote:      estimateNote,
		HasDrift:          len(signals) > 0,
		DriftSignals:      signals,
		Entries:           entries,
	}
}

func (l *MemoryQuotaLedger) totalSpentLocked() int {
	spent := 0
	for _, entry := range l.entries {
		spent += normalizeQuotaEntry(entry).SpentCents
	}
	return spent
}

func NewFileQuotaLedgerWithPolicy(path string, policy QuotaPolicy, seeds []QuotaEntry) (*MemoryQuotaLedger, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		ledger := NewMemoryQuotaLedgerWithPolicy(policy)
		for _, entry := range seeds {
			ledger.Set(entry)
		}
		return ledger, nil
	}

	ledger := &MemoryQuotaLedger{
		policy:       normalizeQuotaPolicy(policy),
		entries:      seedEntryMap(seeds),
		driftSignals: map[string]DriftSignal{},
		path:         path,
		ledgerState:  LedgerStateFileOK,
	}

	if err := ledger.withFileLockLocked(func() error { return ledger.reloadFromDiskLocked() }); err != nil {
		ledger.markUnavailableLocked(err)
		return ledger, err
	}
	return ledger, nil
}

func (l *MemoryQuotaLedger) reloadFromDiskLocked() error {
	if strings.TrimSpace(l.path) == "" {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			l.ledgerState = LedgerStateFileOK
			l.ledgerWarning = ""
			l.failClosedReason = ""
			return l.persistLocked()
		}
		return err
	}

	var disk quotaLedgerFile
	if err := json.Unmarshal(data, &disk); err != nil || disk.SchemaVersion < 1 || disk.SchemaVersion > quotaLedgerSchemaVersion {
		return l.recoverCorruptLedgerLocked(err)
	}

	l.entries = mergeLedgerEntries(l.entries, disk.Entries)
	l.driftSignals = mergeDriftSignals(l.driftSignals, disk.DriftSignals)
	refreshed := l.refreshExpiredEntriesLocked(CurrentMonthISO())
	migrated := disk.SchemaVersion < quotaLedgerSchemaVersion
	if disk.FailClosed {
		l.failClosedReason = "ledger_corrupt_fail_closed"
	} else if l.failClosedReason == "ledger_corrupt_fail_closed" || l.failClosedReason == "ledger_unavailable_fail_closed" {
		l.failClosedReason = ""
	}
	if disk.State != "" {
		l.ledgerState = disk.State
	} else {
		l.ledgerState = LedgerStateFileOK
	}
	l.ledgerWarning = disk.Warning
	if refreshed || migrated {
		return l.persistLocked()
	}
	return nil
}

func (l *MemoryQuotaLedger) recoverCorruptLedgerLocked(parseErr error) error {
	backup, backupErr := backupCorruptLedger(l.path)
	l.ledgerState = LedgerStateRecoveredCorrupt
	l.failClosedReason = "ledger_corrupt_fail_closed"
	if parseErr != nil {
		l.ledgerWarning = "quota ledger was corrupt; recovered in fail-closed mode"
	} else {
		l.ledgerWarning = "quota ledger schema was invalid; recovered in fail-closed mode"
	}
	if backupErr != nil {
		l.ledgerWarning += "; backup failed"
	} else if backup != "" {
		l.ledgerWarning += "; backup created"
	}
	return l.persistLocked()
}

func (l *MemoryQuotaLedger) withFileLockLocked(fn func() error) error {
	if strings.TrimSpace(l.path) == "" {
		return fn()
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(l.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockLedgerFile(lock); err != nil {
		return err
	}
	defer func() { _ = unlockLedgerFile(lock) }()
	return fn()
}

func seedEntryMap(seeds []QuotaEntry) map[string]QuotaEntry {
	entries := map[string]QuotaEntry{}
	for _, entry := range seeds {
		entry = normalizeQuotaEntry(entry)
		if entry.Provider != "" {
			entries[entry.Provider] = entry
		}
	}
	return entries
}

func mergeLedgerEntries(seeds map[string]QuotaEntry, loaded []QuotaEntry) map[string]QuotaEntry {
	entries := map[string]QuotaEntry{}
	for provider, seed := range seeds {
		entries[provider] = normalizeQuotaEntry(seed)
	}
	hasSeeds := len(seeds) > 0
	for _, loadedEntry := range loaded {
		loadedEntry = normalizeQuotaEntry(loadedEntry)
		if loadedEntry.Provider == "" {
			continue
		}
		seed, ok := entries[loadedEntry.Provider]
		if !ok {
			// Orphan: a provider on disk that the current build no longer
			// seeds (e.g. jina after removal). Drop it when we have seeds,
			// since keeping it would surface a no-longer-routable provider
			// in budget output. Without seeds we preserve the entry — this
			// path is exercised by direct in-memory Set() calls.
			if hasSeeds {
				continue
			}
			entries[loadedEntry.Provider] = loadedEntry
			continue
		}
		merged := seed
		// Free-tier accounting (FreeRemaining + FreeQuota + RefreshWindow +
		// PeriodStart) is provider-level state, not cost-class-level. Carry
		// it across cost-class transitions whenever the loaded entry already
		// had a free-tier counter (loaded.FreeQuota > 0). Without this, a
		// user could oscillate NOLE_<PROVIDER>_PAID on/off to reset their
		// monthly free quota and bypass the guard.
		//
		// Two cases that DO take the seed instead:
		//   1. Same cost class: existing behavior. Inherit FreeRemaining and
		//      PeriodStart from disk; let the seed contribute the rest
		//      (which is identical to disk in normal operation).
		//   2. v1 migration where the disk entry has FreeQuota=0 (the field
		//      didn't exist in v1, so we treat it as "no prior free-tier
		//      counter"). Use the seed's fresh FreeRemaining.
		if loadedEntry.CostClass == seed.CostClass {
			merged.FreeRemaining = loadedEntry.FreeRemaining
			if strings.TrimSpace(loadedEntry.PeriodStart) != "" {
				merged.PeriodStart = loadedEntry.PeriodStart
			}
			// If the seeded floor dropped below what the on-disk entry was sized
			// for (e.g. the v0.7.1 tavily/firecrawl 1000->500 credit-vs-call
			// correction), the inherited FreeRemaining can exceed the new ceiling
			// and keep over-reading until the next monthly rollover. Re-base it on
			// calls already consumed this period (loaded.FreeQuota - loaded
			// .FreeRemaining) against the NEW floor so the correction lands on the
			// first load instead of next month. Undercounting is the safe
			// direction; the guard is idempotent once disk carries the new floor.
			if seed.FreeQuota > 0 && loadedEntry.FreeQuota > seed.FreeQuota {
				consumed := loadedEntry.FreeQuota - loadedEntry.FreeRemaining
				if consumed < 0 {
					consumed = 0
				}
				rebased := seed.FreeQuota - consumed
				if rebased < 0 {
					rebased = 0
				}
				merged.FreeRemaining = rebased
			}
		} else if loadedEntry.FreeQuota > 0 {
			merged.FreeRemaining = loadedEntry.FreeRemaining
			merged.FreeQuota = loadedEntry.FreeQuota
			if loadedEntry.RefreshWindow != "" {
				merged.RefreshWindow = loadedEntry.RefreshWindow
			}
			if strings.TrimSpace(loadedEntry.PeriodStart) != "" {
				merged.PeriodStart = loadedEntry.PeriodStart
			}
		}
		if loadedEntry.SpentCents > merged.SpentCents {
			merged.SpentCents = loadedEntry.SpentCents
		}
		entries[loadedEntry.Provider] = normalizeQuotaEntry(merged)
	}
	return entries
}

// refreshExpiredEntriesLocked refills FreeRemaining for any entry whose
// RefreshWindow has elapsed since PeriodStart. Returns true when any entry was
// refreshed (so callers can decide whether to persist).
//
// The function assumes the mutex is held by the caller. It does NOT itself
// take the file lock or persist; the caller chooses whether to persist based
// on its execution path (reloadFromDiskLocked persists, recordLocked persists
// via its existing changed flag).
func (l *MemoryQuotaLedger) refreshExpiredEntriesLocked(now string) bool {
	changed := false
	for provider, entry := range l.entries {
		if entry.RefreshWindow != RefreshMonthly {
			continue
		}
		if entry.FreeQuota <= 0 {
			continue
		}
		// Skip only when there is no period or it is exactly the current
		// period. A PeriodStart that differs from now is refreshed — this
		// covers the normal past→current monthly rollover AND a future-dated
		// PeriodStart (clock skew or a ledger copied from a host whose clock
		// was ahead), which the previous `>= now` guard left stranded as
		// permanently exhausted. Resetting to the current period self-heals it.
		if entry.PeriodStart == "" || entry.PeriodStart == now {
			continue
		}
		entry.FreeRemaining = entry.FreeQuota
		entry.PeriodStart = now
		l.entries[provider] = entry
		changed = true
	}
	return changed
}

func (l *MemoryQuotaLedger) persistLocked() error {
	if strings.TrimSpace(l.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	state := l.ledgerState
	if state == "" {
		state = LedgerStateFileOK
	}
	disk := quotaLedgerFile{
		SchemaVersion: quotaLedgerSchemaVersion,
		Policy:        l.policy,
		Entries:       l.entriesLocked(),
		DriftSignals:  l.driftSignalsLocked(),
		FailClosed:    l.failClosedReason != "",
		State:         state,
		Warning:       l.ledgerWarning,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return atomicWriteFile(l.path, payload)
}

// atomicWriteFile writes payload durably to path via a temp file + rename so a
// crash mid-write never leaves a partially-written ledger. It is a package var
// so tests can inject a persist failure to exercise recordLocked's rollback
// (mirrors the backupCorruptLedger test-seam convention below).
var atomicWriteFile = defaultAtomicWriteFile

func defaultAtomicWriteFile(path string, payload []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func (l *MemoryQuotaLedger) markUnavailableLocked(err error) {
	l.ledgerState = LedgerStateUnavailable
	l.ledgerWarning = "quota ledger unavailable; failing closed for paid/quota-tracked providers"
	l.failClosedReason = "ledger_unavailable_fail_closed"
}

var backupCorruptLedger = defaultBackupCorruptLedger

func defaultBackupCorruptLedger(path string) (string, error) {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UTC().UnixNano())
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

func normalizeQuotaPolicy(policy QuotaPolicy) QuotaPolicy {
	if parsed, ok := ParseCostPolicy(string(policy.Policy)); ok {
		policy.Policy = parsed
	} else {
		policy.Policy = CostPolicyFreeFirst
	}
	if policy.HardCapCents < 0 {
		policy.HardCapCents = 0
	}
	return policy
}

func normalizeQuotaEntry(entry QuotaEntry) QuotaEntry {
	if entry.CostClass == "" {
		switch {
		case entry.KeylessFree:
			entry.CostClass = CostClassKeylessFree
		case entry.Unknown:
			entry.CostClass = CostClassUnknownCost
		default:
			entry.CostClass = CostClassFreeTierBYOK
		}
	}
	switch entry.CostClass {
	case CostClassKeylessFree:
		entry.KeylessFree = true
	case CostClassUnknownCost:
		entry.Unknown = true
	}
	if entry.FreeRemaining < 0 {
		entry.FreeRemaining = 0
	}
	if entry.EstimatedCostCents < 0 {
		entry.EstimatedCostCents = 0
	}
	if entry.SpentCents < 0 {
		entry.SpentCents = 0
	}
	return entry
}

func premiumWithinCap(entry QuotaEntry, policy QuotaPolicy, totalSpentCents int) (bool, string) {
	if policy.HardCapCents <= 0 {
		return false, "cost_cap_not_configured"
	}
	if entry.EstimatedCostCents <= 0 {
		return false, "unknown_cost_blocked"
	}
	if totalSpentCents+entry.EstimatedCostCents > policy.HardCapCents {
		return false, "cost_cap_exceeded"
	}
	return true, "within_cost_cap"
}
