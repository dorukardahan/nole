package tavily

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
	httpClient *http.Client
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) { p.apiKey = key }
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
}

type tavilySearchResponse struct {
	Query   string         `json:"query"`
	Results []tavilyResult `json:"results"`
	Answer  string         `json:"answer,omitempty"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
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

	body := tavilySearchRequest{
		Query:         req.Query,
		MaxResults:    limit,
		SearchDepth:   depth,
		IncludeAnswer: false,
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

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("tavily", "search", resp.StatusCode, respBody)
	}

	var tresp tavilySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&tresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("tavily: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0, len(tresp.Results))
	for _, r := range tresp.Results {
		snippet := core.TruncateRunes(r.Content, 300)
		results = append(results, core.SearchResult{
			Title:    r.Title,
			URL:      r.URL,
			Snippet:  snippet,
			Provider: "tavily",
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

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tavily: extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("tavily", "extract", resp.StatusCode, respBody)
	}

	var tresp tavilyExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&tresp); err != nil {
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
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
	}
}
