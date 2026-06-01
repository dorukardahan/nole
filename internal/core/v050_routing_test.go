package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- planner: recency/event phrases (the validated dogfood miss) + semantic ---

func TestClassifyQueryRecencyAndSemanticPhrases(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  TaskType
	}{
		{"concerts this week (dogfood regression)", "what concerts are happening in Istanbul this week", TaskNews},
		{"upcoming events near me tonight", "upcoming events near me tonight", TaskNews},
		{"NBA schedule this week", "NBA schedule this week", TaskNews},
		{"semantic search", "semantic search for related concepts", TaskSemantic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyQuery(tc.query, PlanOptions{})
			if got.PrimaryTask != tc.want {
				t.Fatalf("ClassifyQuery(%q) primary=%q, want %q (intents=%#v)", tc.query, got.PrimaryTask, tc.want, got.Intents)
			}
		})
	}
}

// A weak event word must NOT flip a strong single-task query (collision guard).
func TestClassifyQueryStrongTaskNotFlippedByEventWord(t *testing.T) {
	got := ClassifyQuery("Cloudflare Workers pricing schedule", PlanOptions{})
	if got.PrimaryTask != TaskPricing {
		t.Fatalf("pricing query with a stray 'schedule' should stay pricing, got %q (%#v)", got.PrimaryTask, got.Intents)
	}
}

// --- service: task resolution + task_source ---

func newTaskSourceService(t *testing.T, provider string) *Service {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{name: provider}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: provider, FreeRemaining: 10})
	matrix := RouteMatrix{TaskNews: {provider}, TaskGeneral: {provider}, TaskDocs: {provider}}
	return NewService(registry, ledger, matrix)
}

func TestServiceSearchAutoClassifiesOmittedTask(t *testing.T) {
	svc := newTaskSourceService(t, "p")
	resp, err := svc.Search(context.Background(), SearchRequest{Query: "what concerts are happening this week", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.Task != TaskNews {
		t.Fatalf("omitted task should auto-classify to news, got %q", resp.Task)
	}
	if resp.TaskSource != TaskSourceDetected {
		t.Fatalf("task_source = %q, want detected", resp.TaskSource)
	}
}

func TestServiceSearchDefaultsWhenNoSignal(t *testing.T) {
	svc := newTaskSourceService(t, "p")
	resp, err := svc.Search(context.Background(), SearchRequest{Query: "jaguar", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.Task != TaskGeneral || resp.TaskSource != TaskSourceDefault {
		t.Fatalf("no-signal omitted task should be general/default, got %q/%q", resp.Task, resp.TaskSource)
	}
}

func TestServiceSearchExplicitTaskIsSupplied(t *testing.T) {
	svc := newTaskSourceService(t, "p")
	resp, err := svc.Search(context.Background(), SearchRequest{Query: "anything", Task: TaskDocs, Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.Task != TaskDocs || resp.TaskSource != TaskSourceSupplied {
		t.Fatalf("explicit task should be supplied, got %q/%q", resp.Task, resp.TaskSource)
	}
}

func TestServiceSearchLenientOnUnknownTask(t *testing.T) {
	svc := newTaskSourceService(t, "p")
	// A bogus task must NOT error: it falls through to classification.
	resp, err := svc.Search(context.Background(), SearchRequest{Query: "jaguar", Task: TaskType("totally-bogus"), Limit: 3})
	if err != nil {
		t.Fatalf("unknown task must not error, got %v", err)
	}
	if resp.Task != TaskGeneral || resp.TaskSource == TaskSourceSupplied {
		t.Fatalf("unknown task should classify (not supplied), got %q/%q", resp.Task, resp.TaskSource)
	}
}

func TestServiceSearchEmptyQueryDoesNotPanic(t *testing.T) {
	svc := newTaskSourceService(t, "p")
	resp, err := svc.Search(context.Background(), SearchRequest{Query: "", Limit: 3})
	if err != nil {
		t.Fatalf("empty query search: %v", err)
	}
	if resp.Task != TaskGeneral || resp.TaskSource != TaskSourceDefault {
		t.Fatalf("empty query should be general/default, got %q/%q", resp.Task, resp.TaskSource)
	}
}

// --- service: recency sort is task-gated (directly answers the news==general
// identical-results dogfood bug) ---

type datedProvider struct{ fakeProvider }

func (p datedProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: p.name,
		Results: []SearchResult{
			{Title: "first", URL: "https://e/1", PublishedAt: "2026-05-20T00:00:00", Provider: p.name},
			{Title: "newest", URL: "https://e/2", PublishedAt: "2026-05-31T00:00:00", Provider: p.name},
			{Title: "undated", URL: "https://e/3", Provider: p.name},
		},
	}, nil
}

func TestServiceSearchRecencySortGatedByTask(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(datedProvider{fakeProvider{name: "p"}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "p", FreeRemaining: 10})
	svc := NewService(registry, ledger, RouteMatrix{TaskNews: {"p"}, TaskGeneral: {"p"}})

	news, err := svc.Search(context.Background(), SearchRequest{Query: "x", Task: TaskNews, Limit: 5})
	if err != nil {
		t.Fatalf("news search: %v", err)
	}
	if news.Results[0].Title != "newest" {
		t.Fatalf("news results should be date-sorted newest-first, got %v", titlesOf(news.Results))
	}

	gen, err := svc.Search(context.Background(), SearchRequest{Query: "x", Task: TaskGeneral, Limit: 5})
	if err != nil {
		t.Fatalf("general search: %v", err)
	}
	if gen.Results[0].Title != "first" {
		t.Fatalf("general results must keep provider order, got %v", titlesOf(gen.Results))
	}
}

// Evergreen/code queries that merely contain a polysemous word must NOT be
// flipped to news (which would attach a month freshness filter). Guards the
// review finding that bare event/schedule/happening words over-triggered news.
func TestClassifyQueryEvergreenQueriesNotNews(t *testing.T) {
	for _, q := range []string{
		"event listener javascript",
		"how to schedule a cron job",
		"release schedule",
		"what is happening in this function",
	} {
		if got := ClassifyQuery(q, PlanOptions{}); got.PrimaryTask == TaskNews {
			t.Fatalf("ClassifyQuery(%q) flipped to news (%#v); evergreen queries must not", q, got.Intents)
		}
	}
}

// A cache hit must report THIS caller's task_source, not the cached one: an
// explicit-task and an omitted-task caller share one entry (task is in the key,
// task_source is not), so the source is overwritten per-caller.
func TestServiceSearchCacheHitOverwritesTaskSource(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(datedProvider{fakeProvider{name: "p"}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "p", FreeRemaining: 10})
	cache := NewMemoryResponseCache(time.Minute)
	svc := NewService(registry, ledger, RouteMatrix{TaskNews: {"p"}, TaskGeneral: {"p"}}, WithResponseCache(cache))

	q := "latest news this week"
	first, err := svc.Search(context.Background(), SearchRequest{Query: q, Task: TaskNews, Limit: 5})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.TaskSource != TaskSourceSupplied {
		t.Fatalf("first (explicit news) source = %q, want supplied", first.TaskSource)
	}

	// Same query, task OMITTED → classifies to news → same cache key → hit.
	second, err := svc.Search(context.Background(), SearchRequest{Query: q, Limit: 5})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Task != TaskNews {
		t.Fatalf("second task = %q, want news (same cache key)", second.Task)
	}
	if len(second.RouteTrace) == 0 || second.RouteTrace[0].CacheStatus != CacheStatusHit {
		t.Fatalf("expected a cache hit, got trace %#v", second.RouteTrace)
	}
	if second.TaskSource != TaskSourceDetected {
		t.Fatalf("cache-hit task_source = %q, want detected (overwritten per-caller)", second.TaskSource)
	}
	if len(second.Results) == 0 {
		t.Fatalf("cache hit should still return results")
	}
}

// The routing insight appends a task qualifier only when Nólë inferred the task
// (detected/default), never for an explicitly supplied one.
func TestSearchRoutingInsightTaskSourceQualifier(t *testing.T) {
	base := SearchResponse{Task: TaskNews, Provider: "brave", Route: []string{"brave"}, Results: []SearchResult{{Title: "x"}}}

	detected := base
	detected.TaskSource = TaskSourceDetected
	if s := BuildSearchRoutingInsight(detected); !strings.Contains(s, "(task detected)") {
		t.Fatalf("detected insight should carry qualifier, got %q", s)
	}

	def := base
	def.TaskSource = TaskSourceDefault
	if s := BuildSearchRoutingInsight(def); !strings.Contains(s, "(task default)") {
		t.Fatalf("default insight should carry qualifier, got %q", s)
	}

	supplied := base
	supplied.TaskSource = TaskSourceSupplied
	if s := BuildSearchRoutingInsight(supplied); strings.Contains(s, "(task ") {
		t.Fatalf("supplied insight must NOT carry a task qualifier, got %q", s)
	}
}
