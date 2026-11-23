package scrapling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safenet"
)

// maxScraplingRedirects bounds how many redirect hops Extract will follow. Each
// hop's target is re-validated against the SSRF preflight before it is fetched,
// so a public URL that 30x-redirects to a private/metadata host is rejected at
// the redirecting hop instead of being fetched locally.
const maxScraplingRedirects = 5

// hopOutput is the parsed result of a single subprocess fetch. Exactly one of
// Redirect, Failure or Content is meaningful: the script emits Redirect (the resolved,
// absolute Location) on a redirect with follow-redirects disabled, otherwise Content
// plus the FinalURL the fetcher actually landed on (a backstop for a Scrapling
// build that ignores the no-follow request).
type hopOutput struct {
	Status   int               `json:"status"`
	Failure  string            `json:"failure"`
	Redirect string            `json:"redirect"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	FinalURL string            `json:"final_url"`
}

// Output caps bound the memory a single subprocess can consume. The ctx
// timeout already bounds runtime; these bound RAM so a runaway or hostile
// local fetch cannot OOM the host process. stdout is generous because it
// carries extracted page content; stderr is small because it only carries
// diagnostics.
const (
	maxStdoutBytes = 64 << 20 // 64 MiB
	maxStderrBytes = 64 << 10 // 64 KiB
)

// Bound pipe draining even if a descendant keeps a subprocess pipe open.
const subprocessWaitDelay = 2 * time.Second

// cappedBuffer is an io.Writer that buffers at most max bytes. Once the cap is
// exceeded it stops buffering and flags truncation; cmd.Run reports the write
// error so callers can surface a clear "output too large" message instead of
// growing without bound.
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func newCappedBuffer(max int) *cappedBuffer { return &cappedBuffer{max: max} }

// Write appends up to the remaining capacity and returns an error once the cap
// is exceeded. Returning a non-nil error makes os/exec abandon further reads,
// so the buffer never grows past max.
func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.truncated {
		return 0, errCapExceeded
	}
	remaining := c.max - c.buf.Len()
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	// Keep whatever still fits, then signal the overflow.
	if remaining > 0 {
		c.buf.Write(p[:remaining])
	}
	c.truncated = true
	return remaining, errCapExceeded
}

func (c *cappedBuffer) Bytes() []byte   { return c.buf.Bytes() }
func (c *cappedBuffer) String() string  { return c.buf.String() }
func (c *cappedBuffer) Truncated() bool { return c.truncated }

// errCapExceeded is the sentinel returned by cappedBuffer.Write once the cap is
// hit. It is never surfaced to users directly; callers translate it into a
// redaction-safe "output too large" message.
var errCapExceeded = errors.New("scrapling: output exceeded size cap")

type Provider struct {
	python          string
	configured      bool
	proxy           string
	proxyErr        error
	timeout         time.Duration
	mode            string
	browserTimeout  time.Duration
	solveCloudflare bool
	browserEndpoint string
	browserProxy    func(context.Context, string) (string, func(), error)
	browserLaunch   func(context.Context, string, string) (string, func(), error)
	configErr       error
	// hopFn overrides the real subprocess fetch for a single hop. Nil in
	// production (execHop runs the Python subprocess); tests set it to simulate
	// redirect chains without a live Scrapling runtime.
	hopFn func(ctx context.Context, url, format string) (hopOutput, error)
}

type Option func(*Provider)

func WithPython(path string) Option {
	return func(p *Provider) {
		p.python = strings.TrimSpace(path)
		p.configured = p.python != ""
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(p *Provider) { p.timeout = timeout }
}

func New(opts ...Option) Provider {
	p := Provider{
		timeout: 30 * time.Second,
		proxy:   strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_PROXY")),
	}
	p.proxyErr = validateProxy(p.proxy)
	p.configureBrowser()
	if envPython := strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_PYTHON")); envPython != "" {
		p.python = envPython
		p.configured = true
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// Proxy endpoints are operator configuration and may be local/private. Only
// syntax is checked here; extraction targets still use the public-URL preflight.
func validateProxy(raw string) error {
	if raw == "" {
		return nil
	}
	invalid := errors.New("scrapling: invalid NOLE_SCRAPLING_PROXY")
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return invalid
	}
	switch u.Scheme {
	case "http", "https", "socks4", "socks4a", "socks5", "socks5h":
	default:
		return invalid
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return invalid
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return invalid
	}
	return nil
}

// Python receives the explicit proxy over stdin and does not need provider keys.
// Strip ambient proxy settings only when an explicit proxy must take precedence.
func (p Provider) subprocessEnv() []string {
	environ := os.Environ()
	env := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToLower(key) {
		case "nole_scrapling_proxy", "firecrawl_api_key", "brave_api_key",
			"brave_search_api_key", "tavily_api_key", "tinyfish_api_key",
			"openclaw_gateway_token":
			continue
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			if p.proxy != "" {
				continue
			}
		}
		env = append(env, entry)
	}
	return env
}

func (p Provider) Name() string { return "scrapling" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilityExtract, core.CapabilityStatus}
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{}, errors.New("scrapling: search is not supported; use extract with a public URL")
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (resp core.ExtractResponse, resultErr error) {
	if p.configErr != nil {
		return core.ExtractResponse{}, p.configErr
	}
	if p.proxyErr != nil {
		return core.ExtractResponse{}, p.proxyErr
	}
	if strings.TrimSpace(req.URL) == "" {
		return core.ExtractResponse{}, errors.New("scrapling: url is required")
	}
	if !p.configured {
		return core.ExtractResponse{}, errors.New("scrapling: NOLE_SCRAPLING_PYTHON is not configured")
	}
	// One timeout budget across the whole redirect walk so a chain of hops
	// cannot extend total runtime beyond the provider timeout.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	mode, escalation := "fetcher", ""
	if p.mode == "stealthy" {
		mode = "stealthy"
	}
	defer func() {
		if resultErr != nil && (mode == "stealthy" || p.mode == "auto") {
			resp.Metadata = map[string]string{"mode": mode}
			if escalation != "" {
				resp.Metadata["escalation_reason"] = escalation
			}
		}
	}()
	fetchCtx := ctx
	if p.mode == "auto" {
		var fetchCancel context.CancelFunc
		fetchCtx, fetchCancel = context.WithTimeout(ctx, 30*time.Second)
		defer fetchCancel()
	}

	current := strings.TrimSpace(req.URL)
	for hop := 0; ; hop++ {
		if hop > maxScraplingRedirects {
			return core.ExtractResponse{}, fmt.Errorf("scrapling: too many redirects (>%d)", maxScraplingRedirects)
		}
		// Service.Extract already validated the initial URL, so only REDIRECT
		// targets (hop > 0) need re-validation here: the local fetcher following
		// a 30x to 169.254.169.254 or an internal host is the classic
		// redirect-based SSRF, and the target must pass the preflight before it
		// is fetched.
		if hop > 0 {
			if err := safenet.ValidateURLContext(fetchCtx, current); err != nil {
				return core.ExtractResponse{}, errors.New("scrapling: blocked URL")
			}
		}
		var out hopOutput
		var err error
		if mode == "stealthy" {
			out, err = p.runBrowser(ctx, current, req.Format)
		} else {
			out, err = p.runHop(fetchCtx, current, req.Format)
			if err == nil && out.Failure == "bot_blocked" && p.mode == "auto" {
				mode, escalation = "stealthy", "bot_blocked"
				out, err = p.runBrowser(ctx, current, req.Format)
			}
		}
		if err != nil {
			return core.ExtractResponse{}, err
		}
		if out.Redirect != "" {
			if mode == "stealthy" {
				return core.ExtractResponse{}, errors.New("scrapling: invalid browser redirect response")
			}
			current = out.Redirect
			continue
		}
		if err := responseError(out); err != nil {
			return core.ExtractResponse{}, err
		}
		// Backstop: if a Scrapling build ignored the no-follow request and
		// landed somewhere other than we asked, validate that final URL before
		// returning any content — otherwise a redirect to a private host would
		// still leak its body. The same-URL common case is already validated.
		if out.FinalURL != "" && out.FinalURL != current {
			validationCtx := ctx
			if mode == "fetcher" {
				validationCtx = fetchCtx
			}
			if err := safenet.ValidateURLContext(validationCtx, out.FinalURL); err != nil {
				return core.ExtractResponse{}, errors.New("scrapling: blocked redirect target")
			}
		}
		if out.Metadata == nil {
			out.Metadata = make(map[string]string)
		}
		out.Metadata["mode"] = mode
		if escalation != "" {
			out.Metadata["escalation_reason"] = escalation
		}
		return core.ExtractResponse{
			URL:      req.URL,
			Provider: "scrapling",
			Content:  strings.TrimSpace(out.Content),
			Metadata: out.Metadata,
		}, nil
	}
}

// responseError accepts only fixed failure codes from the subprocess. Page
// text, headers and URLs must never become error messages or route reasons.
func responseError(out hopOutput) error {
	switch out.Failure {
	case "bot_blocked":
		return fmt.Errorf("scrapling: %w", core.ErrBotBlocked)
	case "rate_limited":
		return fmt.Errorf("scrapling: %w (HTTP 429)", core.ErrTargetRateLimited)
	case "http_error":
		return fmt.Errorf("scrapling: http_error (HTTP %d)", out.Status)
	case "runtime_unavailable":
		return errors.New("scrapling: fetcher runtime unavailable; install scrapling[fetchers]")
	case "unsupported_runtime":
		return errors.New("scrapling: installed runtime does not support the requested fetch options; upgrade scrapling[fetchers]")
	case "fetch_failed":
		return errors.New("scrapling: fetch failed; check runtime and proxy configuration")
	case "browser_unavailable":
		return errors.New("scrapling: browser runtime unavailable; install optional Scrapling browser components")
	case "browser_proxy_unsupported":
		return errors.New("scrapling: configured proxy is unsupported by browser mode")
	case "browser_guard_failed", "browser_fetch_failed":
		return errors.New("scrapling: browser extraction failed; check optional browser installation and runtime")
	case "":
		return nil
	default:
		return errors.New("scrapling: invalid failure code")
	}
}

// runHop fetches a single URL (no internal redirect following) and returns the
// parsed result. Tests inject hopFn; production uses execHop.
func (p Provider) runHop(ctx context.Context, url, format string) (hopOutput, error) {
	if p.hopFn != nil {
		return p.hopFn(ctx, url, format)
	}
	return p.execHop(ctx, url, format)
}

// execHop runs the Python subprocess for one fetch and parses its single JSON
// line into a hopOutput.
func (p Provider) execHop(ctx context.Context, url, format string) (hopOutput, error) {
	return p.execMode(ctx, url, format, "fetcher", "")
}

func (p Provider) execMode(ctx context.Context, url, format, mode, profile string) (hopOutput, error) {
	payload := map[string]any{"url": url, "format": format, "mode": mode}
	script := extractScript
	if mode == "stealthy" {
		script = browserRuntimeScript + "\n" + extractScript
		payload["browser_timeout_ms"] = remainingMilliseconds(ctx, p.browserTimeout)
		payload["profile"] = profile
		payload["solve_cloudflare"] = p.solveCloudflare
		payload["cdp_url"] = p.browserEndpoint
	} else if p.mode == "auto" {
		payload["auto"] = true
		payload["fetch_timeout"] = float64(remainingMilliseconds(ctx, 30*time.Second)) / 1000
	}
	if p.proxy != "" {
		payload["proxy"] = p.proxy
	}
	stdin, err := json.Marshal(payload)
	if err != nil {
		return hopOutput{}, fmt.Errorf("scrapling: marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, p.python, "-c", script)
	cmd.Env = p.subprocessEnv()
	cmd.WaitDelay = subprocessWaitDelay
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := newCappedBuffer(maxStdoutBytes)
	stderr := newCappedBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	var runErr error
	if mode == "stealthy" {
		runErr = runBrowserCommand(cmd)
	} else {
		runErr = cmd.Run()
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return hopOutput{}, errors.New("scrapling: extraction deadline exceeded")
		}
		if stdout.Truncated() || stderr.Truncated() {
			return hopOutput{}, errors.New("scrapling: output too large")
		}
		// Runtime diagnostics can include proxy credentials, addresses, and page
		// payloads. Never promote stderr or execution errors to public output.
		return hopOutput{}, errors.New("scrapling: extract process failed; check runtime and proxy configuration")
	}

	var out hopOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return hopOutput{}, errors.New("scrapling: invalid runtime response")
	}
	return out, nil
}

var scraplingVersionPattern = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4}){1,2}([.+-]?[A-Za-z][0-9A-Za-z]{0,15})?$`)

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	return p.status(ctx, true)
}

// ExtractionStatus checks configuration/imports only. Doctor/provider_status
// retain the deep Status probe; Extract owns its one actual browser attempt.
func (p Provider) ExtractionStatus(ctx context.Context) core.ProviderStatus {
	return p.status(ctx, false)
}

func (p Provider) status(ctx context.Context, probeBrowser bool) (status core.ProviderStatus) {
	defer func() {
		if p.proxy != "" {
			status.Reason += "; proxy: configured"
		}
	}()
	if p.configErr != nil {
		return core.ProviderStatus{Name: p.Name(), Capabilities: p.Capabilities(), Reason: p.configErr.Error()}
	}
	if p.proxyErr != nil {
		return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: p.proxyErr.Error()}
	}
	if !p.configured {
		return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: "NOLE_SCRAPLING_PYTHON is not configured"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if p.mode == "stealthy" && !browserProcessSupported {
		return core.ProviderStatus{Name: p.Name(), Capabilities: p.Capabilities(), Reason: "browser mode unsupported on this platform"}
	}
	if p.mode == "stealthy" && probeBrowser {
		ready, reason := p.browserReadiness(ctx)
		return core.ProviderStatus{Name: p.Name(), Available: ready, Capabilities: p.Capabilities(), Reason: reason}
	}
	importScript := `import scrapling; from scrapling.fetchers import Fetcher; print(getattr(scrapling, '__version__', 'unknown'))`
	if p.mode == "stealthy" {
		importScript = `import scrapling; from scrapling.fetchers import RemoteSession`
	}
	cmd := exec.CommandContext(ctx, p.python, "-c", importScript)
	cmd.Env = p.subprocessEnv()
	cmd.WaitDelay = subprocessWaitDelay
	stdout := newCappedBuffer(maxStdoutBytes)
	stderr := newCappedBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if stdout.Truncated() || stderr.Truncated() {
			return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: "scrapling: output too large"}
		}
		reason := "scrapling Python package not available"
		if strings.Contains(stderr.String(), "ModuleNotFoundError") || strings.Contains(stderr.String(), "No module named") {
			reason = `scrapling Python package not available; install with: pip install "scrapling[fetchers]"`
		}
		return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: reason}
	}
	// Import stdout is runtime-controlled. Show it only when it looks like a
	// version string, so nothing else from the runtime reaches status output.
	version := strings.TrimSpace(stdout.String())
	if !scraplingVersionPattern.MatchString(version) {
		version = "unknown"
	}
	reason := "local Python package " + version
	if p.mode == "stealthy" {
		reason = "browser package available; launch checked during extraction"
	} else if p.mode == "auto" {
		reason += "; mode: auto; browser not checked"
		if probeBrowser {
			_, browserReason := p.browserReadiness(ctx)
			reason += "; " + browserReason
		}
	}
	return core.ProviderStatus{Name: p.Name(), Available: true, Capabilities: p.Capabilities(), Reason: reason}
}

const extractScript = `
import json, sys, re
from html.parser import HTMLParser
from urllib.parse import urljoin

req = json.load(sys.stdin)
url = req.get('url', '')

def fail(code):
    print(json.dumps({'failure': code}))
    raise SystemExit(0)

mode = req.get('mode', 'fetcher')
if mode == 'stealthy':
    try:
        page = fetch_browser(url, timeout_ms=req['browser_timeout_ms'], proxy=req.get('proxy'), profile=req['profile'], cdp_url=req['cdp_url'], solve_cloudflare=req['solve_cloudflare'])
    except BrowserUnavailable:
        fail('browser_unavailable')
    except BrowserProxyError:
        fail('browser_proxy_unsupported')
    except BrowserGuardError:
        fail('browser_guard_failed')
    except Exception:
        fail('browser_fetch_failed')
else:
    try:
        from scrapling.fetchers import Fetcher
    except Exception:
        fail('runtime_unavailable')

    try:
        fetch = getattr(Fetcher, 'fetch', None) or getattr(Fetcher, 'get')
        try:
            # Do NOT follow redirects inside the fetcher: a redirect to an internal
            # / metadata host must be re-validated by the Go SSRF preflight before
            # the next hop is fetched. The Go caller drives the redirect walk.
            kwargs = {'follow_redirects': False}
            if req.get('auto'):
                kwargs.update(retries=1, timeout=req.get('fetch_timeout', 30))
            if req.get('proxy'):
                kwargs['proxy'] = req['proxy']
            page = fetch(url, **kwargs)
        except TypeError:
            # Fail closed: a Scrapling build that does not accept follow_redirects
            # cannot guarantee a redirect target is re-validated before it is
            # fetched. Refuse rather than retry with redirects enabled, which would
            # let a public->internal 302 perform the SSRF request before Go can
            # inspect it (the final_url backstop only blocks returning the body, not
            # the request). SystemExit is a BaseException, so the outer
            # 'except Exception' below does not swallow it. Upgrade scrapling for
            # redirect-safe extract.
            # Unsupported proxy arguments also reach this branch. Never retry by
            # dropping either the proxy or redirect restriction.
            fail('unsupported_runtime')
    except Exception:
        fail('fetch_failed')

def has_challenge_header(headers):
    return any(str(key).lower() == 'cf-mitigated' and str(value).strip().lower() == 'challenge'
               for key, value in headers)

try:
    headers = list((getattr(page, 'headers', None) or {}).items())
except Exception:
    headers = []

# Preserve known status internally; older response objects may omit it.
status = getattr(page, 'status', None)
status = status if isinstance(status, int) else 0
# Surface the resolved Location so Go can re-run the preflight before fetching.
if mode == 'fetcher' and status in (301, 302, 303, 307, 308):
    location = next((value for key, value in headers if str(key).lower() == 'location'), None)
    if location:
        if has_challenge_header(headers):
            fail('bot_blocked')
        print(json.dumps({'redirect': urljoin(url, str(location))}, ensure_ascii=False))
        raise SystemExit(0)

def first_attr(obj, names):
    for name in names:
        try:
            value = getattr(obj, name)
        except Exception:
            continue
        if callable(value):
            try:
                value = value()
            except TypeError:
                continue
            except Exception:
                continue
        if value:
            return value
    return None

def selector_text(page, selector):
    try:
        result = page.css(selector)
        if hasattr(result, 'get'):
            value = result.get()
            if value:
                return str(value)
        if result:
            return str(result[0])
    except Exception:
        return ''
    return ''

def normalized_text(text):
    return ' '.join(text.lower().split()).strip(' .!')

def response_failure(status, headers, title, content, noscript):
    # Keep this helper independent of Fetcher so other fetch modes can use the
    # same classification. Only fixed codes leave the process on failure.
    # A 429 always stays a rate limit, even when it also has challenge signals.
    if status == 429:
        return 'rate_limited'
    if has_challenge_header(headers):
        return 'bot_blocked'
    # Bound text inspection; never classify a truncated prefix as a complete
    # interstitial. Isolated words and length alone are not block signals.
    if len(content) <= 8192:
        text = normalized_text(content)
        heading = normalized_text(title) if len(title) <= 256 else ''
        if noscript and (not text or text == heading):
            content = noscript
            text = normalized_text(content)
        messages = {
            'please enable javascript to continue',
            'enable javascript and cookies to continue',
            'you need to enable javascript to run this app',
            # Akamai Bot Manager challenge pages: the whole visible text is the
            # footer line (the logo is an image).
            'powered and protected by',
            'powered and protected by akamai',
        }
        # The HTML fallback includes the <title> text; compare the body alone too.
        body = text[len(heading):].strip(' .!') if heading and text.startswith(heading) else text
        if text in messages or body in messages:
            return 'bot_blocked'
        if heading == 'just a moment':
            remainder = re.sub(r'^\s*just\s+a\s+moment[\s.!]*', '', content, flags=re.IGNORECASE).strip()
            if len(remainder) <= 512 and normalized_text(remainder).startswith('enable javascript and cookies to continue'):
                return 'bot_blocked'
    if status and not 200 <= status < 300:
        return 'http_error'
    return ''

class TextExtractor(HTMLParser):
    skip_tags = {'script', 'style', 'noscript', 'template', 'svg'}
    block_tags = {'p', 'div', 'section', 'article', 'header', 'footer', 'main', 'br', 'li', 'tr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6'}
    def __init__(self):
        super().__init__()
        self.skip = 0
        self.parts = []
        self.in_noscript = False
        self.noscript = bytearray()
    def handle_starttag(self, tag, attrs):
        tag = tag.lower()
        if tag == 'noscript' and not self.skip:
            self.in_noscript = True
        if tag in self.skip_tags:
            self.skip += 1
        if tag in self.block_tags:
            self.parts.append('\n')
            self.noscript_break()
    def handle_endtag(self, tag):
        tag = tag.lower()
        if tag in self.skip_tags and self.skip:
            self.skip -= 1
        if tag == 'noscript' and not self.skip:
            self.in_noscript = False
        if tag in self.block_tags:
            self.parts.append('\n')
            self.noscript_break()
    def noscript_break(self):
        if self.in_noscript and self.skip == 1 and len(self.noscript) < 2048:
            self.noscript.extend(b'\n')
    def handle_data(self, data):
        if not self.skip:
            self.parts.append(data)
        elif self.in_noscript and self.skip == 1:
            remaining = 2048 - len(self.noscript)
            self.noscript.extend(data[:remaining].encode('utf-8', errors='replace')[:remaining])
    def text(self):
        text = ''.join(self.parts)
        text = re.sub(r'[ \t\r\f\v]+', ' ', text)
        text = re.sub(r'\n\s*\n+', '\n\n', text)
        return text.strip()

content = first_attr(page, ['markdown', 'text', 'body_text', 'content'])
noscript = ''
if content is None:
    html = first_attr(page, ['html_content', 'html', 'body'])
    if html is None:
        html = str(page)
    if isinstance(html, (bytes, bytearray)):
        html = bytes(html).decode('utf-8', errors='replace')
    parser = TextExtractor()
    parser.feed(str(html))
    content = parser.text()
    noscript = parser.noscript.decode('utf-8', errors='ignore')
else:
    content = str(content)

title = selector_text(page, 'title::text')
failure = response_failure(status, headers, title, content, noscript)
if failure:
    print(json.dumps({'status': status, 'failure': failure}))
    raise SystemExit(0)
# A page whose only readable text is its title was built by JavaScript the
# fetcher does not run. Return no content so the router tries the next reader
# instead of accepting the title as the page.
def title_only(title, content):
    # Exact match after case and whitespace folding only; one- or two-word
    # titles ("Operational", "123") are too often a page's real content.
    if len(content) > 512:
        return False
    heading = ' '.join(title.lower().split())
    body = ' '.join(re.sub(r'^[ \t]*#{1,6}[ \t]+', '', content).lower().split())
    return len(heading.split()) >= 3 and body == heading

if title_only(title, content):
    content = ''
metadata = {'mode': mode, 'ai_targeted': 'true'}
if title:
    metadata['title'] = title.strip()
# final_url is the URL the fetcher actually landed on; the Go caller validates
# it as a backstop in case a Scrapling build ignored follow_redirects=False.
final_url = getattr(page, 'url', url) or url
print(json.dumps({'status': status, 'content': content, 'metadata': metadata, 'final_url': str(final_url)}, ensure_ascii=False))
`
