package brave

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestBravePassesPublishedAtPreferringPageAge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := braveSearchResponse{Web: &braveWebResults{Results: []braveWebResult{
			{Title: "A", URL: "https://a", Description: "da", PageAge: "2026-06-01T02:34:19", Age: "6 hours ago"},
			{Title: "B", URL: "https://b", Description: "db", Age: "2 days ago"}, // only age
			{Title: "C", URL: "https://c", Description: "dc"},                    // neither
		}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{apiKey: "k", httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)}}
	out, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out.Results[0].PublishedAt != "2026-06-01T02:34:19" {
		t.Fatalf("result[0] should prefer page_age, got %q", out.Results[0].PublishedAt)
	}
	if out.Results[1].PublishedAt != "2 days ago" {
		t.Fatalf("result[1] should fall back to age, got %q", out.Results[1].PublishedAt)
	}
	if out.Results[2].PublishedAt != "" {
		t.Fatalf("result[2] should be empty, got %q", out.Results[2].PublishedAt)
	}
	for i, r := range out.Results {
		if r.Score != nil {
			t.Fatalf("brave exposes no score; result[%d].Score should be nil", i)
		}
	}
}

func TestBraveFreshnessForRecencyTasks(t *testing.T) {
	var gotFreshness string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFreshness = r.URL.Query().Get("freshness")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(braveSearchResponse{Web: nil})
	}))
	defer srv.Close()
	p := Provider{apiKey: "k", httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)}}

	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskNews})
	if gotFreshness != "pm" {
		t.Fatalf("news freshness = %q, want pm", gotFreshness)
	}
	gotFreshness = "sentinel"
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskFactcheck})
	if gotFreshness != "pm" {
		t.Fatalf("factcheck freshness = %q, want pm", gotFreshness)
	}
	gotFreshness = "sentinel"
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "q", Task: core.TaskGeneral})
	if gotFreshness != "" {
		t.Fatalf("general must send no freshness, got %q", gotFreshness)
	}
}
