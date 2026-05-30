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
		// DDG signals rate-limit / bot-block with HTTP 202 (often with a body
		// echoing pieces of the request). Wrapping providerhttp.NewHTTPStatusError
		// here would put the redaction in place, but safeerr.Message unwraps
		// to the inner *HTTPStatusError and renders only its Error() text —
		// which categorizes 202 as "unexpected" and never mentions "rate
		// limited." That would erase the classification signal in every
		// user-facing surface that uses safeerr.Message (the bench tracer
		// included). Build a sanitized single-shot error here instead: it
		// keeps the "rate limited" marker AND drops the raw body, recording
		// only its size so observers know something was redacted.
		body, _ := io.ReadAll(resp.Body)
		return core.SearchResponse{}, fmt.Errorf("ddgs: rate limited (HTTP 202; response body redacted, %d bytes)", len(body))
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

	// Pair each result link with the snippet that physically follows it in the
	// HTML, bounded by the next link's offset. The previous parser zipped two
	// independently-collected slices with a counter that only advanced on kept
	// links, so a skipped ad row — which can carry its own result__snippet —
	// shifted every subsequent organic snippet onto the wrong result. Matching
	// by byte offset keeps each snippet anchored to the link it belongs to.
	linkMatches := reResultLink.FindAllStringSubmatchIndex(html, -1)
	snippetMatches := reResultSnippet.FindAllStringSubmatchIndex(html, -1)

	results := make([]core.SearchResult, 0)

	for i, lm := range linkMatches {
		href := html[lm[2]:lm[3]]
		title := cleanHTML(html[lm[4]:lm[5]])

		// Skip ad redirects
		if strings.Contains(href, "duckduckgo.com/y.js") || strings.Contains(href, "bing.com/aclick") {
			continue
		}

		nextLinkStart := len(html)
		if i+1 < len(linkMatches) {
			nextLinkStart = linkMatches[i+1][0]
		}
		snippet := ""
		for _, sm := range snippetMatches {
			if sm[0] >= lm[0] && sm[0] < nextLinkStart {
				snippet = cleanHTML(html[sm[2]:sm[3]])
				break
			}
		}
		snippet = core.TruncateRunes(snippet, 300)

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
