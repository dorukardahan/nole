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

// Under a frozen clock every entry shares the same storedAt, so eviction must
// fall back to the insertion-seq tiebreaker rather than randomized map order.
// Without the tiebreak this test is flaky; with it, the first-inserted keys are
// evicted deterministically. Run repeatedly (-count) to confirm stability.
func TestMemoryResponseCacheEvictsFirstInsertedUnderFrozenClock(t *testing.T) {
	frozen := time.Unix(0, 0)
	clock := func() time.Time { return frozen }

	t.Run("search", func(t *testing.T) {
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
		for _, gone := range []string{"q0", "q1"} {
			if _, ok := c.GetSearch(SearchRequest{Query: gone, Task: TaskGeneral, Limit: 1}); ok {
				t.Fatalf("first-inserted %q should have been evicted deterministically", gone)
			}
		}
		for _, kept := range []string{"q2", "q3", "q4"} {
			if _, ok := c.GetSearch(SearchRequest{Query: kept, Task: TaskGeneral, Limit: 1}); !ok {
				t.Fatalf("later-inserted %q should be retained", kept)
			}
		}
	})

	t.Run("extract", func(t *testing.T) {
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
		for i := 0; i < 3; i++ {
			if _, ok := c.GetExtract(ExtractRequest{URL: fmt.Sprintf("https://example.com/%d", i)}); ok {
				t.Fatalf("first-inserted extract /%d should have been evicted deterministically", i)
			}
		}
		if _, ok := c.GetExtract(ExtractRequest{URL: "https://example.com/4"}); !ok {
			t.Fatalf("newest extract entry /4 should be present")
		}
	})
}

func TestMemoryResponseCacheDefaultMaxEntriesPositive(t *testing.T) {
	c := NewMemoryResponseCache(time.Hour)
	if c.maxEntries <= 0 {
		t.Fatalf("default maxEntries = %d, want a positive bound", c.maxEntries)
	}
}
