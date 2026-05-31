package scrapling

import (
	"context"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// configuredProvider returns a provider that reports as configured but never
// runs the real Python subprocess — hopFn replaces execHop so the redirect
// walk can be driven deterministically without a live Scrapling runtime.
func configuredProvider(hop func(ctx context.Context, url, format string) (hopOutput, error)) Provider {
	p := New(WithPython("/usr/bin/true"))
	p.hopFn = hop
	return p
}

// Literal public IPs pass the SSRF preflight without a DNS lookup, keeping these
// tests hermetic/offline.
const (
	pubA     = "https://93.184.216.34/page"
	pubB     = "https://1.1.1.1/next"
	metadata = "http://169.254.169.254/latest/meta-data/"
	private  = "http://10.0.0.1/internal"
)

func TestScraplingExtractNoRedirectReturnsContent(t *testing.T) {
	calls := 0
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		calls++
		return hopOutput{Content: "hello", Metadata: map[string]string{"mode": "fetcher"}, FinalURL: url}, nil
	})
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content = %q, want hello", resp.Content)
	}
	if resp.URL != pubA {
		t.Fatalf("response URL = %q, want original %q", resp.URL, pubA)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 fetch, got %d", calls)
	}
}

func TestScraplingExtractFollowsPublicRedirect(t *testing.T) {
	var seen []string
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		seen = append(seen, url)
		if url == pubA {
			return hopOutput{Redirect: pubB}, nil
		}
		return hopOutput{Content: "final", FinalURL: url}, nil
	})
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "final" {
		t.Fatalf("content = %q, want final", resp.Content)
	}
	if len(seen) != 2 || seen[0] != pubA || seen[1] != pubB {
		t.Fatalf("expected hops [%s %s], got %v", pubA, pubB, seen)
	}
}

func TestScraplingExtractBlocksRedirectToMetadata(t *testing.T) {
	var seen []string
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		seen = append(seen, url)
		if url == pubA {
			return hopOutput{Redirect: metadata}, nil
		}
		t.Fatalf("internal redirect target must NOT be fetched, but a hop to %q was attempted", url)
		return hopOutput{}, nil
	})
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err == nil {
		t.Fatal("expected the redirect to a metadata address to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked URL") {
		t.Fatalf("error should report a blocked URL, got %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("only the initial URL should be fetched; the internal target must be rejected pre-fetch, hops=%v", seen)
	}
}

func TestScraplingExtractBlocksRedirectToPrivate(t *testing.T) {
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		if url == pubA {
			return hopOutput{Redirect: private}, nil
		}
		t.Fatalf("private redirect target must NOT be fetched (%q)", url)
		return hopOutput{}, nil
	})
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err == nil || !strings.Contains(err.Error(), "blocked URL") {
		t.Fatalf("expected a blocked-URL error for a private redirect target, got %v", err)
	}
}

func TestScraplingExtractRejectsTooManyRedirects(t *testing.T) {
	calls := 0
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		calls++
		return hopOutput{Redirect: pubB}, nil // always redirect to a public target
	})
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubB})
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected a too-many-redirects error, got %v", err)
	}
	if calls != maxScraplingRedirects+1 {
		t.Fatalf("expected %d fetches before bailing, got %d", maxScraplingRedirects+1, calls)
	}
}

func TestScraplingExtractBackstopBlocksPrivateFinalURL(t *testing.T) {
	// Simulates a Scrapling build that ignored follow_redirects=False and
	// followed a redirect to a private host itself: the content must not be
	// returned, even though no Go-driven redirect hop occurred.
	p := configuredProvider(func(ctx context.Context, url, format string) (hopOutput, error) {
		return hopOutput{Content: "internal-body", FinalURL: private}, nil
	})
	_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err == nil || !strings.Contains(err.Error(), "blocked redirect target") {
		t.Fatalf("expected the final-URL backstop to block a private landing, got %v", err)
	}
}
