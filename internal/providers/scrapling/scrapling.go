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
)

type Provider struct {
	python  string
	timeout time.Duration
}

type Option func(*Provider)

func WithPython(path string) Option {
	return func(p *Provider) { p.python = path }
}

func WithTimeout(timeout time.Duration) Option {
	return func(p *Provider) { p.timeout = timeout }
}

func New(opts ...Option) Provider {
	p := Provider{
		python:  "python3",
		timeout: 30 * time.Second,
	}
	if envPython := strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_PYTHON")); envPython != "" {
		p.python = envPython
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
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	payload := map[string]string{"url": req.URL, "format": req.Format}
	stdin, err := json.Marshal(payload)
	if err != nil {
		return core.ExtractResponse{}, fmt.Errorf("scrapling: marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, p.python, "-c", extractScript)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return core.ExtractResponse{}, fmt.Errorf("scrapling: extract timed out after %s", p.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return core.ExtractResponse{}, fmt.Errorf("scrapling: extract failed: %s", sanitizeError(msg))
	}

	var out struct {
		Content  string            `json:"content"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return core.ExtractResponse{}, fmt.Errorf("scrapling: decode response: %w", err)
	}
	return core.ExtractResponse{
		URL:      req.URL,
		Provider: "scrapling",
		Content:  strings.TrimSpace(out.Content),
		Metadata: out.Metadata,
	}, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.python, "-c", `import scrapling; print(getattr(scrapling, "__version__", "unknown"))`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
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
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	return s
}

const extractScript = `
import json, sys, re
from html.parser import HTMLParser

req = json.load(sys.stdin)
url = req.get('url', '')
try:
    from scrapling.fetchers import Fetcher
except Exception as exc:
    raise SystemExit(f'import scrapling.fetchers failed: {exc}')

try:
    fetch = getattr(Fetcher, 'fetch', None) or getattr(Fetcher, 'get')
    page = fetch(url)
except Exception as exc:
    raise SystemExit(f'Fetcher.fetch/get failed: {exc}')

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
print(json.dumps({'content': content, 'metadata': metadata}, ensure_ascii=False))
`
