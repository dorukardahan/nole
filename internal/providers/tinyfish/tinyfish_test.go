package tinyfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

const testAPIKey = "unit-test-key"

func testProvider(srv *httptest.Server) Provider {
	p := New(WithAPIKey(testAPIKey))
	p.searchURL = srv.URL
	p.fetchURL = srv.URL
	p.searchClient = srv.Client()
	p.fetchClient = srv.Client()
	p.retryOptions = providerhttp.RetryOptions{
		MaxAttempts: 1,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Sleep:       func(context.Context, time.Duration) error { return nil },
	}
	return p
}

func TestProviderContractAndMissingKey(t *testing.T) {
	p := New(WithAPIKey(""))
	if p.Name() != "tinyfish" {
		t.Fatalf("name = %q, want tinyfish", p.Name())
	}
	for _, want := range []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus} {
		if !core.HasCapability(p.Capabilities(), want) {
			t.Fatalf("capabilities %v missing %s", p.Capabilities(), want)
		}
	}
	status := p.Status(context.Background())
	if status.Available || !strings.Contains(status.Reason, "TINYFISH_API_KEY") {
		t.Fatalf("missing-key status = %#v", status)
	}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil || !strings.Contains(err.Error(), "TINYFISH_API_KEY") {
		t.Fatalf("missing-key search error = %v", err)
	}
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page"}); err == nil || !strings.Contains(err.Error(), "TINYFISH_API_KEY") {
		t.Fatalf("missing-key extract error = %v", err)
	}
}

func TestSearchUsesGETHeaderAuthAndCurrentParameters(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != testAPIKey {
			t.Fatalf("X-API-Key = %q", got)
		}
		q := r.URL.Query()
		if q.Get("query") != "nole router" || q.Get("page") != "0" {
			t.Fatalf("query params = %v", q)
		}
		if q.Get("location") != "US" || q.Get("language") != "en" || q.Get("recency_minutes") != "1440" {
			t.Fatalf("location/language/recency = %q/%q/%q", q.Get("location"), q.Get("language"), q.Get("recency_minutes"))
		}
		if q.Get("domain_type") != "news" {
			t.Fatalf("domain_type = %q, want news", q.Get("domain_type"))
		}
		for _, unsupported := range []string{"fetch", "purpose", "include_domains", "exclude_domains", "thumbnail", "ui_lang", "safesearch", "max_recency"} {
			if q.Has(unsupported) {
				t.Fatalf("unsupported parameter %q was forwarded: %v", unsupported, q)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "nole router",
			"page":  0,
			"results": []map[string]any{
				{"position": 1, "title": "Nólë", "url": "https://example.com/nole", "snippet": "local router", "date": "2026-07-30"},
				{"position": 2, "title": "Second", "url": "https://example.com/2", "snippet": "second", "date": "not-a-date"},
				{"position": 3, "title": "Third", "url": "https://example.com/3", "snippet": "third"},
			},
		})
	}))
	defer srv.Close()

	p := testProvider(srv)
	resp, err := p.Search(context.Background(), core.SearchRequest{
		Query: "nole router", Task: core.TaskNews, Limit: 2,
		Options: core.SearchOptions{Country: "us", SearchLang: "EN", UILang: "en-US", SafeSearch: "strict", Freshness: "pd"},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(gotURL, testAPIKey) {
		t.Fatalf("request URL leaked API key: %s", gotURL)
	}
	if resp.Provider != "tinyfish" || resp.Query != "nole router" || resp.Task != core.TaskNews || len(resp.Results) != 2 {
		t.Fatalf("response = %#v", resp)
	}
	first := resp.Results[0]
	if first.Title != "Nólë" || first.URL != "https://example.com/nole" || first.Snippet != "local router" || first.Provider != "tinyfish" || first.Score != nil || first.PublishedAt != "2026-07-30" {
		t.Fatalf("first result = %#v", first)
	}
	if resp.Results[1].PublishedAt != "" {
		t.Fatalf("malformed date should be dropped, got %q", resp.Results[1].PublishedAt)
	}
}

func TestSearchTaskAndFreshnessMapping(t *testing.T) {
	cases := []struct {
		name       string
		task       core.TaskType
		freshness  string
		wantDomain string
		wantMinute string
	}{
		{"general-default", core.TaskGeneral, "pw", "", "10080"},
		{"academic-no-recency", core.TaskAcademic, "pm", "research_paper", ""},
		{"docs-web", core.TaskDocs, "py", "web", "525600"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotDomain, gotMinute string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotDomain = r.URL.Query().Get("domain_type")
				gotMinute = r.URL.Query().Get("recency_minutes")
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			}))
			defer srv.Close()
			p := testProvider(srv)
			if _, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Task: tc.task, Limit: 1, Options: core.SearchOptions{Freshness: tc.freshness}}); err != nil {
				t.Fatal(err)
			}
			if gotDomain != tc.wantDomain || gotMinute != tc.wantMinute {
				t.Fatalf("domain/minutes = %q/%q, want %q/%q", gotDomain, gotMinute, tc.wantDomain, tc.wantMinute)
			}
		})
	}
}

func TestSearchRejectsBlankBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	p := testProvider(srv)
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "  "}); err == nil {
		t.Fatal("expected blank query error")
	}
	if calls.Load() != 0 {
		t.Fatalf("blank query reached provider %d times", calls.Load())
	}
}

func TestSearchRetries429AndSanitizesHTTPAndDecodeErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "sensitive provider body", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"title": "ok", "url": "https://example.com", "snippet": "ok"}}})
	}))
	defer srv.Close()
	p := testProvider(srv)
	p.retryOptions.MaxAttempts = 2
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Limit: 1})
	if err != nil || len(resp.Results) != 1 || calls.Load() != 2 {
		t.Fatalf("retry response=%#v err=%v calls=%d", resp, err, calls.Load())
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "key=%s url=https://private.example body-secret", testAPIKey)
	}))
	defer bad.Close()
	p = testProvider(bad)
	_, err = p.Search(context.Background(), core.SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	for _, secret := range []string{testAPIKey, "private.example", "body-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("HTTP error leaked %q: %v", secret, err)
		}
	}

	malformed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"results":[`)) }))
	defer malformed.Close()
	p = testProvider(malformed)
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "q"}); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestSearchRetries500(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"title": "ok", "url": "https://example.com", "snippet": "ok"}}})
	}))
	defer srv.Close()
	p := testProvider(srv)
	p.retryOptions.MaxAttempts = 2
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "q", Limit: 1})
	if err != nil || len(resp.Results) != 1 || calls.Load() != 2 {
		t.Fatalf("retry response=%#v err=%v calls=%d", resp, err, calls.Load())
	}
}

func TestSearchResponseBodyBoundAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"snippet":"` + strings.Repeat("x", 200) + `"}]}`))
	}))
	defer srv.Close()
	p := testProvider(srv)
	p.searchBodyLimit = 64
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "q"}); err == nil || !strings.Contains(err.Error(), "too_large") {
		t.Fatalf("expected bounded-body error, got %v", err)
	}

	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer blocked.Close()
	p = testProvider(blocked)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, searchErr := p.Search(ctx, core.SearchRequest{Query: "q"})
		errCh <- searchErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("search request did not reach test server")
	}
	cancel()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled search did not return")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	}
}

func TestExtractBlocksUnsafeTargetsBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	p := testProvider(srv)
	for _, target := range []string{
		"http://127.0.0.1/private",
		"http://10.0.0.1/private",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"https://user:password@1.1.1.1/private",
	} {
		if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: target}); err == nil {
			t.Errorf("expected target blocked: %s", target)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe targets reached TinyFish %d times", calls.Load())
	}
}

func TestExtractPostsOneURLMapsContentAndSelectedMetadata(t *testing.T) {
	const target = "https://1.1.1.1/public"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != testAPIKey {
			t.Fatalf("X-API-Key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		urls, _ := body["urls"].([]any)
		if len(urls) != 1 || urls[0] != target || body["format"] != "markdown" {
			t.Fatalf("body = %#v", body)
		}
		for _, omitted := range []string{"purpose", "links", "image_links", "ttl", "timeout", "include_selectors", "exclude_selectors"} {
			if _, ok := body[omitted]; ok {
				t.Fatalf("unexpected fetch option %q in %#v", omitted, body)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"url": target, "final_url": "http://127.0.0.1/private", "text": "# rendered", "format": "markdown",
				"title": "Title", "description": "Desc", "language": "en", "author": "Author", "published_date": "2026-07-30",
				"arbitrary": "must not pass through",
			}},
			"errors": []any{},
		})
	}))
	defer srv.Close()
	p := testProvider(srv)
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: target})
	if err != nil {
		t.Fatal(err)
	}
	if resp.URL != target || resp.Provider != "tinyfish" || resp.Content != "# rendered" {
		t.Fatalf("response = %#v", resp)
	}
	for key, want := range map[string]string{"title": "Title", "description": "Desc", "language": "en", "author": "Author", "published_date": "2026-07-30"} {
		if resp.Metadata[key] != want {
			t.Fatalf("metadata[%s] = %q, want %q", key, resp.Metadata[key], want)
		}
	}
	if _, ok := resp.Metadata["final_url"]; ok {
		t.Fatalf("final_url leaked into metadata: %#v", resp.Metadata)
	}
	if _, ok := resp.Metadata["arbitrary"]; ok {
		t.Fatalf("arbitrary metadata leaked: %#v", resp.Metadata)
	}
}

func TestExtractSupportsHTMLAndJSONText(t *testing.T) {
	cases := []struct {
		format  string
		payload string
		want    string
	}{
		{"html", `"<main>ok</main>"`, "<main>ok</main>"},
		{"json", `{"items":[1,2]}`, `{"items":[1,2]}`},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"results":[{"url":"https://1.1.1.1/data","text":%s,"format":%q}],"errors":[]}`, tc.payload, tc.format)
			}))
			defer srv.Close()
			p := testProvider(srv)
			resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/data", Format: tc.format})
			if err != nil || resp.Content != tc.want {
				t.Fatalf("content=%q err=%v", resp.Content, err)
			}
		})
	}
}

func TestExtractRejectsUnsupportedFormatBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	p := testProvider(srv)
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page", Format: "xml"}); err == nil {
		t.Fatal("expected unsupported format error")
	}
	if calls.Load() != 0 {
		t.Fatalf("unsupported format reached provider %d times", calls.Load())
	}
}

func TestExtractPerURLErrorsAreAllowlistedAndEmptyContentFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		want string
	}{
		{"known", "target_http_error", "target_http_error"},
		{"unknown", "INTERNAL_SECRET_CODE", "provider_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "errors": []map[string]any{{"url": "https://private.example", "error": tc.code}}})
			}))
			defer srv.Close()
			p := testProvider(srv)
			_, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want class %q", err, tc.want)
			}
			for _, leaked := range []string{"private.example", "raw upstream secret", "INTERNAL_SECRET_CODE"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("error leaked %q: %v", leaked, err)
				}
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"url": "https://1.1.1.1/page", "text": nil}}, "errors": []any{}})
	}))
	defer srv.Close()
	p := testProvider(srv)
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page"}); err == nil || !strings.Contains(err.Error(), "empty_content") {
		t.Fatalf("empty content error = %v", err)
	}
}

func TestExtractRetries503BoundsBodyAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"url": "https://1.1.1.1/page", "text": "ok"}}, "errors": []any{}})
	}))
	defer srv.Close()
	p := testProvider(srv)
	p.retryOptions.MaxAttempts = 2
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page"})
	if err != nil || resp.Content != "ok" || calls.Load() != 2 {
		t.Fatalf("response=%#v err=%v calls=%d", resp, err, calls.Load())
	}

	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"text":"` + strings.Repeat("x", 200) + `"}]}`))
	}))
	defer large.Close()
	p = testProvider(large)
	p.fetchBodyLimit = 64
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://1.1.1.1/page"}); err == nil || !strings.Contains(err.Error(), "too_large") {
		t.Fatalf("bounded fetch error = %v", err)
	}

	started := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer blocked.Close()
	p = testProvider(blocked)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, extractErr := p.Extract(ctx, core.ExtractRequest{URL: "https://1.1.1.1/page"})
		errCh <- extractErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("fetch request did not reach test server")
	}
	cancel()
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled fetch did not return")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	}
}
