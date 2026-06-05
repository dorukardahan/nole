package tavily

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
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(&p)
	}
	if p.apiKey == "" {
		p.apiKey = os.Getenv("TAVILY_API_KEY")
	}
	return p
}

func (p Provider) Name() string { return "tavily" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

// --- Tavily Search API response types ---

type tavilySearchRequest struct {
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results"`
	SearchDepth   string `json:"search_depth"`
	IncludeAnswer bool   `json:"include_answer"`
	// Topic/TimeRange are set only for recency tasks or explicit freshness;
	// omitempty keeps every other task's request byte-identical to before.
	Topic     string `json:"topic,omitempty"`
	TimeRange string `json:"time_range,omitempty"`
	Country   string `json:"country,omitempty"`
}

type tavilySearchResponse struct {
	Query   string         `json:"query"`
	Results []tavilyResult `json:"results"`
	Answer  string         `json:"answer,omitempty"`
}

type tavilyResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	// Score is a pointer so an absent score stays nil (never fabricated as 0.0),
	// preserving the nil-vs-real-0.0 distinction SearchResult.Score relies on.
	Score *float64 `json:"score,omitempty"`
	// PublishedDate is present on results under topic=news (RFC1123, e.g.
	// "Tue, 19 May 2026 18:59:59 GMT"); empty otherwise. Verified live 2026-06-01.
	PublishedDate string `json:"published_date,omitempty"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if p.apiKey == "" {
		return core.SearchResponse{}, fmt.Errorf("tavily: TAVILY_API_KEY not set")
	}

	// Use "advanced" depth for research task, "basic" otherwise
	depth := "basic"
	if req.Task == core.TaskResearch {
		depth = "advanced"
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	// Defense in depth: clamp max_results to Tavily's documented ceiling (20)
	// so an over-large caller limit degrades to the cap instead of risking a
	// provider-side 4xx. Mirrors the brave provider's [1,20] clamp.
	if limit > 20 {
		limit = 20
	}

	body := tavilySearchRequest{
		Query:         req.Query,
		MaxResults:    limit,
		SearchDepth:   depth,
		IncludeAnswer: false,
		Country:       req.Options.Country,
	}
	// Task-aware freshness (allowlist): recency tasks get a time window by
	// default; explicit SearchOptions.Freshness overrides the task default and may
	// be used on any task.
	if tr := core.FreshnessTimeRange(req.Options.Freshness); tr != "" {
		body.TimeRange = tr
		if req.Task == core.TaskNews || req.Task == core.TaskFactcheck {
			body.Topic = "news"
		}
	} else {
		switch req.Task {
		case core.TaskNews, core.TaskFactcheck:
			// Both recency tasks use topic=news so Tavily returns published_date (it
			// only does so under topic=news) — the recency sort needs those dates, and
			// this matches how Brave/Firecrawl treat news==factcheck. time_range keeps
			// it to the conservative month window.
			body.Topic = "news"
			body.TimeRange = "month"
		}
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(jsonBody))
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("tavily", "search", resp.StatusCode, respBody)
	}

	var tresp tavilySearchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &tresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0, len(tresp.Results))
	for _, r := range tresp.Results {
		snippet := core.TruncateRunes(r.Content, 300)
		// Pass Tavily's relevance score + publication date through verbatim for
		// the agent to judge. Score is *float64 on the wire, so an absent score
		// stays nil (never fabricated as 0.0); each decoded result owns its own
		// pointer, so there is no aliasing across results.
		results = append(results, core.SearchResult{
			Title:       r.Title,
			URL:         r.URL,
			Snippet:     snippet,
			Provider:    "tavily",
			Score:       r.Score,
			PublishedAt: r.PublishedDate,
		})
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "tavily",
		Results:  results,
	}, nil
}

// --- Tavily Extract API ---

type tavilyExtractRequest struct {
	URLs []string `json:"urls"`
}

type tavilyExtractResponse struct {
	Results []tavilyExtractResult `json:"results"`
}

type tavilyExtractResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	if p.apiKey == "" {
		return core.ExtractResponse{}, fmt.Errorf("tavily: TAVILY_API_KEY not set")
	}

	body := tavilyExtractRequest{
		URLs: []string{req.URL},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tavily: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/extract", bytes.NewReader(jsonBody))
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tavily: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tavily: extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxExtractResponseBytes)
		return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("tavily", "extract", resp.StatusCode, respBody)
	}

	var tresp tavilyExtractResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxExtractResponseBytes, &tresp); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tavily: decode response: %w", err)
	}

	content := ""
	if len(tresp.Results) > 0 {
		content = tresp.Results[0].RawContent
	}

	return core.ExtractResponse{
		URL:      req.URL,
		Provider: "tavily",
		Content:  content,
	}, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	if p.apiKey == "" {
		return core.ProviderStatus{
			Name:         p.Name(),
			Available:    false,
			Capabilities: p.Capabilities(),
			Reason:       "TAVILY_API_KEY not set",
		}
	}
	state, consecFails, openedAt := providerhttp.BreakerStatusFields(p.breaker)
	status := core.ProviderStatus{
		Name:               p.Name(),
		Available:          true,
		Capabilities:       p.Capabilities(),
		BreakerState:       state,
		BreakerConsecFails: consecFails,
		BreakerOpenedAt:    openedAt,
	}
	if p.breaker.IsOpen() {
		status.Available = false
		status.Reason = "circuit_open"
	}
	return status
}
