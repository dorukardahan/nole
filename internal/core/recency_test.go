package core

import "testing"

func TestParsePublishedAtLayouts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"tavily RFC1123", "Tue, 19 May 2026 18:59:59 GMT", true},
		{"brave zoneless ISO", "2026-06-01T02:34:19", true},
		{"rfc3339 with zone", "2026-06-01T02:34:19Z", true},
		{"date only", "2026-06-01", true},
		{"relative (brave age) is not parseable", "6 hours ago", false},
		{"empty", "", false},
		{"junk", "not-a-date", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parsePublishedAt(tc.in); ok != tc.ok {
				t.Fatalf("parsePublishedAt(%q) ok=%v, want %v", tc.in, ok, tc.ok)
			}
		})
	}
}

func titlesOf(rs []SearchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	return out
}

func TestApplyRecencySortNewsOrdersByDateDescUndatedLast(t *testing.T) {
	results := []SearchResult{
		{Title: "u1"},
		{Title: "old", PublishedAt: "2026-05-20T10:00:00"},
		{Title: "u2"},
		{Title: "new", PublishedAt: "Sat, 30 May 2026 18:59:59 GMT"}, // RFC1123 (tavily)
	}
	applyRecencySort(TaskNews, results)
	want := []string{"new", "old", "u1", "u2"} // dated desc, then undated stable
	for i, w := range want {
		if results[i].Title != w {
			t.Fatalf("position %d = %q, want %q (full=%v)", i, results[i].Title, w, titlesOf(results))
		}
	}
}

func TestApplyRecencySortFactcheckAlsoSorts(t *testing.T) {
	results := []SearchResult{
		{Title: "garbage-date", PublishedAt: "not-a-date"},
		{Title: "dated", PublishedAt: "2026-05-31"},
		{Title: "empty-date"},
	}
	applyRecencySort(TaskFactcheck, results)
	if results[0].Title != "dated" {
		t.Fatalf("dated result should sort first, got %v", titlesOf(results))
	}
	// Both unparseable entries sink to the bottom, stable in original order.
	if results[1].Title != "garbage-date" || results[2].Title != "empty-date" {
		t.Fatalf("undated entries should keep stable order, got %v", titlesOf(results))
	}
}

func TestApplyRecencySortLeavesNonRecencyTasksUntouched(t *testing.T) {
	for _, task := range []TaskType{TaskGeneral, TaskDocs, TaskCode, TaskResearch} {
		results := []SearchResult{
			{Title: "b", PublishedAt: "2026-05-20"},
			{Title: "a", PublishedAt: "2026-05-31"},
		}
		applyRecencySort(task, results)
		if results[0].Title != "b" || results[1].Title != "a" {
			t.Fatalf("task %q must not reorder results, got %v", task, titlesOf(results))
		}
	}
}

func TestApplyRecencySortIsDeterministicAndPreservesScore(t *testing.T) {
	score := 0.42
	mk := func() []SearchResult {
		return []SearchResult{
			{Title: "x", PublishedAt: "garbage"},
			{Title: "y", PublishedAt: "2026-05-31T00:00:00", Score: &score},
			{Title: "z", PublishedAt: "2026-05-20T00:00:00"},
		}
	}
	r1 := mk()
	applyRecencySort(TaskNews, r1)
	r2 := mk()
	applyRecencySort(TaskNews, r2)
	for i := range r1 {
		if r1[i].Title != r2[i].Title {
			t.Fatalf("non-deterministic sort: %v vs %v", titlesOf(r1), titlesOf(r2))
		}
	}
	// The Score pointer must travel with its result, untouched.
	if r1[0].Title != "y" || r1[0].Score == nil || *r1[0].Score != 0.42 {
		t.Fatalf("Score must be preserved and move with its result, got %#v", r1[0])
	}
}
