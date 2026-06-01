package core

import (
	"sort"
	"time"
)

// recencyLayouts are the date/time formats Nólë's providers emit for a result's
// publication date, widest-first; the first layout that parses wins. Verified
// live 2026-06-01: Tavily returns RFC1123 ("Tue, 19 May 2026 18:59:59 GMT");
// Brave returns a zoneless ISO timestamp ("2026-06-01T02:34:19"). A missing
// layout would silently demote a real date to "undated", so keep this list in
// sync with what adapters actually pass through.
var recencyLayouts = []string{
	time.RFC1123,
	time.RFC1123Z,
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parsePublishedAt best-effort parses a provider-supplied publication date.
// Returns ok=false for empty or unrecognized strings (e.g. Brave's relative
// "6 hours ago") — those are kept verbatim for the agent but cannot be ordered.
func parsePublishedAt(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range recencyLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// applyRecencySort stably reorders recency-oriented results (news, factcheck) so
// provider-supplied publication dates surface newest-first, with undated results
// kept in their original relative order at the bottom.
//
// This is the ONLY reorder Nólë applies, and it is deliberately not a quality
// judgment: it surfaces a freshness signal the provider already returned, for
// the agent to use. It never drops, filters, fabricates, or consults Score, and
// it leaves non-recency tasks byte-for-byte untouched (the gateway adds no
// ordering opinion of its own). Results are copied by value during the sort, so
// the shared *Score pointer is moved, never dereferenced or mutated.
func applyRecencySort(task TaskType, results []SearchResult) {
	if task != TaskNews && task != TaskFactcheck {
		return
	}
	if len(results) < 2 {
		return
	}
	type item struct {
		res SearchResult
		t   time.Time
		ok  bool
	}
	items := make([]item, len(results))
	for i, r := range results {
		t, ok := parsePublishedAt(r.PublishedAt)
		items[i] = item{res: r, t: t, ok: ok}
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].ok != items[b].ok {
			return items[a].ok // parseable dates before undated
		}
		if items[a].ok && items[b].ok && !items[a].t.Equal(items[b].t) {
			return items[a].t.After(items[b].t) // newer first
		}
		return false // stable: preserve original relative order
	})
	for i := range items {
		results[i] = items[i].res
	}
}
