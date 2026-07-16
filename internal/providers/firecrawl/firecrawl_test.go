package firecrawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/version"
)

func boolPtr(v bool) *bool { return &v }

func TestFirecrawlClientIdentitySanitizesBuildVersion(t *testing.T) {
	original := version.Version
	version.Version = "v1.2.3\r\nX-Evil: yes"
	t.Cleanup(func() { version.Version = original })
	if got := firecrawlClientUserAgent(); got != "nole/v1.2.3-X-Evil-yes" {
		t.Fatalf("sanitized user agent = %q", got)
	}
	if got := firecrawlClientOrigin(); got != "nole@v1.2.3-X-Evil-yes" {
		t.Fatalf("sanitized origin = %q", got)
	}
}

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
			Success: boolPtr(true),
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

func TestFirecrawlSearchSuccessFalseIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"data": map[string]any{
				"web": []any{},
				"raw": "do not leak provider body",
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil {
		t.Fatal("expected error for success:false response")
	}
	if got, want := err.Error(), "firecrawl: search failed (provider returned success=false)"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "do not leak") {
		t.Fatalf("error must not leak raw provider body: %v", err)
	}
}

func TestFirecrawlSearchOmittedSuccessIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"web": []map[string]any{{
					"title":       "Nólë",
					"url":         "https://example.com/nole",
					"description": "router",
				}},
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err != nil {
		t.Fatalf("omitted success field should not error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %#v, want one result", resp.Results)
	}
}

func TestFirecrawlSearchOptionsMapCountryAndFreshness(t *testing.T) {
	var body firecrawlSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: boolPtr(true),
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

func TestFirecrawlAcademicSearchOmittedSuccessIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"paperId":   "paper-1",
				"primaryId": "arxiv:2401.01234",
				"title":     "Graph RAG",
				"abstract":  "A grounded abstract.",
				"score":     0.5,
			}},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "graph rag", Task: core.TaskAcademic, Limit: 1})
	if err != nil {
		t.Fatalf("omitted academic success field should not error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %#v, want one result", resp.Results)
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

func TestFirecrawlResearchSuccessFalseIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"results": []any{},
			"raw":     "do not leak provider body",
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "graph rag", Task: core.TaskAcademic, Limit: 3})
	if err == nil {
		t.Fatal("expected error for academic success:false response")
	}
	if got, want := err.Error(), "firecrawl: research search failed (provider returned success=false)"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "do not leak") {
		t.Fatalf("error must not leak raw provider body: %v", err)
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
			Success: boolPtr(true),
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

func TestFirecrawlExtractSuccessFalseIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"data": map[string]any{
				"markdown": "do not leak provider body",
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error for scrape success:false response")
	}
	if got, want := err.Error(), "firecrawl: extract failed (provider returned success=false)"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "do not leak") {
		t.Fatalf("error must not leak raw provider body: %v", err)
	}
}

func TestFirecrawlExtractOmittedSuccessIsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"markdown": "# ok",
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("omitted scrape success field should not error: %v", err)
	}
	if resp.Content != "# ok" {
		t.Fatalf("content = %q, want # ok", resp.Content)
	}
}

func TestFirecrawlKeylessSearchOmitsAuthorization(t *testing.T) {
	var auth, userAgent, origin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		userAgent = r.Header.Get("User-Agent")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		origin, _ = body["origin"].(string)
		_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
			Success: boolPtr(true),
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
	if userAgent != "nole/dev" || origin != "nole@dev" {
		t.Fatalf("keyless client identity = ua:%q origin:%q", userAgent, origin)
	}
}

func TestFirecrawlKeylessExtractOmitsAuthorization(t *testing.T) {
	var auth, userAgent, origin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		userAgent = r.Header.Get("User-Agent")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		origin, _ = body["origin"].(string)
		_ = json.NewEncoder(w).Encode(firecrawlScrapeResponse{Success: boolPtr(true), Data: firecrawlScrapeData{Markdown: "# ok"}})
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
	if userAgent != "nole/dev" || origin != "nole@dev" {
		t.Fatalf("keyless client identity = ua:%q origin:%q", userAgent, origin)
	}
}

func TestFirecrawlKeylessAcademicIdentifiesNole(t *testing.T) {
	var auth, userAgent, origin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		userAgent = r.Header.Get("User-Agent")
		origin = r.URL.Query().Get("origin")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": []any{}})
	}))
	defer srv.Close()

	p := New(WithAPIKey(""), WithBaseURL(srv.URL))
	p.apiKey = ""
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "agents", Task: core.TaskAcademic}); err != nil {
		t.Fatalf("keyless academic search failed: %v", err)
	}
	if auth != "" || userAgent != "nole/dev" || origin != "nole@dev" {
		t.Fatalf("keyless client identity = auth:%q ua:%q origin:%q", auth, userAgent, origin)
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
