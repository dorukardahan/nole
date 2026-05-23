package ddgs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	p := New()
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestDDGSSearchParsesHTMLWithoutNetwork(t *testing.T) {
	var sawQuery bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		sawQuery = strings.Contains(string(body), "q=nole")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body>
<a rel="nofollow" class="result__a" href="https://example.com/nole">N&oacute;l&euml; Result</a>
<a class="result__snippet" href="https://example.com/nole">Deep &amp; useful <b>research</b>.</a>
<a class="result__a" href="https://duckduckgo.com/y.js?ad=1">Ad Result</a>
</body></html>`))
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Task: core.TaskGeneral, Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !sawQuery {
		t.Fatal("expected request body to contain encoded query")
	}
	if resp.Provider != "ddgs" || len(resp.Results) != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Results[0].URL != "https://example.com/nole" {
		t.Fatalf("unexpected URL %q", resp.Results[0].URL)
	}
	if !strings.Contains(resp.Results[0].Snippet, "Deep & useful") {
		t.Fatalf("unexpected snippet %q", resp.Results[0].Snippet)
	}
}

func TestDDGSSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestDDGSExtractNotSupported(t *testing.T) {
	p := New()
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"}); err == nil {
		t.Fatal("expected extract unsupported error")
	}
}

func TestDDGSStatus(t *testing.T) {
	status := New().Status(context.Background())
	if !status.Available || status.Name != "ddgs" || !core.HasCapability(status.Capabilities, core.CapabilitySearch) {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestDDGSRequestFormatMatchesSearXNGCanonical(t *testing.T) {
	// DDG's no-JS HTML endpoint flags requests as bots when (a) the form field
	// `b` is non-empty on page 1 or (b) browser-parity headers are missing.
	// SearXNG's canonical implementation sets b="" and emits Referer + the
	// Sec-Fetch-* family; mirror that to avoid the 202 Ratelimit response.
	var captured struct {
		body    string
		headers http.Header
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.body = string(body)
		captured.headers = r.Header.Clone()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body></body></html>`))
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	_, _ = p.Search(context.Background(), core.SearchRequest{Query: "nole", Task: core.TaskGeneral, Limit: 5})

	if !strings.Contains(captured.body, "b=&") && !strings.HasSuffix(captured.body, "b=") {
		t.Fatalf("expected b= (empty) in form body, got %q", captured.body)
	}
	if strings.Contains(captured.body, "b=Web+Search") {
		t.Fatalf("regression: b=Web Search must not be sent, got %q", captured.body)
	}
	wantHeaders := map[string]string{
		"Referer":        "https://html.duckduckgo.com/",
		"Sec-Fetch-Dest": "document",
		"Sec-Fetch-Mode": "navigate",
		"Sec-Fetch-Site": "same-origin",
		"Sec-Fetch-User": "?1",
	}
	for h, want := range wantHeaders {
		if got := captured.headers.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}
}

func TestDDGSRateLimitedReturns202AsRateLimited(t *testing.T) {
	// DDG signals rate-limit / bot-block with HTTP 202 and a "202 Ratelimit"
	// body. The provider must surface this distinctly so callers (and the
	// bench classifier) treat it as transient throttling rather than a generic
	// upstream error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("202 Ratelimit"))
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}}
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected error for 202 rate-limit response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "rate limited") || !strings.Contains(msg, "202") {
		t.Fatalf("error %q should mention rate limited and 202", msg)
	}
}
