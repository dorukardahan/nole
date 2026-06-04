package arxiv

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/safeerr"
)

// redirectTransport rewrites the real export.arxiv.org request to a local
// httptest server, preserving method/headers/query, so every test runs with no
// network (mirrors the wikipedia/DDGS harnesses).
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

// fixtureAtom is a realistic arXiv Atom query response with a feed-level <id>
// (the /api/{token} URL that must NOT be confused for an entry id), a query-echo
// feed <title>, and two real entries. Entry 1 carries the abstract-page link as
// rel="alternate" type="text/html", a wrapped/indented summary with XML entities
// (&amp;, &gt;) and LaTeX, multiple authors/categories, and the http:// entry id;
// entry 2 has no alternate link (exercising the id-derived https fallback).
const fixtureAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns:arxiv="http://arxiv.org/schemas/atom" xmlns="http://www.w3.org/2005/Atom">
  <id>http://arxiv.org/api/dcXY_TOKEN_must_not_be_a_result</id>
  <title>ArXiv Query: search_query=all:electron&amp;start=0</title>
  <updated>2026-06-04T00:00:00Z</updated>
  <link href="http://arxiv.org/api/dcXY?searchtype=all" rel="self" type="application/atom+xml"/>
  <opensearch:totalResults>2</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>2</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/2301.12345v1</id>
    <updated>2023-01-30T18:00:00Z</updated>
    <published>2023-01-29T12:00:00Z</published>
    <title>Retrieval-Augmented Generation:
      a Survey of $\alpha$-weighted methods</title>
    <summary>  We study retrieval &amp; generation, showing the 1D-&gt;2D
  transition matters for $\alpha$ and the ^{-1} scaling of correlated
  systems across many tokens.</summary>
    <author><name>Ada Lovelace</name></author>
    <author><name>Alan Turing</name></author>
    <link href="https://arxiv.org/abs/2301.12345v1" rel="alternate" type="text/html"/>
    <link href="https://arxiv.org/pdf/2301.12345v1" rel="related" type="application/pdf" title="pdf"/>
    <category term="cs.CL" scheme="http://arxiv.org/schemas/atom"/>
    <category term="cs.IR" scheme="http://arxiv.org/schemas/atom"/>
    <arxiv:primary_category term="cs.CL"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/cond-mat/0011267v2</id>
    <updated>2000-11-16T10:00:00Z</updated>
    <published>2000-11-15T16:19:15Z</published>
    <title>The electronic structure of cuprates</title>
    <summary>A short abstract.</summary>
    <author><name>Mark S. Golden</name></author>
    <link href="https://arxiv.org/pdf/cond-mat/0011267v2" rel="related" type="application/pdf" title="pdf"/>
  </entry>
</feed>`

// errorEntryAtom is arXiv's HTTP-200 error response shape: one <entry> whose id
// is under the http://arxiv.org/api/errors namespace.
const errorEntryAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>http://arxiv.org/api/errors</id>
  <title>ArXiv Query: search_query=all:%%%</title>
  <entry>
    <id>http://arxiv.org/api/errors#incorrect_id_format_for_must_leak_nothing</id>
    <title>Error</title>
    <summary>incorrect id format for query-echo-must-not-leak</summary>
    <link href="http://arxiv.org/api/errors" rel="alternate" type="text/html"/>
  </entry>
</feed>`

// emptyAtom is a no-hit response: a valid feed with totalResults=0 and no entries.
const emptyAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns="http://www.w3.org/2005/Atom">
  <id>http://arxiv.org/api/emptytoken</id>
  <title>ArXiv Query: search_query=all:asdkjhqwenonexistenttopic</title>
  <opensearch:totalResults>0</opensearch:totalResults>
</feed>`

func atomServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewHasHTTPTimeout(t *testing.T) {
	p := New()
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected default HTTP client timeout, got %#v", p.httpClient)
	}
}

func TestArxivSearchParsesAtomWithoutNetwork(t *testing.T) {
	srv := atomServer(t, fixtureAtom)

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "rag survey", Task: core.TaskAcademic, Limit: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.Provider != "arxiv" || resp.Query != "rag survey" || resp.Task != core.TaskAcademic {
		t.Fatalf("unexpected response envelope: %#v", resp)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d (%#v)", len(resp.Results), resp.Results)
	}

	r0 := resp.Results[0]
	// Title whitespace-collapsed (the fixture wraps it across a line + indentation).
	if r0.Title != "Retrieval-Augmented Generation: a Survey of $\\alpha$-weighted methods" {
		t.Errorf("title not collapsed/preserved as expected: %q", r0.Title)
	}
	// URL comes from the rel="alternate" type="text/html" link (already https).
	if r0.URL != "https://arxiv.org/abs/2301.12345v1" {
		t.Errorf("URL = %q, want the alternate-link abs page", r0.URL)
	}
	// Entities decoded by encoding/xml (NOT double-unescaped), whitespace collapsed,
	// LaTeX preserved verbatim.
	if !strings.Contains(r0.Snippet, "retrieval & generation") {
		t.Errorf("snippet did not decode &amp; -> &: %q", r0.Snippet)
	}
	if !strings.Contains(r0.Snippet, "1D->2D") {
		t.Errorf("snippet did not decode &gt; -> >: %q", r0.Snippet)
	}
	if !strings.Contains(r0.Snippet, "$\\alpha$") || !strings.Contains(r0.Snippet, "^{-1}") {
		t.Errorf("snippet must preserve literal LaTeX verbatim: %q", r0.Snippet)
	}
	if strings.Contains(r0.Snippet, "&amp;") || strings.Contains(r0.Snippet, "&gt;") || strings.Contains(r0.Snippet, "\n") {
		t.Errorf("snippet leaked a raw entity or newline: %q", r0.Snippet)
	}
	if r0.PublishedAt != "2023-01-29T12:00:00Z" {
		t.Errorf("PublishedAt = %q (must be <published>, verbatim)", r0.PublishedAt)
	}
	if r0.Provider != "arxiv" {
		t.Errorf("per-result provider = %q", r0.Provider)
	}
	if r0.Score != nil {
		t.Errorf("Score must stay nil (arXiv exposes no relevance score), got %v", *r0.Score)
	}

	// Entry 2 has no alternate link → URL derived from the http:// id, served https.
	r1 := resp.Results[1]
	if r1.URL != "https://arxiv.org/abs/cond-mat/0011267v2" {
		t.Errorf("entry-2 URL = %q, want https derived from the http:// id", r1.URL)
	}
}

func TestArxivSearchRequestShape(t *testing.T) {
	var gotQuery url.Values
	var gotMethod, gotUA, gotAcceptEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotUA = r.Header.Get("User-Agent")
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(emptyAtom))
	}))
	defer srv.Close()

	rawQuery := `café & "ML" #tag`
	_, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: rawQuery, Task: core.TaskAcademic, Limit: 7})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	// The whole agent query is a single encoded search_query value (prefixed all:),
	// never split into extra params — exactly 3 params (search_query/start/max_results).
	if got := gotQuery.Get("search_query"); got != "all:"+rawQuery {
		t.Errorf("search_query = %q, want %q", got, "all:"+rawQuery)
	}
	if len(gotQuery) != 3 {
		t.Errorf("expected exactly 3 query params (no injection), got %d: %#v", len(gotQuery), gotQuery)
	}
	if gotQuery.Get("start") != "0" || gotQuery.Get("max_results") != "7" {
		t.Errorf("start/max_results = %q/%q, want 0/7", gotQuery.Get("start"), gotQuery.Get("max_results"))
	}
	// Mandatory descriptive, identifying UA — names the client, carries a contact
	// URL, and is NOT the Go default or a browser-spoof.
	if !strings.HasPrefix(gotUA, "Nole/") || !strings.Contains(gotUA, "github.com/dorukardahan/nole") {
		t.Errorf("User-Agent = %q, want descriptive Nole UA with contact URL", gotUA)
	}
	if strings.Contains(gotUA, "Go-http-client") || strings.Contains(gotUA, "Mozilla/") {
		t.Errorf("User-Agent must not be the Go default or a browser-spoof: %q", gotUA)
	}
	// We must not hand-set Accept-Encoding (net/http negotiates gzip transparently);
	// the value the server sees is the transport's own "gzip", never one we set.
	if gotAcceptEncoding != "" && gotAcceptEncoding != "gzip" {
		t.Errorf("Accept-Encoding = %q, want unset (transport-managed gzip only)", gotAcceptEncoding)
	}
}

func TestArxivSkipsErrorEntry(t *testing.T) {
	const bodyMarker = "query-echo-must-not-leak"
	srv := atomServer(t, errorEntryAtom)

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "%%%"})
	if err != nil {
		t.Fatalf("an arXiv error-entry feed must be an empty fall-through, not an error: %v", err)
	}
	if resp.Results == nil {
		t.Fatal("Results should be a non-nil empty slice")
	}
	if len(resp.Results) != 0 {
		t.Fatalf("error entry must be skipped, got %d results: %#v", len(resp.Results), resp.Results)
	}
	// The error entry's summary echoes the (malformed) query; skipping it means that
	// text never leaks into a result or error.
	for _, r := range resp.Results {
		if strings.Contains(r.Snippet, bodyMarker) || strings.Contains(r.URL, "api/errors") {
			t.Fatalf("error-entry content leaked into a result: %#v", r)
		}
	}
}

func TestArxivFeedLevelIDNotConfusedForEntry(t *testing.T) {
	// Guards the local-name <id> collision: the feed-level <id> is a /api/{token}
	// URL (no /abs/). A flat-struct parser would bind it and drop the one real
	// entry. The nested Feed{Entries} binding must return exactly the real entry.
	const oneRealEntry = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>http://arxiv.org/api/TOKEN_NOT_AN_ENTRY</id>
  <title>ArXiv Query: search_query=all:x</title>
  <entry>
    <id>http://arxiv.org/abs/2406.00001v1</id>
    <title>Only Real Paper</title>
    <summary>abstract</summary>
    <published>2024-06-01T00:00:00Z</published>
    <link href="https://arxiv.org/abs/2406.00001v1" rel="alternate" type="text/html"/>
  </entry>
</feed>`
	srv := atomServer(t, oneRealEntry)

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected exactly 1 real result (feed-id must not be mistaken for an entry), got %d: %#v", len(resp.Results), resp.Results)
	}
	if resp.Results[0].Title != "Only Real Paper" || resp.Results[0].URL != "https://arxiv.org/abs/2406.00001v1" {
		t.Fatalf("unexpected result: %#v", resp.Results[0])
	}
}

func TestArxivMixedRealAndErrorEntries(t *testing.T) {
	const mixed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>http://arxiv.org/api/tok</id>
  <entry>
    <id>http://arxiv.org/api/errors#some_error</id>
    <title>Error</title>
    <summary>bad</summary>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2406.99999v1</id>
    <title>Real</title>
    <summary>real abstract</summary>
    <published>2024-06-02T00:00:00Z</published>
    <link href="https://arxiv.org/abs/2406.99999v1" rel="alternate" type="text/html"/>
  </entry>
</feed>`
	srv := atomServer(t, mixed)

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "Real" {
		t.Fatalf("expected only the real entry, got %#v", resp.Results)
	}
}

func TestArxivEmptyResultsIsNotError(t *testing.T) {
	srv := atomServer(t, emptyAtom)

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

func TestArxivHTTPErrorNoBodyLeak(t *testing.T) {
	// The verified edge rate-limit is a 429 with a text/html "Rate exceeded." body
	// (NOT arXiv's Atom format, NO Retry-After). It must surface as an error naming
	// only the status, never the body, and only HTTP 200 may reach the XML parser.
	const bodyMarker = "Rate exceeded. internal-host-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusTooManyRequests) // 429
		_, _ = w.Write([]byte(bodyMarker))
	}))
	defer srv.Close()

	_, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected HTTP error for 429")
	}
	rendered := safeerr.Message(err)
	if strings.Contains(err.Error(), bodyMarker) || strings.Contains(rendered, bodyMarker) || strings.Contains(err.Error(), "Rate exceeded") {
		t.Fatalf("error leaked response body: %q / %q", err.Error(), rendered)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention the status code: %q", err.Error())
	}
}

func TestArxivDoesNotRetry(t *testing.T) {
	// arXiv's ToU forbids rapid repeat requests (1 req / 3s, single connection) and
	// its edge 429 carries no Retry-After. retryOptions() disables retries
	// (MaxAttempts=1), so a single Search issues exactly ONE upstream request even
	// on a transient 429 — never an immediate second hit that would breach the ToU.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests) // transient — DefaultRetryOptions WOULD retry this
	}))
	defer srv.Close()

	if _, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x"}); err == nil {
		t.Fatal("expected an error from HTTP 429")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected exactly 1 upstream request (no retry per arXiv politeness), got %d", got)
	}
}

func TestArxivContextCancellation(t *testing.T) {
	srv := atomServer(t, emptyAtom)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	_, err := testProvider(srv.URL).Search(ctx, core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected error for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got: %v", err)
	}
}

func TestArxivExtractNotSupported(t *testing.T) {
	if _, err := New().Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"}); err == nil {
		t.Fatal("expected extract unsupported error")
	}
	if core.HasCapability(New().Capabilities(), core.CapabilityExtract) {
		t.Fatal("arxiv must NOT advertise extract capability")
	}
	if !core.HasCapability(New().Capabilities(), core.CapabilitySearch) {
		t.Fatal("arxiv must advertise search capability")
	}
}

func TestArxivStatus(t *testing.T) {
	s := New().Status(context.Background())
	if !s.Available || s.Name != "arxiv" {
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

func TestArxivSearchLimit(t *testing.T) {
	const five = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry><id>http://arxiv.org/abs/1v1</id><title>A</title><summary>a</summary><published>t</published></entry>
  <entry><id>http://arxiv.org/abs/2v1</id><title>B</title><summary>b</summary><published>t</published></entry>
  <entry><id>http://arxiv.org/abs/3v1</id><title>C</title><summary>c</summary><published>t</published></entry>
  <entry><id>http://arxiv.org/abs/4v1</id><title>D</title><summary>d</summary><published>t</published></entry>
  <entry><id>http://arxiv.org/abs/5v1</id><title>E</title><summary>e</summary><published>t</published></entry>
</feed>`
	srv := atomServer(t, five)

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x", Limit: 2})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(resp.Results))
	}
}

func TestArxivSearchDecodesGzipResponse(t *testing.T) {
	// Guards the deliberate decision NOT to set Accept-Encoding manually: net/http
	// requests gzip itself and transparently decompresses. The server gzips ONLY
	// when the inbound request actually carried Accept-Encoding: gzip (which the
	// transport adds), so a passing parse proves we did not hand-set the header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			_, _ = gz.Write([]byte(fixtureAtom))
			_ = gz.Close()
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write([]byte(fixtureAtom))
	}))
	defer srv.Close()

	resp, err := testProvider(srv.URL).Search(context.Background(), core.SearchRequest{Query: "x", Limit: 5})
	if err != nil {
		t.Fatalf("search failed on gzip response: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results from gzip response, got %d", len(resp.Results))
	}
}

func TestArxivBreakerShortCircuitsAfterFailures(t *testing.T) {
	// arXiv is routed before the DDGS fallback, so it carries a breaker: after
	// repeated upstream failures it must short-circuit (fail fast without another
	// network call) so a slow/down export.arxiv.org doesn't stall the academic route
	// on every request before wikipedia/DDGS.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError) // 5xx -> breaker failure
	}))
	defer srv.Close()

	br := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 2, Cooldown: time.Minute})
	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}, breaker: br}

	for i := 0; i < 2; i++ {
		if _, err := p.Search(context.Background(), core.SearchRequest{Query: "x"}); err == nil {
			t.Fatalf("call %d: expected an error from HTTP 500", i)
		}
	}
	if !br.IsOpen() {
		t.Fatal("breaker should be open after reaching the failure threshold")
	}
	hitsBefore := atomic.LoadInt32(&hits)

	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "x"}); err == nil {
		t.Fatal("expected a short-circuit error while the breaker is open")
	}
	if got := atomic.LoadInt32(&hits); got != hitsBefore {
		t.Fatalf("breaker did not short-circuit: server hit again (%d -> %d)", hitsBefore, got)
	}

	st := p.Status(context.Background())
	if st.BreakerState != "open" || st.Available {
		t.Fatalf("status should show open breaker + Available=false: %#v", st)
	}
}

// TestArxivErrorEntryDoesNotTripBreaker pins the deliberate divergence from
// wikipedia: a 200 error <entry> is a per-query malformation, NOT an upstream
// outage, so it must not trip the breaker (one bad agent query must never
// short-circuit arXiv for every subsequent academic search).
func TestArxivErrorEntryDoesNotTripBreaker(t *testing.T) {
	srv := atomServer(t, errorEntryAtom)
	br := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 2, Cooldown: time.Minute})
	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}, breaker: br}

	for i := 0; i < 5; i++ {
		if _, err := p.Search(context.Background(), core.SearchRequest{Query: "%%%"}); err != nil {
			t.Fatalf("call %d: error-entry feed must be a non-error empty fall-through: %v", i, err)
		}
	}
	if br.IsOpen() {
		t.Fatal("breaker must NOT trip on per-query error entries (200 responses)")
	}
}

// TestArxivOversizeBodyDoesNotTripBreaker pins the over-cap-on-200 branch: a 200
// response whose body exceeds MaxSearchResponseBytes is a contract problem (or a
// hostile/oversized upstream), NOT an outage — ReadAllLimited returns a fatal,
// size-only error and the provider must record a breaker SUCCESS so the route
// falls through to wikipedia/DDGS rather than spuriously short-circuiting. With
// breaker threshold 1, a single wrongful failure would open it, so asserting the
// breaker stays closed AND the server is hit again proves the success default. It
// also guards the redaction contract: the error names only the byte cap, never the
// (16 MiB of) body.
func TestArxivOversizeBodyDoesNotTripBreaker(t *testing.T) {
	var hits int32
	oversize := bytes.Repeat([]byte("x"), int(providerhttp.MaxSearchResponseBytes)+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/atom+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(oversize)
	}))
	defer srv.Close()

	br := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Minute})
	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}, breaker: br}

	_, err := p.Search(context.Background(), core.SearchRequest{Query: "x"})
	if err == nil {
		t.Fatal("expected an over-cap read error on a >16 MiB 200 body")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("error should name the byte cap, got: %q", err.Error())
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Fatalf("error leaked response body: %q", err.Error())
	}
	if br.IsOpen() {
		t.Fatal("an over-cap 200 body must NOT trip the breaker (a contract problem, not an outage)")
	}
	// Breaker still closed → the next call must reach the server (no short-circuit).
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "x"}); err == nil {
		t.Fatal("expected the over-cap error again on the second call")
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("breaker wrongly short-circuited after an over-cap body: server hit %d times, want 2", got)
	}
}

// TestArxivSerializesConcurrentRequests pins arXiv's single-connection ToU: a
// Provider with the connection guard never has more than one in-flight HTTP request
// at a time, even under concurrent academic searches (the long-lived serve/MCP
// case). Without the guard, Service.Search only coalesces identical cache keys, so
// distinct concurrent queries would open parallel arXiv connections.
func TestArxivSerializesConcurrentRequests(t *testing.T) {
	var inFlight, maxInFlight int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxInFlight)
			if cur <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond) // hold the connection so any overlap is observable
		atomic.AddInt32(&inFlight, -1)
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(emptyAtom))
	}))
	defer srv.Close()

	// A Provider WITH the single-connection guard (its own sem for test isolation),
	// distinct queries so Service-level singleflight would not coalesce them.
	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}, sem: make(chan struct{}, 1)}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = p.Search(context.Background(), core.SearchRequest{Query: fmt.Sprintf("q%d", n)})
		}(i)
	}
	wg.Wait()
	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Fatalf("arxiv must issue at most ONE concurrent request (single-connection ToU), saw %d in flight", got)
	}
}

// TestArxivConnSlotRespectsContext pins that a caller waiting for the single
// connection slot still observes its own cancellation/deadline (it does not block
// forever and does not issue a request). We occupy the slot, then a call with an
// already-cancelled context must bail with context.Canceled and never hit the wire.
func TestArxivConnSlotRespectsContext(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(emptyAtom))
	}))
	defer srv.Close()

	p := Provider{httpClient: &http.Client{Transport: redirectTransport{baseURL: srv.URL}}, sem: make(chan struct{}, 1)}
	p.sem <- struct{}{} // occupy the single connection slot

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Search(ctx, core.SearchRequest{Query: "x"})
	if err == nil {
		t.Fatal("expected an error when the slot is occupied and the caller's context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled while waiting for the connection slot, got: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("a caller that gave up waiting for the slot must NOT issue a request, server hit %d times", got)
	}
}

// TestArxivNilBreakerStatus pins that an unbreakered Provider (the bench-map /
// New() case) reports empty breaker fields and stays available — the nil-safe path
// the rest of the suite relies on.
func TestArxivNilBreakerStatus(t *testing.T) {
	st := New().Status(context.Background())
	if !st.Available || st.BreakerState != "" || st.BreakerConsecFails != 0 {
		t.Fatalf("unbreakered provider status drifted: %#v", st)
	}
}
