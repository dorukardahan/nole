package wikipedia

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
)

// redirectTransport rewrites the real en.wikipedia.org request to a local
// httptest server, preserving method/headers/query, so every test runs with no
// network (mirrors the DDGS test harness).
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

func testProvider(baseURL string) Provider {
	return Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: baseURL}}}
}

// fixtureJSON is a canonical formatversion=2 list=search response with two hits,
// the first carrying searchmatch markup + entities in its snippet.
const fixtureJSON = `{
  "batchcomplete": true,
  "query": {
    "search": [
      {"ns":0,"title":"Nelson Mandela","pageid":21492751,"snippet":"<span class=\"searchmatch\">Nelson</span> Mandela&#039;s legacy &amp; the &quot;ANC&quot;","timestamp":"2026-05-30T12:00:00Z"},
      {"ns":0,"title":"Alan Turing","pageid":1208,"snippet":"computer scientist","timestamp":"2026-04-01T08:30:00Z"}
    ]
  }
}`

func TestNewHasHTTPTimeout(t *testing.T) {
	p := New()
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestWikipediaSearchParsesJSONWithoutNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureJSON))
	}))
	defer srv.Close()

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "mandela", Task: core.TaskFactcheck, Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "wikipedia" || resp.Query != "mandela" || resp.Task != core.TaskFactcheck {
		t.Fatalf("unexpected response envelope: %#v", resp)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d (%#v)", len(resp.Results), resp.Results)
	}
	r0 := resp.Results[0]
	if r0.Title != "Nelson Mandela" {
		t.Errorf("title = %q", r0.Title)
	}
	if r0.URL != "https://en.wikipedia.org/wiki/Nelson_Mandela" {
		t.Errorf("URL = %q", r0.URL)
	}
	if strings.Contains(r0.Snippet, "<span") || strings.Contains(r0.Snippet, "searchmatch") {
		t.Errorf("snippet still has markup: %q", r0.Snippet)
	}
	// Real MediaWiki snippets emit the zero-padded &#039; apostrophe and &quot;;
	// the cleaned snippet must decode all of them (a leaked "&#039;" is the bug
	// this fixture guards against).
	if !strings.Contains(r0.Snippet, `Nelson Mandela's legacy & the "ANC"`) {
		t.Errorf("snippet not cleaned as expected: %q", r0.Snippet)
	}
	if strings.Contains(r0.Snippet, "&#039;") || strings.Contains(r0.Snippet, "&#39;") || strings.Contains(r0.Snippet, "&quot;") {
		t.Errorf("snippet leaked a raw HTML entity: %q", r0.Snippet)
	}
	if r0.PublishedAt != "2026-05-30T12:00:00Z" {
		t.Errorf("PublishedAt = %q (timestamp should pass through verbatim)", r0.PublishedAt)
	}
	if r0.Provider != "wikipedia" {
		t.Errorf("per-result provider = %q", r0.Provider)
	}
	if r0.Score != nil {
		t.Errorf("Score must stay nil (MediaWiki exposes no relevance score), got %v", *r0.Score)
	}
}

func TestWikipediaSearchRequestShape(t *testing.T) {
	var gotQuery url.Values
	var gotMethod, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotUA = r.Header.Get("User-Agent")
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	defer srv.Close()

	_, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "café & bar", Task: core.TaskPeople, Limit: 3})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	want := map[string]string{
		"action": "query", "list": "search", "srsearch": "café & bar",
		"srnamespace": "0", "format": "json", "formatversion": "2",
		"maxlag": "5", "srlimit": "3", "srprop": "snippet|timestamp",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query param %s = %q, want %q", k, got, v)
		}
	}
	// Mandatory descriptive, identifying UA — present, names the client, carries
	// a contact URL, and is NOT the Go default or a browser-spoof.
	if !strings.HasPrefix(gotUA, "Nole/") || !strings.Contains(gotUA, "github.com/dorukardahan/nole") {
		t.Errorf("User-Agent = %q, want descriptive Nole UA with contact URL", gotUA)
	}
	if strings.Contains(gotUA, "Go-http-client") || strings.Contains(gotUA, "Mozilla/") {
		t.Errorf("User-Agent must not be the Go default or a browser-spoof: %q", gotUA)
	}
}

func TestWikipediaSnippetStripsSearchMatchMarkup(t *testing.T) {
	// Use the entity forms the LIVE MediaWiki API actually emits: the zero-padded
	// apostrophe &#039; (the common case for possessives/contractions), &quot;,
	// &amp;, and the named &nbsp;. A &#39;-only decoder would leak &#039; raw.
	in := `Foo <span class="searchmatch">bar</span> &amp; O&#039;Brien&nbsp;said &quot;hi&quot;`
	got := stripSearchMatch(in)
	want := `Foo bar & O'Brien said "hi"`
	if got != want {
		t.Fatalf("stripSearchMatch(%q) = %q, want %q", in, got, want)
	}
}

func TestWikipediaArticleURLEncoding(t *testing.T) {
	cases := map[string]string{
		"Alan Turing":      "https://en.wikipedia.org/wiki/Alan_Turing",
		"Mercury (planet)": "https://en.wikipedia.org/wiki/Mercury_%28planet%29",
		"Café":             "https://en.wikipedia.org/wiki/Caf%C3%A9",
		"AC/DC":            "https://en.wikipedia.org/wiki/AC/DC",
	}
	for title, want := range cases {
		if got := articleURL(title); got != want {
			t.Errorf("articleURL(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestWikipediaEmptyResultsIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	defer srv.Close()

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "asdkjhqwenonexistenttopic"})
	if err != nil {
		t.Fatalf("empty results must NOT be an error, got: %v", err)
	}
	if resp.Results == nil {
		t.Fatal("Results should be a non-nil empty slice")
	}
	if len(resp.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(resp.Results))
	}
}

func TestWikipediaHTTPError(t *testing.T) {
	const bodyMarker = "query-echo-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // 429
		_, _ = w.Write([]byte("rate stuff " + bodyMarker))
	}))
	defer srv.Close()

	_, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected HTTP error for 429")
	}
	rendered := safeerr.Message(err)
	if strings.Contains(err.Error(), bodyMarker) || strings.Contains(rendered, bodyMarker) {
		t.Fatalf("error leaked response body: %q / %q", err.Error(), rendered)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention the status code: %q", err.Error())
	}
}

func TestWikipediaMaxlagErrorIsRateLimited(t *testing.T) {
	// maxlag is reported as HTTP 200 with an error body whose info field can echo
	// host/lag detail — the error must classify as rate-limited and NOT leak info.
	const infoMarker = "Waiting-for-db-host-internal-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// HTTP 200 with an error block (MediaWiki's maxlag behaviour).
		_, _ = w.Write([]byte(`{"error":{"code":"maxlag","info":"` + infoMarker + `"}}`))
	}))
	defer srv.Close()

	_, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected error for maxlag error block")
	}
	msg := err.Error()
	if !strings.Contains(msg, "rate limited") || !strings.Contains(msg, "maxlag") {
		t.Errorf("error should classify as rate limited (maxlag): %q", msg)
	}
	if strings.Contains(msg, infoMarker) || strings.Contains(safeerr.Message(err), infoMarker) {
		t.Fatalf("error leaked the error.info detail: %q", msg)
	}
}

func TestWikipediaExtractNotSupported(t *testing.T) {
	if _, err := New().Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"}); err == nil {
		t.Fatal("expected extract unsupported error")
	}
	if core.HasCapability(New().Capabilities(), core.CapabilityExtract) {
		t.Fatal("wikipedia must NOT advertise extract capability")
	}
}

func TestWikipediaStatus(t *testing.T) {
	s := New().Status(context.Background())
	if !s.Available || s.Name != "wikipedia" {
		t.Fatalf("unexpected status: %#v", s)
	}
	if !core.HasCapability(s.Capabilities, core.CapabilitySearch) || !core.HasCapability(s.Capabilities, core.CapabilityStatus) {
		t.Fatalf("status missing expected capabilities: %#v", s.Capabilities)
	}
	// Keyless/unbreakered providers do not stamp cost/breaker fields themselves.
	if s.CostClass != "" || s.BreakerState != "" {
		t.Errorf("provider must not set CostClass/BreakerState itself: %#v", s)
	}
}

func TestWikipediaSearchLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Five results returned; req.Limit=2 must cap to 2.
		_, _ = w.Write([]byte(`{"query":{"search":[
			{"title":"A","pageid":1,"snippet":"a","timestamp":"t"},
			{"title":"B","pageid":2,"snippet":"b","timestamp":"t"},
			{"title":"C","pageid":3,"snippet":"c","timestamp":"t"},
			{"title":"D","pageid":4,"snippet":"d","timestamp":"t"},
			{"title":"E","pageid":5,"snippet":"e","timestamp":"t"}
		]}}`))
	}))
	defer srv.Close()

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x", Limit: 2})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(resp.Results))
	}
}

func TestWikipediaContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	_, err := testProvider(srv.URL).Search(ctx, core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected error for a cancelled context")
	}
	// Pin the cancellation path specifically (the error is %w-wrapped through
	// DoWithRetry), not just "some error" — the server would otherwise answer 200.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got: %v", err)
	}
}

func TestWikipediaSearchDecodesGzipResponse(t *testing.T) {
	// Guards the deliberate decision NOT to set Accept-Encoding manually: net/http
	// requests gzip itself and transparently decompresses. The server gzips the
	// body; Search must still parse it. (Setting Accept-Encoding by hand would
	// break this — the transport would hand back raw gzip bytes.)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(fixtureJSON))
		_ = gz.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "mandela", Limit: 5})
	if err != nil {
		t.Fatalf("search failed on gzip response: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results from gzip response, got %d", len(resp.Results))
	}
}
