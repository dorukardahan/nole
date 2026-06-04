// Package arxiv is a keyless Nólë provider backed by the arXiv Atom query API
// (export.arxiv.org/api/query). It needs no API key and no setup, and it
// reinforces the `academic` route with primary-source scholarly preprints,
// analogous to how the wikipedia provider reinforces factcheck/people/academic
// with encyclopedic coverage. It is NOT a general fallback — it is routed only on
// `academic`, before the DDGS last-resort backstop.
//
// Like every Nólë provider it is a dumb gateway: it never judges result quality,
// never fabricates a relevance score (arXiv exposes none), never reorders by
// quality, and never prints or logs anything. The agent's query is passed through
// verbatim; a query arXiv rejects comes back as an error <entry> that is skipped
// (an honest empty fall-through), never surfaced as a result or an error. Errors
// are redaction-safe (HTTP status metadata only; never the response body, which
// can echo the query or upstream detail).
package arxiv

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/version"
)

// Compile-time assertion that Provider satisfies the core.Provider contract,
// pinned locally (not only indirectly via registry.Register at the call sites).
var _ core.Provider = Provider{}

// apiBase is the keyless arXiv Atom query API. https (TLS) is used for the
// REQUEST: arXiv serves the API over TLS, and a plaintext fetch would let a
// network MITM tamper with the metadata + abstract URLs an agent then trusts.
// Note the response <id> values are http:// by arXiv convention (an identifier
// scheme, not a transport signal) — see absPageURL, which serves https links.
const apiBase = "https://export.arxiv.org/api/query"

// userAgent is a descriptive, identifying User-Agent. arXiv's API Terms of Use do
// not require a UA, but a descriptive one (naming the client + a contact URL) is
// good-citizen practice and lets arXiv reach us rather than IP-block silently. No
// browser-spoof, no personal email — the contact is the project repo URL. Mirrors
// the wikipedia provider.
var userAgent = "Nole/" + version.Version + " (+https://github.com/dorukardahan/nole)"

type Provider struct {
	httpClient *http.Client
	breaker    *providerhttp.Breaker
}

type Option func(*Provider)

// WithBreaker attaches a circuit breaker so persistent export.arxiv.org failures
// short-circuit fast instead of stalling the `academic` route ahead of the DDGS
// fallback on every request in a long-lived serve/MCP process. arXiv is routed
// BEFORE the last-resort DDGS fallback (like wikipedia), so it IS breakered;
// DDGS/Scrapling (the fallbacks themselves) stay unbreakered. A nil breaker (the
// default, e.g. the bench map) leaves behaviour unchanged.
//
// arXiv's ToU asks for a single connection at a time (no more than one request
// every three seconds). Nólë issues exactly one request per logical search and
// never internally fans out concurrent arXiv requests. Concurrent academic
// searches in a long-lived process CAN still draw an edge 429 ("Rate exceeded.")
// that trips this breaker — that is intended, honest backpressure: the route then
// degrades to wikipedia/DDGS rather than hammering arXiv. Retries are disabled for
// the same politeness reason (see retryOptions).
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

func (p Provider) Name() string { return "arxiv" }

func (p Provider) Capabilities() []core.Capability {
	// Search + Status only — never Extract. arXiv is a metadata search source, not
	// a page extractor; this keeps it off the TaskExtract route and out of the
	// extract-tool gating.
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}

// retryOptions disables retries for arXiv (MaxAttempts=1). arXiv's API Terms of
// Use mandate "no more than one request every three seconds, and a single
// connection at a time," and its edge rate-limiter returns a 429 ("Rate
// exceeded.") with NO Retry-After header — so the shared DefaultRetryOptions
// (which would fire a second request ~200ms later) would breach the ToU and risk
// an IP block. With no retry, a transient failure is recorded once by the breaker
// and the route falls through to wikipedia/DDGS; repeated failures trip the
// breaker. This is the polite posture for a provider whose own ToU forbids rapid
// repeat requests, and it keeps the breaker's threshold counting one logical call
// per request.
func retryOptions() providerhttp.RetryOptions {
	opts := providerhttp.DefaultRetryOptions()
	opts.MaxAttempts = 1
	return opts
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	q := url.Values{}
	// "all:" is arXiv's broadest field prefix (title+abstract+authors+...). Nólë
	// passes the agent's query through verbatim and does NOT parse or rewrite arXiv
	// field operators — a query arXiv rejects comes back as an error <entry>, which
	// resultsFromFeed skips (an empty fall-through), never an error. url.Values
	// percent-escapes the whole value, so the query cannot inject extra params or
	// override the host/path.
	q.Set("search_query", "all:"+req.Query)
	q.Set("start", "0")
	q.Set("max_results", strconv.Itoa(clampLimit(req.Limit)))
	reqURL := apiBase + "?" + q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("arxiv: create request: %w", err)
	}
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("Accept", "application/atom+xml")
	// NOTE: we do NOT set Accept-Encoding. net/http transparently requests gzip and
	// decompresses ONLY when it adds the header itself; setting it manually would
	// hand us a raw gzip body the XML parser cannot read.

	// Manage the breaker manually (like wikipedia): arXiv reports a query error as
	// an HTTP 200 + error <entry> (not a status), so classifying the FULL outcome
	// here lets us trip on real outages (transport error, 5xx, edge 429) while NOT
	// tripping on a per-query error entry — one bad agent query must not short-
	// circuit the whole academic route for every other caller. Allow/Record are
	// nil-safe, so an unbreakered Provider behaves like a plain request.
	allowed, gen := p.breaker.Allow()
	if !allowed {
		return core.SearchResponse{}, fmt.Errorf("arxiv: search short-circuited; circuit breaker open after repeated export.arxiv.org failures")
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

	resp, err := providerhttp.DoWithRetry(ctx, p.httpClient, httpReq, retryOptions())
	if err != nil {
		// A live caller context means a transport/dial error or client-side timeout
		// (a slow/hung upstream) — a provider failure. A cancelled caller context is
		// not.
		if ctx.Err() != nil {
			callerCancelled = true
		} else {
			breakerFailure = true
		}
		return core.SearchResponse{}, fmt.Errorf("arxiv: search request failed: %w", err)
	}
	defer resp.Body.Close()

	// Only a 200 carries an Atom feed. Every non-200 — including the edge 429
	// "Rate exceeded." text/html body — goes through NewHTTPStatusError, which
	// records ONLY status + byte size, never the body. A 5xx/429/transient status
	// trips the breaker; a 4xx (e.g. max_results>30000 -> HTTP 400) does not (a bad
	// request is not an outage).
	if resp.StatusCode != http.StatusOK {
		breakerFailure = providerhttp.ShouldTrip(resp.StatusCode, nil, ctx)
		body, _ := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
		return core.SearchResponse{}, providerhttp.NewHTTPStatusError("arxiv", "search", resp.StatusCode, body)
	}

	// Bound the body before parsing. ReadAllLimited caps the (transparently
	// gunzipped) read at MaxSearchResponseBytes and treats an over-cap body as a
	// FATAL error — so a hostile/compromised upstream cannot OOM the process with an
	// unbounded XML body or a gzip bomb. We deliberately do NOT use
	// xml.NewDecoder(resp.Body) (an unbounded stream read). A 200 was received, so
	// an over-cap/decode problem is a contract mismatch, not an outage: leave the
	// breaker outcome a (default) success.
	body, err := providerhttp.ReadAllLimited(resp.Body, providerhttp.MaxSearchResponseBytes)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("arxiv: read response: %w", err)
	}

	feed, err := parseAtom(body)
	if err != nil {
		return core.SearchResponse{}, fmt.Errorf("arxiv: decode response: %w", err)
	}

	return core.SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: "arxiv",
		Results:  resultsFromFeed(feed, req.Limit), // empty (non-nil) slice on no hits — a valid response, never an error
	}, nil
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, fmt.Errorf("arxiv: extract not supported; use tavily, firecrawl, scrapling, or httpfetch")
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	// Keyless: statically available (no key check, no live ping — a ping would add
	// latency to every status call and burn arXiv's request budget; the route walk
	// discovers real failures at call time). When a breaker is attached, surface its
	// lifecycle for observability and fold "currently short-circuiting" into
	// Available so /health and the route walk treat a tripped provider as not-ready
	// (a breaker past its cooldown reports IsOpen()==false and stays Available;
	// BreakerState still shows the raw lifecycle). Breaker helpers are nil-safe, so
	// an unbreakered Provider (e.g. the bench map) reports empty breaker fields and
	// Available=true.
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
		status.Reason = "circuit breaker open (recent export.arxiv.org failures)"
	}
	return status
}

// clampLimit bounds the requested result count. 0/negative means "unspecified" ->
// a sensible default of 10; we cap at 50. arXiv permits up to 2000 results per
// request slice (30000 total via start+max_results, and recommends refining
// queries that return more than 1000), so 50 is well within its limits and ample
// for a routing-reinforcement provider tried before the DDGS fallback.
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
