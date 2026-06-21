package brave

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	Web     *braveWebResults `json:"web,omitempty"`
	Results []braveWebResult `json:"results,omitempty"`
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

	// Brave Search endpoints reject out-of-range `count` values. Web Search caps
	// at 20, while News Search caps at 50. Clamp so an over-large caller limit
	// degrades to the endpoint cap instead of failing the whole request.
	endpoint := braveSearchEndpoint(req.Task)
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("count", fmt.Sprintf("%d", clampRange(req.Limit, 1, braveCountMax(req.Task))))
	if req.Options.Country != "" {
		params.Set("country", req.Options.Country)
	}
	if req.Options.SearchLang != "" {
		params.Set("search_lang", req.Options.SearchLang)
	}
	if req.Options.UILang != "" {
		params.Set("ui_lang", req.Options.UILang)
	}
	if req.Options.SafeSearch != "" {
		params.Set("safesearch", req.Options.SafeSearch)
	}
	// Task-aware freshness (allowlist): recency tasks get a conservative last-month
	// window by default; an explicit caller SearchOptions.Freshness overrides it.
	freshness := req.Options.Freshness
	if freshness == "" {
		freshness = braveFreshness(req.Task)
	}
	if freshness != "" {
		params.Set("freshness", freshness)
	}
	u := fmt.Sprintf("https://api.search.brave.com%s?%s", endpoint, params.Encode())

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
	remoteUsage := braveUsageFromRateLimitHeaders(resp.Header)

	if resp.StatusCode != http.StatusOK {
		respBody, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{Provider: "brave", RemoteUsage: remoteUsage}, providerhttp.NewHTTPStatusError("brave", "search", resp.StatusCode, respBody)
	}

	var bresp braveSearchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &bresp); err != nil {
		return core.SearchResponse{}, fmt.Errorf("brave: decode response: %w", err)
	}

	results := make([]core.SearchResult, 0)
	braveResults := []braveWebResult(nil)
	if braveUsesNewsEndpoint(req.Task) {
		braveResults = bresp.Results
	} else if bresp.Web != nil {
		braveResults = bresp.Web.Results
	}
	for _, r := range braveResults {
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

	return core.SearchResponse{
		Query:       req.Query,
		Task:        req.Task,
		Provider:    "brave",
		Results:     results,
		RemoteUsage: remoteUsage,
	}, nil
}

func braveUsageFromRateLimitHeaders(h http.Header) *core.ProviderUsage {
	remaining := parseCSVInts(h.Get("X-RateLimit-Remaining"))
	if len(remaining) == 0 {
		return nil
	}
	idx := braveMonthlyRateLimitIndex(h.Get("X-RateLimit-Policy"), len(remaining))
	if idx < 0 || idx >= len(remaining) {
		return nil
	}
	rem := remaining[idx]
	usage := core.ProviderUsage{
		Provider:        "brave",
		Source:          "brave_rate_limit_headers",
		RemainingCalls:  intPtr(rem),
		NativeRemaining: intPtr(rem),
		NativeUnit:      "requests",
	}
	if limits := parseCSVInts(h.Get("X-RateLimit-Limit")); idx < len(limits) {
		usage.LimitCalls = intPtr(limits[idx])
		usage.NativeLimit = intPtr(limits[idx])
	}
	if resets := parseCSVInts(h.Get("X-RateLimit-Reset")); idx < len(resets) {
		usage.ResetSeconds = intPtr(resets[idx])
	}
	return &usage
}

func braveMonthlyRateLimitIndex(policy string, valueCount int) int {
	parts := strings.Split(policy, ",")
	bestIdx := -1
	bestWindow := -1
	for i, part := range parts {
		if i >= valueCount {
			break
		}
		part = strings.TrimSpace(part)
		for _, segment := range strings.Split(part, ";") {
			segment = strings.TrimSpace(segment)
			if !strings.HasPrefix(segment, "w=") {
				continue
			}
			window, err := strconv.Atoi(strings.TrimPrefix(segment, "w="))
			if err == nil && window > bestWindow {
				bestWindow = window
				bestIdx = i
			}
		}
	}
	if bestIdx >= 0 && bestWindow > 1 {
		return bestIdx
	}
	if valueCount > 1 {
		return valueCount - 1
	}
	return -1
}

func parseCSVInts(raw string) []int {
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if v < 0 {
			v = 0
		}
		out = append(out, v)
	}
	return out
}

func intPtr(v int) *int { return &v }

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
	state, consecFails, openedAt := providerhttp.BreakerStatusFields(p.breaker)
	status := core.ProviderStatus{
		Name:               p.Name(),
		Available:          true,
		Capabilities:       p.Capabilities(),
		BreakerState:       state,
		BreakerConsecFails: consecFails,
		BreakerOpenedAt:    openedAt,
	}
	// Fold the breaker's "currently short-circuiting" truth into Available so
	// the route walk and /health treat a tripped provider as not-ready without
	// needing a handle on the breaker. A breaker past its cooldown reports
	// IsOpen()==false (probe-eligible) and stays Available; BreakerState above
	// still shows the raw "open" lifecycle for observability.
	if p.breaker.IsOpen() {
		status.Available = false
		status.Reason = "circuit_open"
	}
	return status
}

// braveSearchEndpoint returns the Brave Search API endpoint for the task. News
// and factcheck keep the existing recency bias (`freshness=pm`) but use Brave's
// dedicated News Search endpoint instead of generic Web Search.
func braveSearchEndpoint(task core.TaskType) string {
	if braveUsesNewsEndpoint(task) {
		return "/res/v1/news/search"
	}
	return "/res/v1/web/search"
}

func braveUsesNewsEndpoint(task core.TaskType) bool {
	switch task {
	case core.TaskNews, core.TaskFactcheck:
		return true
	default:
		return false
	}
}

func braveCountMax(task core.TaskType) int {
	if braveUsesNewsEndpoint(task) {
		return 50
	}
	return 20
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

// clampRange constrains v to [min, max] inclusive. Brave Search endpoint
// `count` ranges differ (Web 1..20, News 1..50); callers pass the selected
// endpoint cap so the request builder clamps rather than sends a non-retryable
// out-of-range value.
func clampRange(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
