package ddgs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

type Provider struct {
	httpClient *http.Client
}

func New() Provider {
	return Provider{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p Provider) Name() string { return "ddgs" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}

var (
	reResultLink    = regexp.MustCompile(`class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	reResultSnippet = regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</a>`)
	reStripTags     = regexp.MustCompile(`<[^>]+>`)
	reHTMLEntity    = regexp.MustCompile(`&amp;`)
)

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	form := url.Values{}
	form.Set("q", req.Query)
	// DDG no-JS HTML endpoint expects b="" on the first page. Setting b="Web Search"
	// (the visible submit-button label) trips the anti-bot heuristic — SearXNG's
	// canonical implementation explicitly sends an empty string.
	form.Set("b", "")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("ddgs: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	// Browser-parity headers required by DDG's bot blocker (Q3/Q4 2025 tightening).
	// Missing Sec-Fetch-* + Referer triggers immediate 202 Ratelimit on most queries.
	httpReq.Header.Set("Referer", "https://html.duckduckgo.com/")
	httpReq.Header.Set("Sec-Fetch-Dest", "document")
	httpReq.Header.Set("Sec-Fetch-Mode", "navigate")
	httpReq.Header.Set("Sec-Fetch-Site", "same-origin")
	httpReq.Header.Set("Sec-Fetch-User", "?1")

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("ddgs: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		// DDG signals rate-limit / bot-block with HTTP 202 + body "202 Ratelimit"
		// rather than a 4xx. Surface this distinctly so callers and the bench
		// classifier can treat it as transient rather than a generic 4xx error.
		body, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, fmt.Errorf("ddgs: rate limited (status 202): %s", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("ddgs", "search", resp.StatusCode, body)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("ddgs: read response: %w", err)
	}
	html := string(bodyBytes)

	// Extract result links (skip ad links containing duckduckgo.com/y.js)
	linkMatches := reResultLink.FindAllStringSubmatch(html, -1)
	snippetMatches := reResultSnippet.FindAllStringSubmatch(html, -1)

	results := make([]core.SearchResult, 0)
	snippetIdx := 0

	for _, match := range linkMatches {
		href := match[1]
		title := cleanHTML(match[2])

		// Skip ad redirects
		if strings.Contains(href, "duckduckgo.com/y.js") || strings.Contains(href, "bing.com/aclick") {
			continue
		}

		snippet := ""
		if snippetIdx < len(snippetMatches) {
			snippet = cleanHTML(snippetMatches[snippetIdx][1])
			snippetIdx++
		}
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}

		results = append(results, core.SearchResult{
			Title:    title,
			URL:      href,
			Snippet:  snippet,
			Provider: "ddgs",
		})

		if req.Limit > 0 && len(results) >= req.Limit {
			break
		}
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "ddgs",
		Results:  results,
	}, nil
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, fmt.Errorf("ddgs: extract not supported; use tavily or firecrawl")
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
	}
}

func cleanHTML(s string) string {
	s = reStripTags.ReplaceAllString(s, "")
	s = reHTMLEntity.ReplaceAllString(s, "&")
	s = strings.TrimSpace(s)
	// Decode common HTML entities
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}
