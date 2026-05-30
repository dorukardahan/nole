package core

import (
	"fmt"
	"testing"
	"time"
)

// A long-lived MCP server can issue many distinct queries; with only lazy TTL
// expiry the cache maps grow without bound. SetMaxEntries caps each map and
// evicts the oldest entry on overflow.
func TestMemoryResponseCacheBoundsSearchEntriesEvictingOldest(t *testing.T) {
	tick := time.Unix(0, 0)
	clock := func() time.Time { tick = tick.Add(time.Second); return tick }
	c := NewMemoryResponseCacheWithClock(time.Hour, clock)
	c.SetMaxEntries(3)

	for i := 0; i < 5; i++ {
		c.SetSearch(
			SearchRequest{Query: fmt.Sprintf("q%d", i), Task: TaskGeneral, Limit: 1},
			SearchResponse{Provider: "x", Results: []SearchResult{{Title: "t"}}},
		)
	}

	if len(c.search) != 3 {
		t.Fatalf("search cache size = %d, want bounded at 3", len(c.search))
	}
	if _, ok := c.GetSearch(SearchRequest{Query: "q0", Task: TaskGeneral, Limit: 1}); ok {
		t.Fatalf("oldest entry q0 should have been evicted")
	}
	if _, ok := c.GetSearch(SearchRequest{Query: "q1", Task: TaskGeneral, Limit: 1}); ok {
		t.Fatalf("second-oldest entry q1 should have been evicted")
	}
	if _, ok := c.GetSearch(SearchRequest{Query: "q4", Task: TaskGeneral, Limit: 1}); !ok {
		t.Fatalf("newest entry q4 should be present")
	}
}

func TestMemoryResponseCacheBoundsExtractEntries(t *testing.T) {
	tick := time.Unix(0, 0)
	clock := func() time.Time { tick = tick.Add(time.Second); return tick }
	c := NewMemoryResponseCacheWithClock(time.Hour, clock)
	c.SetMaxEntries(2)

	for i := 0; i < 5; i++ {
		c.SetExtract(
			ExtractRequest{URL: fmt.Sprintf("https://example.com/%d", i)},
			ExtractResponse{Provider: "x", Content: "body"},
		)
	}
	if len(c.extract) != 2 {
		t.Fatalf("extract cache size = %d, want bounded at 2", len(c.extract))
	}
}

func TestMemoryResponseCacheDefaultMaxEntriesPositive(t *testing.T) {
	c := NewMemoryResponseCache(time.Hour)
	if c.maxEntries <= 0 {
		t.Fatalf("default maxEntries = %d, want a positive bound", c.maxEntries)
	}
}
