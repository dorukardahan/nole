package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

type Provider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	breaker    *providerhttp.Breaker
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
}

func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithBreaker attaches a circuit breaker so persistent upstream failures
// short-circuit fast instead of burning the per-call timeout + retry budget. A
// nil breaker (the default) leaves behaviour unchanged.
func WithBreaker(b *providerhttp.Breaker) Option {
	return func(p *Provider) { p.breaker = b }
}

func New(opts ...Option) Provider {
	p := Provider{
		baseURL:    "https://api.firecrawl.dev/v2",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(&p)
	}
	if p.apiKey == "" {
		p.apiKey = os.Getenv("FIRECRAWL_API_KEY")
	}
	return p
}

func (p Provider) Name() string { return "firecrawl" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

// --- Firecrawl Search API request/response types ---

type firecrawlSearchRequest struct {
	Query   string `json:"query"`
	Limit   int    `json:"limit"`
	Country string `json:"country,omitempty"`
	// TBS is set only for recency tasks or explicit freshness. qdr:m = past
	// month; omitempty keeps every other task's request byte-identical to before.
	TBS string `json:"tbs,omitempty"`
}

type firecrawlSearchResponse struct {
	Success bool                `json:"success"`
	Data    firecrawlSearchData `json:"data"`
}

type firecrawlSearchData struct {
	Web []firecrawlSearchWebResult `json:"web"`
}

type firecrawlSearchWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Markdown    string `json:"markdown"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	// Defense in depth: clamp the limit to a sane ceiling before sending it
	// upstream, mirroring brave/tavily. core.Service already clamps to
	// maxSearchLimit (20), but a caller constructing the provider directly
	// bypasses that single upstream guard.
	if limit > 20 {
		limit = 20
	}

	body := firecrawlSearchRequest{
		Query:   req.Query,
		Limit:   limit,
		Country: req.Options.Country,
	}
	// Task-aware freshness (allowlist): recency tasks get a conservative past-month
	// window by default; explicit SearchOptions.Freshness overrides the task
	// default and may be used on any task. firecrawl web-source results carry no
	// per-result date (only sources=news would, deferred), so this filters but adds
	// no PublishedAt to pass through yet.
	if tbs := core.FreshnessTBS(req.Options.Freshness); tbs != "" {
		body.TBS = tbs
	} else {
		switch req.Task {
		case core.TaskNews, core.TaskFactcheck:
			body.TBS = "qdr:m"
		}
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/search", bytes.NewReader(jsonBody))
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: create request: %w", err)
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("firecrawl", "search", resp.StatusCode, respBody)
	}

	var fcresp firecrawlSearchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &fcresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: decode response: %w", err)
	}

	// Firecrawl web-source results carry no relevance score or publication date,
	// so Score stays nil and PublishedAt empty (never fabricated).
	results := make([]core.SearchResult, 0, len(fcresp.Data.Web))
	for _, r := range fcresp.Data.Web {
		snippet := r.Description
		if snippet == "" {
			snippet = r.Markdown
		}
		snippet = core.TruncateRunes(snippet, 300)
		results = append(results, core.SearchResult{
			Title:    r.Title,
			URL:      r.URL,
			Snippet:  snippet,
			Provider: "firecrawl",
		})
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "firecrawl",
		Results:  results,
	}, nil
}

// --- Firecrawl Scrape API request/response types ---

type firecrawlScrapeRequest struct {
	URL string `json:"url"`
}

type firecrawlScrapeResponse struct {
	Success bool                `json:"success"`
	Data    firecrawlScrapeData `json:"data"`
}

type firecrawlScrapeData struct {
	Markdown string                 `json:"markdown"`
	Metadata map[string]interface{} `json:"metadata"`
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	body := firecrawlScrapeRequest{
		URL: req.URL,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/scrape", bytes.NewReader(jsonBody))
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: create request: %w", err)
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxExtractResponseBytes)
		return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("firecrawl", "extract", resp.StatusCode, respBody)
	}

	var fcresp firecrawlScrapeResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxExtractResponseBytes, &fcresp); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: decode response: %w", err)
	}

	metadata := make(map[string]string)
	for k, v := range fcresp.Data.Metadata {
		if s, ok := v.(string); ok {
			metadata[k] = s
		}
	}

	return core.ExtractResponse{
		URL:      req.URL,
		Provider: "firecrawl",
		Content:  fcresp.Data.Markdown,
		Metadata: metadata,
	}, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	state, consecFails, openedAt := providerhttp.BreakerStatusFields(p.breaker)
	status := core.ProviderStatus{
		Name:               p.Name(),
		Available:          true,
		Capabilities:       p.Capabilities(),
		BreakerState:       state,
		BreakerConsecFails: consecFails,
		BreakerOpenedAt:    openedAt,
	}
	if p.apiKey == "" {
		status.Reason = "keyless mode: limited/shared upstream; set FIRECRAWL_API_KEY for account-backed quota"
	}
	if p.breaker.IsOpen() {
		status.Available = false
		status.Reason = "circuit_open"
	}
	return status
}
