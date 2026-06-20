package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	Success *bool               `json:"success"`
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

type firecrawlResearchPapersResponse struct {
	Success *bool                          `json:"success"`
	Results []firecrawlResearchPaperResult `json:"results"`
}

type firecrawlResearchPaperResult struct {
	PaperID         string              `json:"paperId"`
	PrimaryID       string              `json:"primaryId"`
	IDs             map[string][]string `json:"ids"`
	Title           string              `json:"title"`
	Abstract        string              `json:"abstract"`
	URL             string              `json:"url"`
	Score           *float64            `json:"score"`
	PublishedAt     string              `json:"publishedAt"`
	PublishedDate   string              `json:"publishedDate"`
	PublicationDate string              `json:"publicationDate"`
}

// explicitSuccessFalse reports only an explicit provider-level failure. A nil
// pointer means the success field was omitted, which Firecrawl-compatible mocks
// and future response shapes may do on success; omission must not be treated as
// failure.
func explicitSuccessFalse(success *bool) bool {
	return success != nil && !*success
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
	if req.Task == core.TaskAcademic {
		return p.searchResearchPapers(ctx, req, limit)
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
	if explicitSuccessFalse(fcresp.Success) {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: search failed (provider returned success=false)")
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

func (p Provider) searchResearchPapers(ctx context.Context, req core.SearchRequest, limit int) (core.SearchResponse, error) {
	endpoint, err := url.Parse(strings.TrimRight(p.baseURL, "/") + "/search/research/papers")
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: create research request URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("query", req.Query)
	query.Set("k", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: create research request: %w", err)
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := providerhttp.DoWithRetryBreaker(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions(), p.breaker)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: research search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("firecrawl", "research search", resp.StatusCode, respBody)
	}

	var fcresp firecrawlResearchPapersResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &fcresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: decode research response: %w", err)
	}
	if explicitSuccessFalse(fcresp.Success) {
		return core.SearchResponse{}, fmt.Errorf("firecrawl: research search failed (provider returned success=false)")
	}

	results := make([]core.SearchResult, 0, len(fcresp.Results))
	for _, r := range fcresp.Results {
		results = append(results, core.SearchResult{
			Title:       r.Title,
			URL:         researchPaperURL(r),
			Snippet:     core.TruncateRunes(r.Abstract, 300),
			Provider:    "firecrawl",
			Score:       r.Score,
			PublishedAt: researchPaperPublishedAt(r),
		})
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "firecrawl",
		Results:  results,
	}, nil
}

func researchPaperPublishedAt(r firecrawlResearchPaperResult) string {
	for _, candidate := range []string{r.PublishedAt, r.PublishedDate, r.PublicationDate} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func researchPaperURL(r firecrawlResearchPaperResult) string {
	if strings.TrimSpace(r.URL) != "" {
		return strings.TrimSpace(r.URL)
	}
	if arxivURL := canonicalArxivURL(r.PrimaryID); arxivURL != "" {
		return arxivURL
	}
	for _, id := range r.IDs["arxiv"] {
		if arxivURL := canonicalArxivURL(id); arxivURL != "" {
			return arxivURL
		}
	}
	return ""
}

var arxivIDPattern = regexp.MustCompile(`^(?:\d{4}\.\d{4,5}(?:v\d+)?|[A-Za-z-]+(?:\.[A-Za-z-]+)?/\d{7}(?:v\d+)?)$`)

func canonicalArxivURL(value string) string {
	id := strings.TrimSpace(value)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(id), "https://arxiv.org/abs/") {
		return id
	}
	if strings.HasPrefix(strings.ToLower(id), "arxiv:") {
		id = strings.TrimSpace(id[len("arxiv:"):])
	}
	if !arxivIDPattern.MatchString(id) {
		return ""
	}
	return "https://arxiv.org/abs/" + id
}

// --- Firecrawl Scrape API request/response types ---

type firecrawlScrapeRequest struct {
	URL string `json:"url"`
}

type firecrawlScrapeResponse struct {
	Success *bool               `json:"success"`
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
	if explicitSuccessFalse(fcresp.Success) {
		return core.ExtractResponse{}, fmt.Errorf("firecrawl: extract failed (provider returned success=false)")
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
