package httpfetch

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

// errTransport is a RoundTripper that always fails with a fixed error — used to
// exercise the transport-error redaction path deterministically and offline.
type errTransport struct{ err error }

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

const htmlFixture = `<!DOCTYPE html><html><head><title>Fixture Title</title>
<script>var s = "SCRIPT_LEAK";</script><style>.c{}</style></head>
<body><h1>Header</h1><p>Hello O&#039;Brien &amp; &quot;world&quot;.</p>
<div>Second block.</div></body></html>`

// publicRedirectTransport rewrites any request host to a local httptest server,
// preserving path+query, so a redirect to a PUBLIC literal IP (which passes the
// SSRF preflight without DNS) can be exercised offline. Only the redirect-walk
// tests need it; direct-fetch tests pass the loopback srv.URL straight through.
type publicRedirectTransport struct{ baseURL string }

func (t publicRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := t.baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	clone, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	clone.Header = req.Header.Clone()
	return http.DefaultTransport.RoundTrip(clone)
}

// testProvider returns a Provider whose HTTP client uses the DEFAULT transport
// (no SSRF dial Control), so it can reach loopback httptest servers. Production
// New() installs validateDialedAddr, which rejects loopback/private dials — the
// dial-time SSRF guard exercised separately in the unit tests below.
func testProvider(opts ...Option) Provider {
	return New(append([]Option{WithHTTPClient(&http.Client{})}, opts...)...)
}

func TestNewHasDefaults(t *testing.T) {
	p := New()
	if p.httpClient == nil || p.httpClient.Timeout <= 0 {
		t.Fatalf("expected a default HTTP client with a timeout, got %#v", p.httpClient)
	}
	// The default client must install the SSRF dial guard (a non-default Transport).
	if p.httpClient.Transport == nil {
		t.Fatalf("default client must carry the SSRF-guarding transport, got nil")
	}
}

func TestValidateDialedAddrBlocksPrivateAndAllowsPublic(t *testing.T) {
	// The dial-time SSRF guard must reject loopback/private/metadata/CGNAT IPs (the
	// DNS-rebinding target) and accept a public IP — the half that closes the TOCTOU
	// gap net/http's own dial-time resolution would otherwise open.
	blocked := []string{
		"127.0.0.1:80", "10.0.0.5:443", "192.168.1.1:80", "169.254.169.254:80",
		"[::1]:80", "100.64.0.1:80", "0.0.0.0:80",
	}
	for _, a := range blocked {
		if err := validateDialedAddr("tcp", a, nil); err == nil {
			t.Errorf("validateDialedAddr(%q) = nil, want a blocked-address error", a)
		}
	}
	allowed := []string{"93.184.216.34:443", "8.8.8.8:53", "1.1.1.1:80"}
	for _, a := range allowed {
		if err := validateDialedAddr("tcp", a, nil); err != nil {
			t.Errorf("validateDialedAddr(%q) = %v, want nil (public IP)", a, err)
		}
	}
}

func TestExtractRedactsTransportErrorURL(t *testing.T) {
	// A net/http transport error is a *url.Error carrying the full request URL
	// (path + query, e.g. ?token=...). The returned provider error must NOT leak
	// it (the non-JSON CLI path prints the error verbatim).
	const queryMarker = "QUERY_VALUE_MUST_NOT_LEAK"
	failing := &http.Client{Transport: errTransport{
		err: &url.Error{Op: "Get", URL: "http://93.184.216.34/path?q=" + queryMarker, Err: errors.New("connect: connection refused")},
	}}
	_, err := New(WithHTTPClient(failing)).Extract(context.Background(), core.ExtractRequest{URL: "http://example.com/path?q=" + queryMarker})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), queryMarker) || strings.Contains(err.Error(), "/path") {
		t.Fatalf("transport error leaked the request URL/query: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should still surface the redaction-safe transport cause: %v", err)
	}
}

func TestExtractParsesHTMLWithoutNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlFixture))
	}))
	defer srv.Close()

	resp, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "httpfetch" {
		t.Errorf("provider = %q, want httpfetch", resp.Provider)
	}
	if resp.URL != srv.URL {
		t.Errorf("URL = %q, want the original request URL %q", resp.URL, srv.URL)
	}
	if resp.Metadata["title"] != "Fixture Title" {
		t.Errorf("metadata title = %q, want %q", resp.Metadata["title"], "Fixture Title")
	}
	if resp.Metadata["mode"] != "http-fetch" {
		t.Errorf("metadata mode = %q, want http-fetch", resp.Metadata["mode"])
	}
	if strings.Contains(resp.Content, "SCRIPT_LEAK") {
		t.Errorf("script body leaked into content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, `Hello O'Brien & "world".`) {
		t.Errorf("entities not decoded / content missing: %q", resp.Content)
	}
	for _, want := range []string{"Header", "Second block."} {
		if !strings.Contains(resp.Content, want) {
			t.Errorf("content missing %q: %q", want, resp.Content)
		}
	}
}

func TestExtractRequestShape(t *testing.T) {
	var gotMethod, gotUA, gotAccept, gotAcceptEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>ok</p>"))
	}))
	defer srv.Close()

	if _, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL}); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if !strings.HasPrefix(gotUA, "Nole/") || !strings.Contains(gotUA, "github.com/dorukardahan/nole") {
		t.Errorf("User-Agent = %q, want a descriptive Nole UA with a contact URL", gotUA)
	}
	if strings.Contains(gotUA, "Go-http-client") || strings.Contains(gotUA, "Mozilla/") {
		t.Errorf("User-Agent must not be the Go default or a browser-spoof: %q", gotUA)
	}
	if gotAccept == "" {
		t.Errorf("Accept header should be set")
	}
	// We deliberately do NOT set Accept-Encoding by hand: net/http then negotiates
	// gzip itself (adding "gzip") and transparently decompresses. The server seeing
	// exactly the transport's auto value "gzip" proves we left it for net/http; a
	// manual override would surface a different value AND break transparent decode.
	if gotAcceptEncoding != "gzip" {
		t.Errorf("Accept-Encoding = %q, want the net/http auto-negotiated \"gzip\" (we must not set it manually)", gotAcceptEncoding)
	}
}

func TestExtractRequiresURL(t *testing.T) {
	if _, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: "  "}); err == nil {
		t.Fatal("expected an error for a blank URL")
	}
}

func TestExtractBlocksRedirectToMetadataIP(t *testing.T) {
	// The classic redirect-based SSRF: a public (loopback test) URL 302-redirects
	// to the cloud metadata IP. The hop target MUST be re-validated and blocked
	// BEFORE it is fetched. The metadata IP is never contacted (literal IP, no DNS).
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data/")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	_, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("expected the redirect to the metadata IP to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should explain the URL was blocked: %v", err)
	}
	if hits != 1 {
		t.Errorf("server should be hit exactly once (the redirect target must not be fetched), got %d", hits)
	}
}

func TestExtractFollowsRedirectToPublicURL(t *testing.T) {
	// A redirect to a PUBLIC literal IP passes the preflight; the rewrite transport
	// routes it back to the test server's /final path so the walk completes offline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<p>final content</p>"))
			return
		}
		w.Header().Set("Location", "http://93.184.216.34/final") // public literal IP
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	p := New(WithHTTPClient(&http.Client{Transport: publicRedirectTransport{baseURL: srv.URL}}))
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: srv.URL + "/start"})
	if err != nil {
		t.Fatalf("extract failed on a valid public redirect: %v", err)
	}
	if !strings.Contains(resp.Content, "final content") {
		t.Errorf("content not extracted from the redirect target: %q", resp.Content)
	}
	if resp.URL != srv.URL+"/start" {
		t.Errorf("URL should be the ORIGINAL request URL, got %q", resp.URL)
	}
}

func TestExtractTooManyRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://93.184.216.34/loop")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	p := New(WithHTTPClient(&http.Client{Transport: publicRedirectTransport{baseURL: srv.URL}}))
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: srv.URL + "/loop"})
	if err == nil {
		t.Fatal("expected a too-many-redirects error")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("error should mention redirects: %v", err)
	}
}

func TestExtractBodyCapIsFatal(t *testing.T) {
	const marker = "BODY_CONTENT_MUST_NOT_LEAK"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.Repeat("x", 100) + marker))
	}))
	defer srv.Close()

	p := testProvider(WithMaxBodyBytes(64))
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("expected a fatal error when the body exceeds the cap")
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(safeerr.Message(err), marker) {
		t.Fatalf("body-cap error leaked body content: %v", err)
	}
	if !strings.Contains(err.Error(), "64") {
		t.Errorf("body-cap error should mention the byte limit: %v", err)
	}
}

func TestExtractHTTPErrorNoBodyLeak(t *testing.T) {
	const marker = "url-echo-must-not-leak-Bearer-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // 429
		_, _ = w.Write([]byte("rate stuff " + marker))
	}))
	defer srv.Close()

	_, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("expected an HTTP error for 429")
	}
	rendered := safeerr.Message(err)
	if strings.Contains(err.Error(), marker) || strings.Contains(rendered, marker) {
		t.Fatalf("error leaked response body: %q / %q", err.Error(), rendered)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention the status code: %v", err)
	}
}

func TestExtractUnsupportedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	defer srv.Close()

	_, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("expected an unsupported-content-type error for a binary response")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "content type") {
		t.Errorf("error should explain the content type was unsupported: %v", err)
	}
}

func TestExtractEmptyContentIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>only()</script><style>.a{}</style>"))
	}))
	defer srv.Close()

	resp, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("an empty (script-only) page must NOT be an error, got: %v", err)
	}
	if strings.TrimSpace(resp.Content) != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestExtractPreservesPlainText(t *testing.T) {
	// A server that EXPLICITLY declares text/plain must have its body returned
	// verbatim — the HTML tag-stripper would corrupt angle-bracketed content like
	// a C source file's #include.
	const body = "#include <stdio.h>\nint main() { return 0 < 1; }\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	resp, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if !strings.Contains(resp.Content, "<stdio.h>") {
		t.Errorf("text/plain angle-bracketed content was stripped: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "0 < 1") {
		t.Errorf("text/plain content corrupted: %q", resp.Content)
	}
}

func TestExtractDecodesGzipResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(htmlFixture))
		_ = gz.Close()
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	resp, err := testProvider().Extract(context.Background(), core.ExtractRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("extract failed on a gzip response: %v", err)
	}
	if !strings.Contains(resp.Content, "Second block.") {
		t.Errorf("gzip body not transparently decoded: %q", resp.Content)
	}
}

func TestExtractContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>ok</p>"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	_, err := testProvider().Extract(ctx, core.ExtractRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got: %v", err)
	}
}

func TestSearchUnsupportedAndCapabilities(t *testing.T) {
	p := New()
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "x"}); err == nil {
		t.Fatal("httpfetch must not support search")
	}
	if core.HasCapability(p.Capabilities(), core.CapabilitySearch) {
		t.Error("httpfetch must NOT advertise search capability")
	}
	if !core.HasCapability(p.Capabilities(), core.CapabilityExtract) {
		t.Error("httpfetch must advertise extract capability")
	}
	if !core.HasCapability(p.Capabilities(), core.CapabilityStatus) {
		t.Error("httpfetch must advertise status capability")
	}
}

func TestStatusAlwaysAvailableKeyless(t *testing.T) {
	s := New().Status(context.Background())
	if !s.Available || s.Name != "httpfetch" {
		t.Fatalf("unexpected status: %#v", s)
	}
	if !core.HasCapability(s.Capabilities, core.CapabilityExtract) {
		t.Errorf("status missing extract capability: %#v", s.Capabilities)
	}
	// Keyless/unbreakered: must not stamp cost or breaker fields itself.
	if s.CostClass != "" || s.BreakerState != "" {
		t.Errorf("provider must not set CostClass/BreakerState itself: %#v", s)
	}
}
