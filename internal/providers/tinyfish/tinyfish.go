package tinyfish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/safenet"
)

const (
	defaultSearchURL = "https://api.search.tinyfish.ai"
	defaultFetchURL  = "https://api.fetch.tinyfish.ai"
)

type Provider struct {
	apiKey       string
	apiKeySet    bool
	searchURL    string
	fetchURL     string
	searchClient *http.Client
	fetchClient  *http.Client
	breaker      *providerhttp.Breaker
	retryOptions providerhttp.RetryOptions

	searchBodyLimit int64
	fetchBodyLimit  int64
}

type Option func(*Provider)

func WithAPIKey(key string) Option {
	return func(p *Provider) {
		p.apiKey = key
		p.apiKeySet = true
	}
}

func WithBreaker(b *providerhttp.Breaker) Option {
	return func(p *Provider) { p.breaker = b }
}

func New(opts ...Option) Provider {
	p := Provider{
		searchURL:       defaultSearchURL,
		fetchURL:        defaultFetchURL,
		searchClient:    &http.Client{Timeout: 30 * time.Second},
		fetchClient:     &http.Client{Timeout: 150 * time.Second},
		retryOptions:    providerhttp.DefaultRetryOptions(),
		searchBodyLimit: providerhttp.MaxSearchResponseBytes,
		fetchBodyLimit:  providerhttp.MaxExtractResponseBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&p)
		}
	}
	if !p.apiKeySet {
		p.apiKey = os.Getenv("TINYFISH_API_KEY")
	}
	return p
}

func (p Provider) Name() string { return "tinyfish" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

type searchResponse struct {
	Query   string         `json:"query"`
	Results []searchResult `json:"results"`
	Page    int            `json:"page"`
}

type searchResult struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Snippet  string `json:"snippet"`
	Date     string `json:"date"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: TINYFISH_API_KEY not set")
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: query is required")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	endpoint, err := url.Parse(p.searchURL)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: create search URL: %w", err)
	}
	params := endpoint.Query()
	params.Set("query", req.Query)
	params.Set("page", "0")
	if country := strings.TrimSpace(req.Options.Country); country != "" {
		params.Set("location", strings.ToUpper(country))
	}
	if language := strings.TrimSpace(req.Options.SearchLang); language != "" {
		params.Set("language", strings.ToLower(language))
	}
	domain := tinyFishDomainType(req.Task)
	if domain != "" {
		params.Set("domain_type", domain)
	}
	if domain != "research_paper" {
		if minutes := freshnessMinutes(req.Options.Freshness); minutes != "" {
			params.Set("recency_minutes", minutes)
		}
	}
	endpoint.RawQuery = params.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: create search request: %w", err)
	}
	httpReq.Header.Set("X-API-Key", p.apiKey)
	httpReq.Header.Set("Accept", "application/json")
	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.searchClient, httpReq, p.retryOptions, p.breaker)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: search request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := providerhttp.ReadAllLimited(resp.Body, p.searchBodyLimit)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("tinyfish", "search", resp.StatusCode, body)
	}

	var upstream searchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, p.searchBodyLimit, &upstream); err != nil {
		return core.SearchResponse{}, fmt.Errorf("tinyfish: decode search response: %w", err)
	}
	if len(upstream.Results) > limit {
		upstream.Results = upstream.Results[:limit]
	}
	results := make([]core.SearchResult, 0, len(upstream.Results))
	for _, result := range upstream.Results {
		result.URL = strings.TrimSpace(result.URL)
		if result.URL == "" {
			return core.SearchResponse{}, fmt.Errorf("tinyfish: decode search response: result URL is required")
		}
		results = append(results, core.SearchResult{
			Title:       result.Title,
			URL:         result.URL,
			Snippet:     result.Snippet,
			Provider:    p.Name(),
			PublishedAt: validatedDate(result.Date),
		})
	}
	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: p.Name(),
		Results:  results,
	}, nil
}

func tinyFishDomainType(task core.TaskType) string {
	switch task {
	case "", core.TaskGeneral:
		return ""
	case core.TaskNews:
		return "news"
	case core.TaskAcademic:
		return "research_paper"
	default:
		return "web"
	}
}

func freshnessMinutes(freshness string) string {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "pd":
		return "1440"
	case "pw":
		return "10080"
	case "pm":
		return "43200"
	case "py":
		return "525600"
	default:
		return ""
	}
}

func validatedDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if _, err := time.Parse(layout, raw); err == nil {
			return raw
		}
	}
	return ""
}

type fetchRequest struct {
	URLs   []string `json:"urls"`
	Format string   `json:"format"`
}

type fetchResponse struct {
	Results []fetchResult `json:"results"`
	Errors  []fetchError  `json:"errors"`
}

type fetchResult struct {
	URL           string          `json:"url"`
	FinalURL      string          `json:"final_url"`
	Text          json.RawMessage `json:"text"`
	Format        string          `json:"format"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Language      string          `json:"language"`
	Author        string          `json:"author"`
	PublishedDate string          `json:"published_date"`
}

type fetchError struct {
	URL       string `json:"url"`
	Code      string `json:"code"`
	ErrorCode string `json:"error_code"`
	Error     string `json:"error"`
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: TINYFISH_API_KEY not set")
	}
	req.URL = strings.TrimSpace(req.URL)
	parsed, err := url.Parse(req.URL)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: url validation: invalid URL")
	}
	if parsed.User != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: url validation: authenticated URLs are not allowed")
	}
	if err := safenet.ValidateURLContext(ctx, req.URL); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: url validation: %w", err)
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "markdown"
	}
	switch format {
	case "markdown", "html", "json":
	default:
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: unsupported extract format %q", format)
	}

	payload, err := json.Marshal(fetchRequest{URLs: []string{req.URL}, Format: format})
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: marshal fetch request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.fetchURL, bytes.NewReader(payload))
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: create fetch request: %w", err)
	}
	httpReq.Header.Set("X-API-Key", p.apiKey)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.fetchClient, httpReq, p.retryOptions, p.breaker)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: fetch request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := providerhttp.ReadAllLimited(resp.Body, p.fetchBodyLimit)
		return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("tinyfish", "fetch", resp.StatusCode, body)
	}

	var upstream fetchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, p.fetchBodyLimit, &upstream); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: decode fetch response: %w", err)
	}
	for _, result := range upstream.Results {
		if strings.TrimSpace(result.URL) != req.URL {
			continue
		}
		content, err := decodeFetchText(result.Text, format)
		if err != nil {
			return core.ExtractResponse{}, fmt.Errorf("tinyfish: decode fetch content: %w", err)
		}
		if strings.TrimSpace(content) == "" {
			return core.ExtractResponse{}, fmt.Errorf("tinyfish: fetch failed (empty_content)")
		}
		metadata := selectedFetchMetadata(result)
		return core.ExtractResponse{
			URL:      req.URL,
			Provider: p.Name(),
			Content:  content,
			Metadata: metadata,
		}, nil
	}
	if len(upstream.Errors) > 0 {
		return core.ExtractResponse{}, fmt.Errorf("tinyfish: fetch failed (%s)", allowlistedFetchErrorCode(upstream.Errors[0]))
	}
	return core.ExtractResponse{}, fmt.Errorf("tinyfish: fetch failed (empty_content)")
}

func decodeFetchText(raw json.RawMessage, format string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return "", err
		}
		return text, nil
	}
	if format != "json" {
		return "", fmt.Errorf("non-string content for %s format", format)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return "", err
	}
	return compact.String(), nil
}

func selectedFetchMetadata(result fetchResult) map[string]string {
	candidates := map[string]string{
		"title":          result.Title,
		"description":    result.Description,
		"language":       result.Language,
		"author":         result.Author,
		"published_date": result.PublishedDate,
	}
	metadata := make(map[string]string, len(candidates))
	for key, value := range candidates {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func allowlistedFetchErrorCode(upstream fetchError) string {
	// The current Fetch API places a lowercase structured code in `error`.
	// Code/ErrorCode remain decode-only compatibility fallbacks; every candidate
	// still passes through this fixed allowlist before reaching caller surfaces.
	code := strings.ToLower(strings.TrimSpace(upstream.Error))
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(upstream.Code))
	}
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(upstream.ErrorCode))
	}
	switch code {
	case "target_http_error", "page_not_found", "target_unreachable", "timeout", "bot_blocked", "empty_content", "invalid_url", "invalid_redirect_url", "proxy_error", "conditional_unsupported", "selector_not_matched", "selector_unsupported":
		return code
	default:
		return "provider_error"
	}
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	state, failures, openedAt := providerhttp.BreakerStatusFields(p.breaker)
	status := core.ProviderStatus{
		Name:               p.Name(),
		Available:          strings.TrimSpace(p.apiKey) != "",
		Capabilities:       p.Capabilities(),
		BreakerState:       state,
		BreakerConsecFails: failures,
		BreakerOpenedAt:    openedAt,
	}
	if !status.Available {
		status.Reason = "TINYFISH_API_KEY not set"
		return status
	}
	if p.breaker.IsOpen() {
		status.Available = false
		status.Reason = "circuit_open"
	}
	return status
}

var _ core.Provider = Provider{}
