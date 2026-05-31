package scrapling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
// Redirect or Content is meaningful: the script emits Redirect (the resolved,
// absolute Location) on a 3xx with follow-redirects disabled, otherwise Content
// plus the FinalURL the fetcher actually landed on (a backstop for a Scrapling
// build that ignores the no-follow request).
type hopOutput struct {
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
	python     string
	configured bool
	timeout    time.Duration
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
	}
	if envPython := strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_PYTHON")); envPython != "" {
		p.python = envPython
		p.configured = true
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

func (p Provider) Name() string { return "scrapling" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilityExtract, core.CapabilityStatus}
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{}, errors.New("scrapling: search is not supported; use extract with a public URL")
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
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
			if err := safenet.ValidateURLContext(ctx, current); err != nil {
				return core.ExtractResponse{}, fmt.Errorf("scrapling: blocked URL: %w", err)
			}
		}
		out, err := p.runHop(ctx, current, req.Format)
		if err != nil {
			return core.ExtractResponse{}, err
		}
		if out.Redirect != "" {
			current = out.Redirect
			continue
		}
		// Backstop: if a Scrapling build ignored the no-follow request and
		// landed somewhere other than we asked, validate that final URL before
		// returning any content — otherwise a redirect to a private host would
		// still leak its body. The same-URL common case is already validated.
		if out.FinalURL != "" && out.FinalURL != current {
			if err := safenet.ValidateURLContext(ctx, out.FinalURL); err != nil {
				return core.ExtractResponse{}, fmt.Errorf("scrapling: blocked redirect target: %w", err)
			}
		}
		return core.ExtractResponse{
			URL:      req.URL,
			Provider: "scrapling",
			Content:  strings.TrimSpace(out.Content),
			Metadata: out.Metadata,
		}, nil
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
	payload := map[string]string{"url": url, "format": format}
	stdin, err := json.Marshal(payload)
	if err != nil {
		return hopOutput{}, fmt.Errorf("scrapling: marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, p.python, "-c", extractScript)
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := newCappedBuffer(maxStdoutBytes)
	stderr := newCappedBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return hopOutput{}, fmt.Errorf("scrapling: extract timed out after %s", p.timeout)
		}
		if stdout.Truncated() || stderr.Truncated() {
			return hopOutput{}, errors.New("scrapling: output too large")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return hopOutput{}, fmt.Errorf("scrapling: extract failed: %s", sanitizeError(msg))
	}

	var out hopOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return hopOutput{}, fmt.Errorf("scrapling: decode response: %w", err)
	}
	return out, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	if !p.configured {
		return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: "NOLE_SCRAPLING_PYTHON is not configured"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.python, "-c", `import scrapling; from scrapling.fetchers import Fetcher; print(getattr(scrapling, "__version__", "unknown"))`)
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
		} else if strings.TrimSpace(stderr.String()) != "" {
			reason = sanitizeError(stderr.String())
		}
		return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: reason}
	}
	version := strings.TrimSpace(stdout.String())
	if version == "" {
		version = "unknown"
	}
	return core.ProviderStatus{Name: p.Name(), Available: true, Capabilities: p.Capabilities(), Reason: "local Python package " + version}
}

func sanitizeError(s string) string {
	// Python subprocess stderr can contain non-ASCII; truncate on a rune
	// boundary so the trailing characters never become invalid UTF-8.
	return core.TruncateRunes(strings.TrimSpace(s), 500)
}

const extractScript = `
import json, sys, re
from html.parser import HTMLParser
from urllib.parse import urljoin

req = json.load(sys.stdin)
url = req.get('url', '')
try:
    from scrapling.fetchers import Fetcher
except Exception as exc:
    raise SystemExit(f'import scrapling.fetchers failed: {exc}')

try:
    fetch = getattr(Fetcher, 'fetch', None) or getattr(Fetcher, 'get')
    try:
        # Do NOT follow redirects inside the fetcher: a redirect to an internal
        # / metadata host must be re-validated by the Go SSRF preflight before
        # the next hop is fetched. The Go caller drives the redirect walk.
        page = fetch(url, follow_redirects=False)
    except TypeError:
        # Fail closed: a Scrapling build that does not accept follow_redirects
        # cannot guarantee a redirect target is re-validated before it is
        # fetched. Refuse rather than retry with redirects enabled, which would
        # let a public->internal 302 perform the SSRF request before Go can
        # inspect it (the final_url backstop only blocks returning the body, not
        # the request). SystemExit is a BaseException, so the outer
        # 'except Exception' below does not swallow it. Upgrade scrapling for
        # redirect-safe extract.
        raise SystemExit('scrapling: installed build does not support follow_redirects=False; upgrade scrapling for redirect-safe extract')
except Exception as exc:
    raise SystemExit(f'Fetcher.fetch/get failed: {exc}')

# On a 3xx (redirect-following disabled), surface the resolved absolute Location
# so the Go caller can re-run the preflight on it before fetching the next hop.
status = getattr(page, 'status', None)
if isinstance(status, int) and 300 <= status < 400:
    location = None
    headers = getattr(page, 'headers', None) or {}
    try:
        header_items = list(headers.items())
    except Exception:
        header_items = []
    for hk, hv in header_items:
        if str(hk).lower() == 'location':
            location = hv
            break
    if location:
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

class TextExtractor(HTMLParser):
    skip_tags = {'script', 'style', 'noscript', 'template', 'svg'}
    block_tags = {'p', 'div', 'section', 'article', 'header', 'footer', 'main', 'br', 'li', 'tr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6'}
    def __init__(self):
        super().__init__()
        self.skip = 0
        self.parts = []
    def handle_starttag(self, tag, attrs):
        tag = tag.lower()
        if tag in self.skip_tags:
            self.skip += 1
        if tag in self.block_tags:
            self.parts.append('\n')
    def handle_endtag(self, tag):
        tag = tag.lower()
        if tag in self.skip_tags and self.skip:
            self.skip -= 1
        if tag in self.block_tags:
            self.parts.append('\n')
    def handle_data(self, data):
        if not self.skip:
            self.parts.append(data)
    def text(self):
        text = ''.join(self.parts)
        text = re.sub(r'[ \t\r\f\v]+', ' ', text)
        text = re.sub(r'\n\s*\n+', '\n\n', text)
        return text.strip()

content = first_attr(page, ['markdown', 'text', 'body_text', 'content'])
if content is None:
    html = first_attr(page, ['html_content', 'html', 'body'])
    if html is None:
        html = str(page)
    parser = TextExtractor()
    parser.feed(str(html))
    content = parser.text()
else:
    content = str(content)

title = selector_text(page, 'title::text')
metadata = {'mode': 'fetcher', 'ai_targeted': 'true'}
if title:
    metadata['title'] = title.strip()
# final_url is the URL the fetcher actually landed on; the Go caller validates
# it as a backstop in case a Scrapling build ignored follow_redirects=False.
final_url = getattr(page, 'url', url) or url
print(json.dumps({'content': content, 'metadata': metadata, 'final_url': str(final_url)}, ensure_ascii=False))
`
