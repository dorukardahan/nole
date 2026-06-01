package brave

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

type Provider struct {
	apiKey     string
	httpClient *http.Client
	breaker    *providerhttp.Breaker
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
}

// WithBreaker attaches a circuit breaker so persistent upstream failures
// short-circuit fast instead of burning the per-call timeout + retry budget. A
// nil breaker (the default) leaves behaviour unchanged.
func WithBreaker(b *providerhttp.Breaker) Option {
	return func(p *Provider) { p.breaker = b }
}

func New(opts ...Option) Provider {
	p := Provider{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
	for _, opt := range opts {
		opt(&p)
	}
	if p.apiKey == "" {
		p.apiKey = os.Getenv("BRAVE_API_KEY")
	}
	if p.apiKey == "" {
		p.apiKey = os.Getenv("BRAVE_SEARCH_API_KEY")
	}
	return p
}

func (p Provider) Name() string { return "brave" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}

// --- Brave Search API response types ---

type braveSearchResponse struct {
	Query struct {
		Original string `json:"original"`
	} `json:"query"`
	Web *braveWebResults `json:"web,omitempty"`
}

type braveWebResults struct {
	Results []braveWebResult `json:"results"`
}

type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// PageAge is a machine-parseable zoneless ISO timestamp (e.g.
	// "2026-06-01T02:34:19"); Age is a human-relative string (e.g. "6 hours
	// ago"). Prefer PageAge. Either may be absent. Verified live 2026-06-01.
	Age     string `json:"age,omitempty"`
	PageAge string `json:"page_age,omitempty"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if p.apiKey == "" {
		return core.SearchResponse{}, fmt.Errorf("brave: BRAVE_API_KEY not set")
	}

	// Brave documents a hard max of 20 for `count`; anything above yields a
	// guaranteed non-retryable HTTP 422. Clamp to [1,20] so an over-large caller
	// limit degrades to the cap instead of failing the whole request.
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(req.Query), clampRange(req.Limit, 1, 20))
	// Task-aware freshness (allowlist): recency tasks get a conservative last-month
	// window; every other task sends an unchanged URL.
	if f := braveFreshness(req.Task); f != "" {
		u += "&freshness=" + f
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: create request: %w", err)
	}
	httpReq.Header.Set("X-Subscription-Token", p.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("brave", "search", resp.StatusCode, respBody)
	}

	var bresp braveSearchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &bresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0)
	if bresp.Web != nil {
		for _, r := range bresp.Web.Results {
			// Pass the publication date through (page_age preferred — parseable;
			// age is human-relative). Brave exposes no relevance score, so Score
			// stays nil — never fabricated.
			published := r.PageAge
			if published == "" {
				published = r.Age
			}
			results = append(results, core.SearchResult{
				Title:       r.Title,
				URL:         r.URL,
				Snippet:     r.Description,
				Provider:    "brave",
				PublishedAt: published,
			})
		}
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "brave",
		Results:  results,
	}, nil
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, fmt.Errorf("brave: extract not supported; use tavily or firecrawl")
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	if p.apiKey == "" {
		return core.ProviderStatus{
			Name:         p.Name(),
			Available:    false,
			Capabilities: p.Capabilities(),
			Reason:       "BRAVE_API_KEY not set",
		}
	}
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
	}
}

// braveFreshness maps recency-oriented tasks to Brave's `freshness` window.
// "pm" = past month (a conservative window that avoids emptying sparse-recency
// queries). Returns "" for every other task so the request is unchanged.
func braveFreshness(task core.TaskType) string {
	switch task {
	case core.TaskNews, core.TaskFactcheck:
		return "pm"
	default:
		return ""
	}
}

// clampRange constrains v to [min, max] inclusive. Brave's `count` parameter is
// documented to accept 1..20; values outside that band produce a non-retryable
// HTTP 422, so the request builder clamps rather than passes them through.
func clampRange(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
