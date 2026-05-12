package jina

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/dorukardahan/searchmcp/internal/core"
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
		p.apiKey = os.Getenv("JINA_API_KEY")
	}
	return p
}

func (p Provider) Name() string { return "jina" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

// --- Jina Search API response types ---

type jinaSearchResponse struct {
	Code int                `json:"code"`
	Data []jinaSearchResult `json:"data"`
}

type jinaSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if p.apiKey == "" {
		return core.SearchResponse{}, fmt.Errorf("jina: JINA_API_KEY not set")
	}

	body := map[string]interface{}{
		"q":   req.Query,
		"num": req.Limit,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("jina: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://s.jina.ai/", bytes.NewReader(jsonBody))
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("jina: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("jina: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, fmt.Errorf("jina: search returned %d: %s", resp.StatusCode, string(respBody))
	}

	var jresp jinaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&jresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("jina: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0, len(jresp.Data))
	for _, r := range jresp.Data {
		snippet := r.Description
		if snippet == "" {
			snippet = r.Content
		}
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		results = append(results, core.SearchResult{
			Title:    r.Title,
			URL:      r.URL,
			Snippet:  snippet,
			Provider: "jina",
		})
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "jina",
		Results:  results,
	}, nil
}

// --- Jina Reader API response types ---

type jinaReaderResponse struct {
	Code int            `json:"code"`
	Data jinaReaderData `json:"data"`
}

type jinaReaderData struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	if p.apiKey == "" {
		return core.ExtractResponse{}, fmt.Errorf("jina: JINA_API_KEY not set")
	}

	body := map[string]interface{}{
		"url": req.URL,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("jina: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://r.jina.ai/", bytes.NewReader(jsonBody))
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("jina: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("jina: extract request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return core.ExtractResponse{}, fmt.Errorf("jina: extract returned %d: %s", resp.StatusCode, string(respBody))
	}

	var jresp jinaReaderResponse
	if err := json.NewDecoder(resp.Body).Decode(&jresp); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("jina: decode response: %w", err)
	}

	return core.ExtractResponse{
		URL:      req.URL,
		Provider: "jina",
		Content:  jresp.Data.Content,
		Metadata: map[string]string{
			"title": jresp.Data.Title,
		},
	}, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	if p.apiKey == "" {
		return core.ProviderStatus{
			Name:         p.Name(),
			Available:    false,
			Capabilities: p.Capabilities(),
			Reason:       "JINA_API_KEY not set",
		}
	}
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
	}
}
