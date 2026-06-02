// Package wikipedia is a keyless Nólë provider backed by the MediaWiki Action
// API (list=search) on English Wikipedia. It needs no API key and no setup, and
// it reinforces the factcheck/people/academic routes with primary-source
// encyclopedic coverage. It is NOT a general fallback — DDGS remains the
// last-resort keyless backstop on every search route.
//
// Like every Nólë provider it is a dumb gateway: it never judges result quality
// (disambiguation/list pages are passed through for the agent to weigh), never
// fabricates a relevance score, and never prints or logs anything. Errors are
// redaction-safe (HTTP status metadata only; never the response body, which can
// echo the query).
package wikipedia

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/version"
)

// Compile-time assertion that Provider satisfies the core.Provider contract,
// pinned locally (not only indirectly via registry.Register at the call sites).
var _ core.Provider = Provider{}

// wikiAPIBase is the per-wiki Action API endpoint. We target the per-wiki
// api.php deliberately: it is keyless, stable for 20+ years, has no per-IP
// gateway throttle, and is not part of the api.wikimedia.org gateway that
// Wikimedia is deprecating in 2026.
const wikiAPIBase = "https://en.wikipedia.org/w/api.php"

// wikiUserAgent is a descriptive, identifying User-Agent. Wikimedia's User-Agent
// policy REQUIRES an informative UA with contact info or the client "may be
// blocked without notice" (HTTP 403). The contact is the project repo URL
// (issues are the contact channel) — a URL is an accepted contact form, and it
// keeps any personal email out of the committed source. We deliberately do NOT
// send a browser-spoof UA (unlike DDGS): Wikimedia wants an honest bot identity.
var wikiUserAgent = "Nole/" + version.Version + " (+https://github.com/dorukardahan/nole)"

type Provider struct {
	httpClient *http.Client
	breaker    *providerhttp.Breaker
}

type Option func(*Provider)

// WithBreaker attaches a circuit breaker so persistent en.wikipedia.org failures
// short-circuit fast instead of burning the per-call timeout + retry budget on
// every factcheck/people/academic request in a long-lived serve/MCP process.
// Wikipedia is a remote provider routed BEFORE the DDGS last-resort fallback, so
// (unlike DDGS/Scrapling, which are the fallback and stay unbreakered) it needs a
// breaker to keep a slow upstream from stalling the route ahead of DDGS. A nil
// breaker (the default, e.g. the bench map) leaves behaviour unchanged.
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
	return p
}

func (p Provider) Name() string { return "wikipedia" }

func (p Provider) Capabilities() []core.Capability {
	// Search + Status only — never Extract. This keeps Wikipedia out of the
	// extract-tool gating and the TaskExtract route.
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}

// reStripTags removes the <span class="searchmatch"> highlight markup (and any
// other tag) MediaWiki embeds in the snippet field.
var reStripTags = regexp.MustCompile(`<[^>]+>`)

// wikiSearchResponse is the formatversion=2 shape of an Action API list=search
// response. The legacy `error` object is still returned for API-level failures
// (e.g. maxlag) under the default error format.
type wikiSearchResponse struct {
	Error *struct {
		Code string `json:"code"`
		// Info can echo host/lag/query details — captured for branching but
		// NEVER surfaced in an error message.
		Info string `json:"info"`
	} `json:"error,omitempty"`
	Query struct {
		Search []struct {
			Title     string `json:"title"`
			PageID    int    `json:"pageid"`
			Snippet   string `json:"snippet"`   // HTML with searchmatch spans
			Timestamp string `json:"timestamp"` // ISO8601 last-edit time
		} `json:"search"`
	} `json:"query"`
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("list", "search")
	q.Set("srsearch", req.Query)
	q.Set("srnamespace", "0")            // main/article namespace only (no Talk/Template/Category)
	q.Set("srprop", "snippet|timestamp") // only the fields we surface
	q.Set("srlimit", strconv.Itoa(clampLimit(req.Limit)))
	q.Set("format", "json")
	q.Set("formatversion", "2") // flat, modern response shape
	q.Set("maxlag", "5")        // replica-lag backpressure (API etiquette)
	reqURL := wikiAPIBase + "?" + q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("wikipedia: create request: %w", err)
	}
	httpReq.Header.Set("User-Agent", wikiUserAgent)
	httpReq.Header.Set("Accept", "application/json")
	// NOTE: we do NOT set Accept-Encoding here. net/http transparently requests
	// gzip and decompresses ONLY when it adds the header itself; setting it
	// manually would hand us a raw gzip body that DecodeJSONLimited cannot parse.

	// Manage the breaker manually rather than via DoWithRetryBreaker: MediaWiki
	// reports maxlag/rate-limiting as an HTTP 200 with an error BODY, which
	// DoWithRetryBreaker — keyed off the HTTP status alone — would record as a
	// SUCCESS (even resetting a prior failure streak), defeating the breaker during
	// exactly the overload/rate-limit periods it exists to short-circuit. So we
	// Allow() up front, run DoWithRetry, classify the FULL outcome (status +
	// transport error + 200 error body), and record it once in the deferred call.
	// Allow/Record are nil-safe, so an unbreakered Provider (bench map, tests)
	// behaves exactly like a plain DoWithRetry.
	allowed, gen := p.breaker.Allow()
	if !allowed {
		return core.SearchResponse{}, fmt.Errorf("wikipedia: search short-circuited; circuit breaker open after repeated en.wikipedia.org failures")
	}
	breakerFailure := false  // default = success
	callerCancelled := false // caller's own cancellation is never the provider's fault
	defer func() {
		switch {
		case callerCancelled:
			p.breaker.RecordCancellation(gen)
		case breakerFailure:
			p.breaker.RecordFailure(gen)
		default:
			p.breaker.RecordSuccess(gen)
		}
	}()

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, providerhttp.DefaultRetryOptions())
	if err != nil {
		// A live caller context means a transport/dial error or client-side timeout
		// (a slow/hung upstream) — a provider failure. A cancelled caller context is
		// not.
		if ctx.Err() != nil {
			callerCancelled = true
		} else {
			breakerFailure = true
		}
		return core.SearchResponse{}, fmt.Errorf("wikipedia: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 5xx/transient is an upstream failure; a 4xx client error (e.g. a bad
		// query) is not an outage and must not trip the breaker.
		breakerFailure = providerhttp.ShouldTrip(resp.StatusCode, nil, ctx)
		body, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("wikipedia", "search", resp.StatusCode, body)
	}

	var wresp wikiSearchResponse
	if err := providerhttp.DecodeJSONLimited(resp.Body, providerhttp.MaxSearchResponseBytes, &wresp); err != nil {
		// A 200 was received; a decode failure is a contract mismatch, not an
		// outage — leave it a (default) success for breaker purposes.
		return core.SearchResponse{}, fmt.Errorf("wikipedia: decode response: %w", err)
	}

	// MediaWiki reports maxlag / rate limiting as an HTTP 200 body with an
	// {"error":{"code":...}} block (not a non-2xx status). This IS an upstream
	// failure for the breaker (so repeated overload trips it and the route
	// short-circuits to DDGS). Surface only the code, never error.info (which can
	// carry host/lag/query text).
	if wresp.Error != nil {
		breakerFailure = true
		switch wresp.Error.Code {
		case "maxlag", "ratelimited":
			return core.SearchResponse{}, fmt.Errorf("wikipedia: rate limited (api error code %q; details redacted)", wresp.Error.Code)
		default:
			return core.SearchResponse{}, fmt.Errorf("wikipedia: api error (code %q; details redacted)", wresp.Error.Code)
		}
	}

	results := make([]core.SearchResult, 0, len(wresp.Query.Search))
	for _, s := range wresp.Query.Search {
		results = append(results, core.SearchResult{
			Title:    s.Title,
			URL:      articleURL(s.Title),
			Snippet:  core.TruncateRunes(stripSearchMatch(s.Snippet), 300),
			Provider: "wikipedia",
			// Score stays nil: MediaWiki exposes no relevance score and Nólë
			// never fabricates one. PublishedAt = last-edit timestamp, passed
			// through verbatim for the agent to judge recency.
			PublishedAt: s.Timestamp,
		})
		if req.Limit > 0 && len(results) >= req.Limit {
			break
		}
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "wikipedia",
		Results:  results, // empty (non-nil) slice on no hits — a valid response, never an error
	}, nil
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, fmt.Errorf("wikipedia: extract not supported; use tavily or firecrawl")
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	// Keyless: statically available (no key check, no live ping — a ping would add
	// latency to every status call and burn Wikimedia budget; the route walk
	// discovers real failures at call time). When a breaker is attached, surface
	// its lifecycle for observability and fold "currently short-circuiting" into
	// Available so /health and the route walk treat a tripped provider as
	// not-ready (a breaker past its cooldown reports IsOpen()==false and stays
	// Available; BreakerState still shows the raw lifecycle). Breaker helpers are
	// nil-safe, so an unbreakered Provider (e.g. the bench map) reports empty
	// breaker fields and Available=true.
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
		status.Reason = "circuit breaker open (recent en.wikipedia.org failures)"
	}
	return status
}

// stripSearchMatch removes the searchmatch span markup and decodes the entities
// MediaWiki emits, then collapses whitespace. Tags are stripped BEFORE entities
// are decoded, so it is deliberately not idempotent and may re-emit tag-shaped
// substrings (e.g. "&lt;script&gt;" -> "<script>") — the fuzz test documents this.
//
// Decoding uses the stdlib html.UnescapeString rather than a hand-rolled entity
// table: MediaWiki emits the ZERO-PADDED numeric apostrophe &#039; (the most
// common entity in English prose, in every possessive/contraction), which a
// fixed &#39;-only table would leak raw to the agent. html.UnescapeString decodes
// &#039;, &#39;, &apos;, &amp;, &quot;, named and numeric entities uniformly, is
// UTF-8 safe (out-of-range/surrogate code points become U+FFFD), and never panics.
func stripSearchMatch(s string) string {
	s = reStripTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ") // collapse all runs of whitespace (incl. decoded &nbsp;)
	return strings.TrimSpace(s)
}

// articleURL builds a canonical article URL from a title: spaces become
// underscores, then the path segment is escaped. Slashes in titles (e.g.
// "AC/DC") stay as path separators so the link resolves.
func articleURL(title string) string {
	escaped := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	escaped = strings.ReplaceAll(escaped, "%2F", "/")
	return "https://en.wikipedia.org/wiki/" + escaped
}

// clampLimit bounds the requested result count to the srlimit range we allow.
// 0/negative means "unspecified" -> a sensible default of 10; we cap at 50
// (api.php permits up to 500, but 50 is plenty for a routing reinforcement).
func clampLimit(n int) int {
	switch {
	case n <= 0:
		return 10
	case n > 50:
		return 50
	default:
		return n
	}
}
