package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func f64(v float64) *float64 { return &v }

func TestTavilyPassesScoreAndPublishedDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tavilySearchResponse{
			Query: "q",
			Results: []tavilyResult{
				{Title: "A", URL: "https://a", Content: "ca", Score: f64(0.97), PublishedDate: "Tue, 19 May 2026 18:59:59 GMT"},
				{Title: "B", URL: "https://b", Content: "cb", Score: f64(0.41)},
				{Title: "C", URL: "https://c", Content: "cc"}, // provider omits score AND date
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{apiKey: "k", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}
	out, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskGeneral, Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("want 3 results, got %d", len(out.Results))
	}
	if out.Results[0].Score == nil || *out.Results[0].Score != 0.97 {
		t.Fatalf("score[0] = %v, want 0.97", out.Results[0].Score)
	}
	if out.Results[0].PublishedAt != "Tue, 19 May 2026 18:59:59 GMT" {
		t.Fatalf("publishedAt[0] = %q", out.Results[0].PublishedAt)
	}
	if out.Results[1].PublishedAt != "" {
		t.Fatalf("publishedAt[1] should be empty, got %q", out.Results[1].PublishedAt)
	}
	// Absent score must stay nil — never fabricated as 0.0 (north-star: no inventing).
	if out.Results[2].Score != nil {
		t.Fatalf("score[2] should be nil when the provider omits it, got %v", *out.Results[2].Score)
	}
	// Distinct pointers guard against aliasing across results.
	if out.Results[0].Score == out.Results[1].Score {
		t.Fatalf("Score pointers must be distinct per result")
	}
}

func TestTavilyFreshnessForRecencyTasks(t *testing.T) {
	var got tavilySearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tavilySearchResponse{Query: got.Query})
	}))
	defer srv.Close()
	p := Provider{apiKey: "k", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}

	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskNews})
	if got.Topic != "news" || got.TimeRange != "month" {
		t.Fatalf("news: topic=%q time_range=%q, want news/month", got.Topic, got.TimeRange)
	}

	got = tavilySearchRequest{}
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskFactcheck})
	if got.TimeRange != "month" || got.Topic != "news" {
		t.Fatalf("factcheck: topic=%q time_range=%q, want news/month (so Tavily returns published_date)", got.Topic, got.TimeRange)
	}

	got = tavilySearchRequest{}
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskGeneral})
	if got.Topic != "" || got.TimeRange != "" {
		t.Fatalf("general must send no freshness, got topic=%q time_range=%q", got.Topic, got.TimeRange)
	}

	// Research must keep advanced depth and add no freshness (regression guard).
	got = tavilySearchRequest{}
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskResearch})
	if got.SearchDepth != "advanced" || got.Topic != "" || got.TimeRange != "" {
		t.Fatalf("research: depth=%q topic=%q time_range=%q, want advanced/\"\"/\"\"", got.SearchDepth, got.Topic, got.TimeRange)
	}
}
