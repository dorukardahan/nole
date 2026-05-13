package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

type testTransport struct {
	baseURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func TestNewHasHTTPTimeout(t *testing.T) {
	p := New(WithAPIKey("test-key"))
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestTavilySearchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body tavilySearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(400)
			return
		}
		resp := tavilySearchResponse{
			Query: body.Query,
			Results: []tavilyResult{
				{Title: "Tavily Result", URL: "https://example.com", Content: "test content", Score: 0.95},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "test", Task: core.TaskGeneral, Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Tavily Result" {
		t.Errorf("expected 'Tavily Result', got %q", resp.Results[0].Title)
	}
}

func TestTavilySearchNoAPIKey(t *testing.T) {
	p := Provider{apiKey: ""}
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestTavilySearchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestTavilySearchUsesAdvancedForResearch(t *testing.T) {
	var receivedBody tavilySearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		resp := tavilySearchResponse{Query: receivedBody.Query}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "test", Task: core.TaskResearch})
	if receivedBody.SearchDepth != "advanced" {
		t.Errorf("expected 'advanced' depth for research task, got %q", receivedBody.SearchDepth)
	}
}

func TestTavilyExtractHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tavilyExtractResponse{
			Results: []tavilyExtractResult{
				{URL: "https://example.com", RawContent: "extracted content"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: &testTransport{srv.URL}}}
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Content != "extracted content" {
		t.Errorf("expected 'extracted content', got %q", resp.Content)
	}
}

func TestTavilyExtractNoAPIKey(t *testing.T) {
	p := Provider{apiKey: ""}
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestTavilyStatusWithKey(t *testing.T) {
	p := Provider{apiKey: "test-key"}
	status := p.Status(context.Background())
	if !status.Available {
		t.Error("expected provider to be available with key")
	}
}

func TestTavilyStatusWithoutKey(t *testing.T) {
	p := Provider{apiKey: ""}
	status := p.Status(context.Background())
	if status.Available {
		t.Error("expected provider to be unavailable without key")
	}
}

func TestTavilyName(t *testing.T) {
	p := Provider{}
	if p.Name() != "tavily" {
		t.Errorf("expected name 'tavily', got %q", p.Name())
	}
}
