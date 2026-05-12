package core

import "fmt"

type QuotaPolicy struct {
	HardCapCents int `json:"hard_cap_cents"`
}

type QuotaEntry struct {
	Provider      string `json:"provider"`
	FreeRemaining int    `json:"free_remaining"`
	KeylessFree   bool   `json:"keyless_free"`
	Unknown       bool   `json:"unknown"`
}

type QuotaLedger interface {
	Allow(provider string) bool
	Record(provider string) error
	Get(provider string) (QuotaEntry, bool)
	Entries() []QuotaEntry
}

type MemoryQuotaLedger struct {
	entries map[string]QuotaEntry
}

func NewMemoryQuotaLedger() *MemoryQuotaLedger {
	return &MemoryQuotaLedger{entries: map[string]QuotaEntry{}}
}

func (l *MemoryQuotaLedger) Set(entry QuotaEntry) {
	l.entries[entry.Provider] = entry
}

func (l *MemoryQuotaLedger) Allow(provider string) bool {
	entry, ok := l.entries[provider]
	if !ok {
		return false
	}
	if entry.KeylessFree {
		return true
	}
	if entry.FreeRemaining > 0 {
		return true
	}
	if entry.Unknown {
		return true
	}
	return false
}

func (l *MemoryQuotaLedger) Record(provider string) error {
	entry, ok := l.entries[provider]
	if !ok {
		return fmt.Errorf("quota entry for provider %q not found", provider)
	}
	if entry.KeylessFree {
		return nil
	}
	if entry.FreeRemaining <= 0 {
		return fmt.Errorf("provider %q has no free quota", provider)
	}
	entry.FreeRemaining--
	l.entries[provider] = entry
	return nil
}

func (l *MemoryQuotaLedger) Get(provider string) (QuotaEntry, bool) {
	entry, ok := l.entries[provider]
	return entry, ok
}

func (l *MemoryQuotaLedger) Entries() []QuotaEntry {
	out := make([]QuotaEntry, 0, len(l.entries))
	for _, entry := range l.entries {
		out = append(out, entry)
	}
	return out
}
