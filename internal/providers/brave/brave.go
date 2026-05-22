package brave

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
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
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if p.apiKey == "" {
		return core.SearchResponse{}, fmt.Errorf("brave: BRAVE_API_KEY not set")
	}

	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(req.Query), maxInt(req.Limit, 1))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: create request: %w", err)
	}
	httpReq.Header.Set("X-Subscription-Token", p.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("brave", "search", resp.StatusCode, respBody)
	}

	var bresp braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&bresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0)
	if bresp.Web != nil {
		for _, r := range bresp.Web.Results {
			results = append(results, core.SearchResult{
				Title:    r.Title,
				URL:      r.URL,
				Snippet:  r.Description,
				Provider: "brave",
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
