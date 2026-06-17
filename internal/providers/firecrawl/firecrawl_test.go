package firecrawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestFirecrawlKeylessSearchOmitsAuthorization(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: true,
			Data:    firecrawlSearchData{Web: []firecrawlSearchWebResult{{Title: "Nólë", URL: "https://example.com/nole", Description: "router"}}},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey(""), WithBaseURL(srv.URL))
	p.apiKey = ""
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err != nil {
		t.Fatalf("keyless search failed: %v", err)
	}
	if auth != "" {
		t.Fatalf("keyless request must omit Authorization header, got %q", auth)
	}
}

func TestFirecrawlKeylessExtractOmitsAuthorization(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(firecrawlScrapeResponse{Success: true, Data: firecrawlScrapeData{Markdown: "# ok"}})
	}))
	defer srv.Close()

	p := New(WithAPIKey(""), WithBaseURL(srv.URL))
	p.apiKey = ""
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"}); err != nil {
		t.Fatalf("keyless extract failed: %v", err)
	}
	if auth != "" {
		t.Fatalf("keyless request must omit Authorization header, got %q", auth)
	}
}

func TestFirecrawlStatusWithoutKeyIsAvailable(t *testing.T) {
	p := New(WithAPIKey(""))
	p.apiKey = ""
	status := p.Status(context.Background())
	if !status.Available {
		t.Fatalf("expected keyless firecrawl to be available, got %#v", status)
	}
	if status.Reason == "" {
		t.Fatal("expected status reason to mention keyless/account-backed mode")
	}
}
