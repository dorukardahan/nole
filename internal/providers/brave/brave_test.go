package brave

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

func TestBraveSearchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Error("missing or wrong API key header")
			w.WriteHeader(401)
			return
		}
		resp := braveSearchResponse{
			Web: &braveWebResults{
				Results: []braveWebResult{
					{Title: "Test Result", URL: "https://example.com", Description: "A test result"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Create provider with custom transport pointing to test server
	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Task: core.TaskGeneral, Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Test Result" {
		t.Errorf("expected 'Test Result', got %q", resp.Results[0].Title)
	}
}

func TestBraveSearchUsesNewsEndpointForRecencyTasks(t *testing.T) {
	for _, tc := range []struct {
		name string
		task core.TaskType
	}{
		{name: "news", task: core.TaskNews},
		{name: "factcheck", task: core.TaskFactcheck},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotFreshness string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotFreshness = r.URL.Query().Get("freshness")
				if r.URL.Path != "/res/v1/news/search" {
					http.Error(w, "wrong endpoint", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"type":"news","results":[{"type":"news_result","title":"Fresh story","url":"https://example.com/news","description":"recent context","page_age":"2026-06-04T12:00:00"}]}`))
			}))
			defer srv.Close()

			p := Provider{
				apiKey:     "test-key",
				httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
			}
			resp, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Task: tc.task, Limit: 5})
			if err != nil {
				t.Fatalf("search failed: %v (path %q)", err, gotPath)
			}
			if gotPath != "/res/v1/news/search" {
				t.Fatalf("path = %q, want Brave News endpoint", gotPath)
			}
			if gotFreshness != "pm" {
				t.Fatalf("freshness = %q, want pm", gotFreshness)
			}
			if len(resp.Results) != 1 {
				t.Fatalf("expected 1 news result, got %d", len(resp.Results))
			}
			if resp.Results[0].Title != "Fresh story" || resp.Results[0].PublishedAt != "2026-06-04T12:00:00" {
				t.Fatalf("unexpected result: %#v", resp.Results[0])
			}
		})
	}
}

func TestBraveSearchKeepsNonRecencyTasksOnWebEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name string
		task core.TaskType
	}{
		{name: "general", task: core.TaskGeneral},
		{name: "docs", task: core.TaskDocs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				if r.URL.Path != "/res/v1/web/search" {
					http.Error(w, "wrong endpoint", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(braveSearchResponse{Web: nil})
			}))
			defer srv.Close()

			p := Provider{
				apiKey:     "test-key",
				httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
			}
			if _, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Task: tc.task, Limit: 5}); err != nil {
				t.Fatalf("search failed: %v (path %q)", err, gotPath)
			}
			if gotPath != "/res/v1/web/search" {
				t.Fatalf("path = %q, want Brave Web endpoint", gotPath)
			}
		})
	}
}

func TestBraveSearchClampsCountToBraveMax(t *testing.T) {
	// Brave Web Search documents a hard max of 20 for `count`; an over-large
	// limit must be clamped to 20 in the built request rather than passed through
	// (which yields a guaranteed non-retryable HTTP 422).
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(braveSearchResponse{Web: nil})
	}))
	defer srv.Close()

	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Limit: 100}); err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotCount != "20" {
		t.Fatalf("count = %q, want clamped to 20 for over-large limit", gotCount)
	}
}

func TestBraveSearchClampsNewsCountToBraveNewsMax(t *testing.T) {
	// Brave News Search documents count 1..50. News/factcheck tasks should use
	// the News cap rather than the stricter Web Search cap of 20.
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"news","results":[]}`))
	}))
	defer srv.Close()

	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Task: core.TaskNews, Limit: 100}); err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotCount != "50" {
		t.Fatalf("count = %q, want 50", gotCount)
	}
}

func TestBraveSearchClampsCountFloorToOne(t *testing.T) {
	// A zero/unset limit must floor to 1 (the documented minimum), not 0.
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(braveSearchResponse{Web: nil})
	}))
	defer srv.Close()

	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Limit: 0}); err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotCount != "1" {
		t.Fatalf("count = %q, want floored to 1 for zero limit", gotCount)
	}
}

func TestBraveSearchNoAPIKey(t *testing.T) {
	p := Provider{apiKey: ""}
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestBraveSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestBraveSearchEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := braveSearchResponse{Web: nil}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{
		apiKey:     "test-key",
		httpClient: &http.Client{Transport: newRedirectTransport(srv.URL)},
	}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "test"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
}

func TestBraveExtractNotSupported(t *testing.T) {
	p := Provider{apiKey: "test-key"}
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error: brave does not support extract")
	}
}

func TestBraveStatusWithKey(t *testing.T) {
	p := Provider{apiKey: "test-key"}
	status := p.Status(context.Background())
	if !status.Available {
		t.Error("expected provider to be available with key")
	}
}

func TestBraveStatusWithoutKey(t *testing.T) {
	p := Provider{apiKey: ""}
	status := p.Status(context.Background())
	if status.Available {
		t.Error("expected provider to be unavailable without key")
	}
}

func TestBraveName(t *testing.T) {
	p := Provider{}
	if p.Name() != "brave" {
		t.Errorf("expected name 'brave', got %q", p.Name())
	}
}

// newRedirectTransport creates an http.RoundTripper that redirects all requests
// to the given base URL, preserving path and query.
func newRedirectTransport(baseURL string) http.RoundTripper {
	return &redirectTransport{baseURL: baseURL}
}

type redirectTransport struct {
	baseURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect to test server
	newURL := t.baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}
