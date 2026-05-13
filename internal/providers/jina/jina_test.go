package jina

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

type redirectTransport struct {
	baseURL string
}

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header.Clone()
	return http.DefaultTransport.RoundTrip(newReq)
}

func TestNewHasHTTPTimeout(t *testing.T) {
	p := New(WithAPIKey("test-key"))
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestJinaSearchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("expected /, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["q"] != "nole" {
			t.Fatalf("expected query nole, got %#v", body["q"])
		}
		_ = json.NewEncoder(w).Encode(jinaSearchResponse{
			Code: 200,
			Data: []jinaSearchResult{{Title: "Nólë", URL: "https://example.com", Description: "Deep knowledge"}},
		})
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "jina" || len(resp.Results) != 1 || resp.Results[0].Title != "Nólë" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestJinaSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestJinaExtractHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("expected /, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(jinaReaderResponse{
			Code: 200,
			Data: jinaReaderData{Title: "Nólë", URL: "https://example.com", Content: "reader content"},
		})
	}))
	defer srv.Close()

	p := Provider{apiKey: "test-key", httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Content != "reader content" || resp.Metadata["title"] != "Nólë" {
		t.Fatalf("unexpected extract response: %#v", resp)
	}
}

func TestJinaMissingAPIKey(t *testing.T) {
	p := Provider{apiKey: "", httpClient: &http.Client{Transport: redirectTransport{baseURL: "http://127.0.0.1"}}}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("expected missing key error")
	}
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"}); err == nil {
		t.Fatal("expected missing key error")
	}
}
