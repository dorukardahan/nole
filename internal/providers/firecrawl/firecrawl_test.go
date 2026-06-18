package firecrawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

func TestNewHasHTTPTimeout(t *testing.T) {
	p := New(WithAPIKey("test-key"))
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestFirecrawlSearchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("expected /search, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST /search, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["query"] != "nole" {
			t.Fatalf("expected query nole, got %#v", body["query"])
		}
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: true,
			Data: firecrawlSearchData{Web: []firecrawlSearchWebResult{{
				Title:       "Nólë",
				URL:         "https://example.com/nole",
				Description: "Deep research router",
			}}},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Task: core.TaskResearch, Limit: 3})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "firecrawl" || len(resp.Results) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Results[0].Snippet != "Deep research router" {
		t.Fatalf("unexpected snippet %q", resp.Results[0].Snippet)
	}
}

func TestFirecrawlSearchOptionsMapCountryAndFreshness(t *testing.T) {
	var body firecrawlSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: true,
			Data:    firecrawlSearchData{Web: []firecrawlSearchWebResult{{Title: "Nólë", URL: "https://example.com/nole", Description: "router"}}},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Task: core.TaskGeneral, Limit: 3, Options: core.SearchOptions{Country: "us", Freshness: "pd"}})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if body.Country != "us" || body.TBS != "qdr:d" {
		t.Fatalf("country/tbs = %q/%q, want us/qdr:d", body.Country, body.TBS)
	}
}

func TestFirecrawlAcademicSearchUsesResearchIndexGET(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotK, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		gotK = r.URL.Query().Get("k")
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"results": []map[string]any{{
				"paperId":   "paper-1",
				"primaryId": "arxiv:2401.01234",
				"ids":       map[string]any{"arxiv": []string{"2401.01234"}},
				"title":     "Graph RAG for scientific agents",
				"abstract":  "A grounded abstract from the research index.",
				"score":     0.82,
			}},
		})
	}))
	defer srv.Close()

	key := "test-key"
	p := New(WithAPIKey(key), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "graph rag", Task: core.TaskAcademic, Limit: 7})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/search/research/papers" {
		t.Fatalf("academic search used %s %s, want GET /search/research/papers", gotMethod, gotPath)
	}
	if gotQuery != "graph rag" || gotK != "7" {
		t.Fatalf("query/k = %q/%q, want graph rag/7", gotQuery, gotK)
	}
	if gotAuth != "Bearer "+key {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1: %#v", len(resp.Results), resp.Results)
	}
	got := resp.Results[0]
	if got.Title != "Graph RAG for scientific agents" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.URL != "https://arxiv.org/abs/2401.01234" {
		t.Fatalf("url = %q, want canonical arXiv URL", got.URL)
	}
	if got.Snippet != "A grounded abstract from the research index." {
		t.Fatalf("snippet = %q", got.Snippet)
	}
	if got.Score == nil || *got.Score != 0.82 {
		t.Fatalf("score = %#v, want 0.82", got.Score)
	}
	if got.PublishedAt != "" {
		t.Fatalf("published_at = %q, want empty when provider omits a clear date", got.PublishedAt)
	}
}

func TestFirecrawlAcademicSearchMappingPrefersReturnedURLAndLeavesUnknownIDEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"results": []map[string]any{
				{
					"paperId":   "paper-1",
					"primaryId": "arxiv:2401.09999",
					"url":       "https://doi.org/10.1000/example",
					"title":     "Provider URL wins",
					"abstract":  "Use the returned URL instead of deriving another one.",
				},
				{
					"paperId":   "paper-2",
					"primaryId": "doi:10.1000/unknown",
					"title":     "Unknown canonical URL",
					"abstract":  "Do not fabricate a URL for non-arXiv identifiers.",
				},
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "agents", Task: core.TaskAcademic, Limit: 2})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2: %#v", len(resp.Results), resp.Results)
	}
	if resp.Results[0].URL != "https://doi.org/10.1000/example" {
		t.Fatalf("returned URL not preserved: %#v", resp.Results[0])
	}
	if resp.Results[1].URL != "" {
		t.Fatalf("unknown id should leave URL empty, got %#v", resp.Results[1])
	}
}

func TestFirecrawlAcademicSearchEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": []any{}})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "no hits", Task: core.TaskAcademic, Limit: 2})
	if err != nil {
		t.Fatalf("empty academic response should not error: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %#v, want empty", resp.Results)
	}
}

func TestFirecrawlAcademicSearchMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"results":[`))
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "broken", Task: core.TaskAcademic, Limit: 2}); err == nil {
		t.Fatal("expected malformed academic response error")
	}
}

func TestFirecrawlSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestFirecrawlExtractHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scrape" {
			t.Fatalf("expected /scrape, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(firecrawlScrapeResponse{
			Success: true,
			Data: firecrawlScrapeData{
				Markdown: "# Nólë\nResearch content",
				Metadata: map[string]interface{}{"title": "Nólë"},
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Content != "# Nólë\nResearch content" || resp.Metadata["title"] != "Nólë" {
		t.Fatalf("unexpected extract response: %#v", resp)
	}
}

func TestFirecrawlMissingAPIKey(t *testing.T) {
	p := New(WithAPIKey(""))
	p.apiKey = ""
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := p.Search(ctx, core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("expected missing key error")
	}
	if _, err := p.Extract(ctx, core.ExtractRequest{URL: "https://example.com"}); err == nil {
		t.Fatal("expected missing key error")
	}
}
