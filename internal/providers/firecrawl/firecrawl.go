package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
}

func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
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
	Query string `json:"query"`
	Limit int    `json:"limit"`
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
	if p.apiKey == "" {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: FIRECRAWL_API_KEY not set")
	}

	body := firecrawlSearchRequest{
		Query: req.Query,
		Limit: req.Limit,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/search", bytes.NewReader(jsonBody))
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("firecrawl", "search", resp.StatusCode, respBody)
	}

	var fcresp firecrawlSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&fcresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0, len(fcresp.Data.Web))
	for _, r := range fcresp.Data.Web {
		snippet := r.Description
		if snippet == "" {
			snippet = r.Markdown
		}
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
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
	if p.apiKey == "" {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: FIRECRAWL_API_KEY not set")
	}

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
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("firecrawl", "extract", resp.StatusCode, respBody)
	}

	var fcresp firecrawlScrapeResponse
	if err := json.NewDecoder(resp.Body).Decode(&fcresp); err != nil {
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
	if p.apiKey == "" {
		return core.ProviderStatus{
			Name:         p.Name(),
			Available:    false,
			Capabilities: p.Capabilities(),
			Reason:       "FIRECRAWL_API_KEY not set",
		}
	}
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
	}
}
