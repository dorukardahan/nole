package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
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
}

type QuotaEntry struct {
	Provider           string            `json:"provider"`
	CostClass          ProviderCostClass `json:"cost_class,omitempty"`
	FreeRemaining      int               `json:"free_remaining"`
	KeylessFree        bool              `json:"keyless_free"`
	Unknown            bool              `json:"unknown"`
	EstimatedCostCents int               `json:"estimated_cost_cents,omitempty"`
	SpentCents         int               `json:"spent_cents,omitempty"`
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
	Get(provider string) (QuotaEntry, bool)
	Entries() []QuotaEntry
	BudgetStatus() BudgetStatus
}

type MemoryQuotaLedger struct {
	mu      sync.Mutex
	policy  QuotaPolicy
	entries map[string]QuotaEntry
}

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
	return &MemoryQuotaLedger{policy: normalizeQuotaPolicy(policy), entries: map[string]QuotaEntry{}}
}

func (l *MemoryQuotaLedger) Set(entry QuotaEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry = normalizeQuotaEntry(entry)
	l.entries[entry.Provider] = entry
}

func (l *MemoryQuotaLedger) Allow(provider string) bool {
	return l.Decide(provider).Allowed
}

func (l *MemoryQuotaLedger) Decide(provider string) QuotaDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.decideLocked(provider)
}

func (l *MemoryQuotaLedger) decideLocked(provider string) QuotaDecision {
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
	decision := l.decideLocked(provider)
	if !decision.Allowed {
		return fmt.Errorf("provider %q is blocked by %s policy: %s", provider, decision.Policy, decision.Reason)
	}
	entry, ok := l.entries[provider]
	if !ok {
		return fmt.Errorf("quota entry for provider %q not found", provider)
	}
	entry = normalizeQuotaEntry(entry)
	switch entry.CostClass {
	case CostClassKeylessFree:
		return nil
	case CostClassFreeTierBYOK:
		if entry.FreeRemaining <= 0 {
			return fmt.Errorf("provider %q has no free quota", provider)
		}
		entry.FreeRemaining--
	case CostClassPremiumCapable:
		if entry.EstimatedCostCents > 0 {
			entry.SpentCents += entry.EstimatedCostCents
		}
	}
	l.entries[provider] = entry
	return nil
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

func (l *MemoryQuotaLedger) BudgetStatus() BudgetStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.entriesLocked()
	spent := 0
	for _, entry := range entries {
		spent += entry.SpentCents
	}
	return BudgetStatus{
		Policy:            l.policy.Policy,
		HardCapCents:      l.policy.HardCapCents,
		SpentCents:        spent,
		NoHiddenPaidSpend: l.policy.Policy != CostPolicyQualityFirst,
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
