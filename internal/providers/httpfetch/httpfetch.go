// Package httpfetch is a keyless, pure-Go Nólë EXTRACT provider. It GETs a public
// URL and strips the HTML to readable text using only the standard library (no
// API key, no Python, no headless browser, no third-party dependency). It is the
// LAST-RESORT keyless backstop on the extract route — the extract-side analogue
// of DDGS on the search route: it is tried only after a configured local
// Scrapling and the keyed remote extractors (Firecrawl/Tavily). It does NOT
// execute JavaScript, so it is weaker than those on SPA / JS-rendered pages —
// an honest, accepted limit that nonetheless makes extract / search_and_extract
// work out of the box with zero keys and zero setup.
//
// Like every Nólë provider it is a dumb gateway: it never judges result quality,
// never ranks or filters, and never prints or logs. Errors are redaction-safe
// (HTTP status + byte-size metadata only; never the response body, which can echo
// auth headers or private URLs). SSRF safety is enforced on every redirect hop
// via safenet.ValidateURLContext, mirroring the Scrapling redirect walk.
package httpfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/safenet"
	"github.com/dorukardahan/nole/internal/version"
)

// Compile-time assertion that Provider satisfies the core.Provider contract.
var _ core.Provider = Provider{}

// maxRedirects bounds how many redirect hops Extract follows. Each hop's target
// is re-validated against the SSRF preflight before it is fetched, so a public
// URL that 30x-redirects to a private / metadata host is rejected at the
// redirecting hop instead of being fetched locally.
const maxRedirects = 5

// userAgent is a descriptive, identifying User-Agent with a contact URL (the
// project repo). We deliberately do NOT browser-spoof — an honest bot identity,
// consistent with the wikipedia provider.
var userAgent = "Nole/" + version.Version + " (+https://github.com/dorukardahan/nole)"

// acceptHeader prefers HTML/text but accepts anything; a non-textual response is
// rejected by the Content-Type gate rather than tokenized as if it were HTML.
const acceptHeader = "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1"

type Provider struct {
	httpClient *http.Client
	timeout    time.Duration
	maxBytes   int64
}

type Option func(*Provider)

// WithHTTPClient injects the HTTP client (test seam). Extract always copies the
// client and forces a no-follow redirect policy onto the copy, so an injected or
// default client can never bypass the per-hop SSRF re-validation.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) {
		if c != nil {
			p.httpClient = c
		}
	}
}

// WithTimeout sets the total budget across the whole redirect walk.
func WithTimeout(d time.Duration) Option {
	return func(p *Provider) {
		if d > 0 {
			p.timeout = d
		}
	}
}

// WithMaxBodyBytes overrides the response body cap (default
// providerhttp.MaxExtractResponseBytes). Exposed primarily so tests can exercise
// the fatal over-cap path without streaming 64 MiB.
func WithMaxBodyBytes(n int64) Option {
	return func(p *Provider) {
		if n > 0 {
			p.maxBytes = n
		}
	}
}

func New(opts ...Option) Provider {
	p := Provider{
		// The default client installs an SSRF dial guard (validateDialedAddr via the
		// dialer Control hook) so the IP net/http ACTUALLY connects to is validated
		// immediately before dialing — closing the DNS-rebinding / split-horizon
		// TOCTOU window between the preflight check and the real dial (a malicious
		// resolver returning a public IP at preflight then a private/metadata IP at
		// dial). A test seam (WithHTTPClient) can replace this for loopback httptest.
		httpClient: &http.Client{Timeout: 20 * time.Second, Transport: safeTransport()},
		timeout:    30 * time.Second,
		maxBytes:   providerhttp.MaxExtractResponseBytes,
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

func (p Provider) Name() string { return "httpfetch" }

func (p Provider) Capabilities() []core.Capability {
	// Extract + Status only — never Search. This keeps httpfetch out of every
	// search route and the search-tool gating; it is an extract backstop.
	return []core.Capability{core.CapabilityExtract, core.CapabilityStatus}
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{}, errors.New("httpfetch: search is not supported; use extract with a public URL")
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	target := strings.TrimSpace(req.URL)
	if target == "" {
		return core.ExtractResponse{}, errors.New("httpfetch: url is required")
	}
	// One timeout budget across the whole redirect walk so a chain of hops cannot
	// extend total runtime beyond the provider timeout.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	client := p.noFollowClient()
	current := target
	for hop := 0; ; hop++ {
		// A caller cancellation mid-walk reports as cancellation, never as a
		// spurious resolve/block error from the preflight below.
		if err := ctx.Err(); err != nil {
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: request cancelled: %w", err)
		}
		if hop > maxRedirects {
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: too many redirects (>%d)", maxRedirects)
		}
		// Service.Extract already validated the initial URL, so only REDIRECT
		// targets (hop > 0) need re-validation here: a local fetch following a 30x
		// to 169.254.169.254 or an internal host is the classic redirect SSRF, and
		// the target must pass the preflight before it is fetched.
		if hop > 0 {
			if err := safenet.ValidateURLContext(ctx, current); err != nil {
				return core.ExtractResponse{}, fmt.Errorf("httpfetch: blocked redirect URL: %w", err)
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: create request: %w", err)
		}
		httpReq.Header.Set("User-Agent", userAgent)
		httpReq.Header.Set("Accept", acceptHeader)
		// NOTE: we do NOT set Accept-Encoding. net/http negotiates gzip and
		// transparently decompresses ONLY when it adds the header itself; setting
		// it by hand would hand us a raw gzip body.

		resp, err := providerhttp.DoWithRetry(ctx, client, httpReq, providerhttp.DefaultRetryOptions())
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return core.ExtractResponse{}, fmt.Errorf("httpfetch: request aborted: %w", cerr)
			}
			// REDACTION: a raw net/http transport error is a *url.Error that carries
			// the full request URL (path + query, e.g. ?token=...). The non-JSON CLI
			// path prints the provider error verbatim, so we must not preserve
			// url.Error.URL. Surface only the underlying transport cause (which holds
			// the dialed host:port + reason, never the URL/query).
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: request failed: %s", redactTransportErr(err))
		}

		// Redirect: read the Location, drain+close, re-validate on the next hop.
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := strings.TrimSpace(resp.Header.Get("Location"))
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
			if location == "" {
				return core.ExtractResponse{}, fmt.Errorf("httpfetch: redirect (%d) without a Location header", resp.StatusCode)
			}
			next, err := resolveRedirect(current, location)
			if err != nil {
				return core.ExtractResponse{}, fmt.Errorf("httpfetch: invalid redirect location: %w", err)
			}
			current = next
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := providerhttp.ReadAllLimited(resp.Body, p.maxBytes)
			_ = resp.Body.Close()
			return core.ExtractResponse{}, providerhttp.NewHTTPStatusError("httpfetch", "extract", resp.StatusCode, body)
		}

		// Only extract textual responses; tokenizing a binary body as HTML would
		// emit mojibake. The media type (a standard header, not a secret) is safe
		// to surface, truncated defensively.
		if ct := mediaType(resp.Header.Get("Content-Type")); ct != "" && !isTextual(ct) {
			_ = resp.Body.Close()
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: unsupported content type %q (only HTML/text is extracted; no JS rendering)", core.TruncateRunes(ct, 100))
		}

		bodyBytes, err := providerhttp.ReadAllLimited(resp.Body, p.maxBytes)
		_ = resp.Body.Close()
		if err != nil {
			// Over-cap is FATAL: never extract from a truncated body. The error
			// records only the byte limit, never body content.
			return core.ExtractResponse{}, fmt.Errorf("httpfetch: read response: %w", err)
		}

		text, title := htmlToText(bodyBytes)
		md := map[string]string{"mode": "http-fetch"}
		if title != "" {
			md["title"] = title
		}
		return core.ExtractResponse{
			URL:      req.URL, // the ORIGINAL request URL, not the redirected target
			Provider: "httpfetch",
			Content:  strings.TrimSpace(text),
			Metadata: md,
		}, nil
	}
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	// Keyless and always available: no key, no subprocess, no network ping. The
	// route walk discovers any real per-call failure at call time. Unbreakered
	// (it is the last-resort fallback), so it leaves CostClass/Breaker* zero — the
	// service/ledger fills cost policy.
	return core.ProviderStatus{
		Name:         p.Name(),
		Available:    true,
		Capabilities: p.Capabilities(),
		Reason:       "keyless pure-Go HTTP fetch + HTML-to-text extractor (no JS rendering)",
	}
}

// noFollowClient returns a shallow copy of the configured client with a no-follow
// redirect policy. Copying (http.Client holds no locks) means neither an injected
// nor the default client can enable auto-follow and bypass the per-hop SSRF
// re-validation in Extract.
func (p Provider) noFollowClient() *http.Client {
	c := *p.httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// safeTransport returns an http.Transport whose dialer re-validates the resolved
// remote IP immediately before connecting. net/http performs its OWN DNS lookup
// at dial time (separate from the safenet preflight), so a DNS-rebinding or
// split-horizon resolver could pass the preflight with a public IP and then dial
// a private/metadata IP. The Control hook runs once per candidate address with
// the resolved IP:port and aborts the dial if it is not a safe public target.
// Defaults mirror http.DefaultTransport so behaviour (proxy env, HTTP/2, idle
// pooling, transparent gzip) is unchanged apart from the dial guard.
func safeTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   validateDialedAddr,
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// validateDialedAddr is the dialer Control hook: address is the resolved IP:port
// net/http is about to connect to. Returning an error aborts that dial. This is
// the dial-time half of the SSRF defense (the preflight is the resolve-time half).
func validateDialedAddr(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("httpfetch: cannot parse dial address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("httpfetch: dial address %q is not a literal IP", host)
	}
	if err := safenet.ValidateIP(ip); err != nil {
		return fmt.Errorf("httpfetch: blocked dial address: %w", err)
	}
	return nil
}

// redactTransportErr renders a transport error WITHOUT any request URL. A
// net/http failure is a *url.Error whose URL field carries the path+query (which
// may include secrets); the underlying cause (a net.OpError) holds only the
// dialed host:port + reason. We peel EVERY nested *url.Error layer (client.Do can
// wrap a transport error that is itself a url.Error) down to that cause so no URL
// survives at any depth.
func redactTransportErr(err error) string {
	for {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Err != nil {
			err = ue.Err
			continue
		}
		break
	}
	if err == nil {
		return "transport error (details redacted)"
	}
	return err.Error()
}

// resolveRedirect resolves a (possibly relative) Location against the current URL.
func resolveRedirect(current, location string) (string, error) {
	base, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	loc, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(loc).String(), nil
}

// mediaType returns the lowercased media type from a Content-Type header value,
// dropping any parameters (e.g. "; charset=utf-8").
func mediaType(contentType string) string {
	ct := contentType
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// isTextual reports whether a media type is one we extract text from. HTML, plain
// text, and XML-family documents qualify; binary types (images, PDFs, archives,
// octet-stream) do not.
func isTextual(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "text/"):
		return true
	case ct == "application/xhtml+xml", ct == "application/xml":
		return true
	case strings.HasSuffix(ct, "+xml"):
		return true
	default:
		return false
	}
}
