package scrapling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestAutoBrowserUsesUpstreamPageClassification(t *testing.T) {
	for _, tc := range []struct {
		name, mode, title, text, calls, reason string
	}{
		{"title only", "auto", "Example product catalogue", "Example product catalogue", "fetcher\nstealthy\n", "empty_content"},
		{"markdown title", "auto", "Example product catalogue", "# Example product catalogue", "fetcher\nstealthy\n", "empty_content"},
		{"empty body", "auto", "", " \n\t", "fetcher\nstealthy\n", "empty_content"},
		{"akamai", "auto", "Example shop", "Powered and protected by Akamai", "fetcher\nstealthy\n", "bot_blocked"},
		{"title with article", "auto", "Example product catalogue", "Example product catalogue\nUseful details.", "fetcher\n", ""},
		{"short status", "auto", "Operational", "Operational", "fetcher\n", ""},
		{"fetcher title only", "fetcher", "Example product catalogue", "Example product catalogue", "fetcher\n", ""},
		{"explicit browser", "stealthy", "Example product catalogue", "Example product catalogue", "stealthy\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_MODE", tc.mode)
			t.Setenv("NOLE_SCRAPLING_SOLVE_CLOUDFLARE", "false")
			p := browserFixture(t, map[string]any{"status": 200, "title": tc.title, "text": tc.text})
			resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err != nil || browserCalls(t) != tc.calls || resp.Metadata["escalation_reason"] != tc.reason {
				t.Fatalf("calls=%q reason=%q err=%v", browserCalls(t), resp.Metadata["escalation_reason"], err)
			}
		})
	}
}

func TestAutoEmptyBrowserOutcomeFallsBackWithoutCaching(t *testing.T) {
	for _, tc := range []struct {
		name, title, text, failure string
		status                     int
	}{
		{"still title only", "Example product catalogue", "Example product catalogue", "empty_content", 200},
		{"still blocked", "", "Powered and protected by Akamai", "bot_blocked", 200},
		{"rate limited", "", "Too many requests", "rate_limited", 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_MODE", "auto")
			t.Setenv("NOLE_SCRAPLING_SOLVE_CLOUDFLARE", "false")
			p := browserFixture(t, map[string]any{"status": 200, "title": "Example product catalogue", "text": "Example product catalogue"})
			page, _ := json.Marshal(map[string]any{"status": tc.status, "title": tc.title, "text": tc.text})
			path := filepath.Join(t.TempDir(), "rendered.json")
			if err := os.WriteFile(path, page, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NOLE_TEST_BROWSER_PAGE", path)
			primary := &countedBlockProvider{Provider: p, name: "scrapling"}
			fallback := &countedBlockProvider{Provider: configuredProvider(func(context.Context, string, string) (hopOutput, error) {
				return hopOutput{Content: "Useful fallback."}, nil
			}), name: "fallback"}
			service := blockTestService(t, primary, fallback)
			defer service.Close()
			for i := 0; i < 2; i++ {
				resp, err := service.Extract(context.Background(), core.ExtractRequest{URL: pubA})
				if err != nil || resp.Provider != "fallback" {
					t.Fatalf("fallback failed: provider=%s err=%v", resp.Provider, err)
				}
				if i == 0 {
					var a core.RouteAttempt
					for _, attempt := range resp.RouteTrace {
						if attempt.Provider == "scrapling" {
							a = attempt
							break
						}
					}
					if a.Mode != "stealthy" || a.EscalationReason != "empty_content" || a.Reason != tc.failure {
						t.Fatalf("wrong attempt: %#v", a)
					}
				}
			}
			if browserCalls(t) != "fetcher\nstealthy\n" || primary.calls != 1 || fallback.calls != 1 {
				t.Fatal("failed browser repeated or cached instead of useful fallback")
			}
		})
	}
}

func TestAutoEmptyContentDoesNotReplaceRedirectValidation(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "public redirect", true: "unsafe final URL"}[blocked], func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_MODE", "auto")
			p := browserFixture(t, map[string]any{})
			calls := 0
			p.hopFn = func(context.Context, string, string) (hopOutput, error) {
				calls++
				if blocked {
					return hopOutput{Status: 200, FinalURL: "http://127.0.0.1/private"}, nil
				}
				if calls == 1 {
					return hopOutput{Redirect: pubB}, nil
				}
				return hopOutput{Status: 200, Content: "Useful final page."}, nil
			}
			_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if blocked && (err == nil || !strings.Contains(err.Error(), "blocked redirect target")) || !blocked && err != nil {
				t.Fatalf("redirect outcome: %v", err)
			}
			if browserCalls(t) != "" {
				t.Fatal("redirect or unsafe final URL launched browser")
			}
			if !blocked && calls != 2 {
				t.Fatal("redirect was not followed by Fetcher")
			}
		})
	}
}

func TestAutoRedirectEmptyRendersCheckedTargetAndCachesSafety(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_MODE", "auto")
	t.Setenv("NOLE_SCRAPLING_SOLVE_CLOUDFLARE", "")
	p := browserFixture(t, map[string]any{})
	loadedURL := filepath.Join(t.TempDir(), "browser-url")
	t.Setenv("NOLE_TEST_BROWSER_URL", loadedURL)
	hops := 0
	p.hopFn = func(_ context.Context, url, _ string) (hopOutput, error) {
		hops++
		if hops == 1 {
			return hopOutput{Redirect: pubB}, nil
		}
		if url != pubB {
			t.Errorf("Fetcher did not follow the redirect")
		}
		return hopOutput{Status: 200, FinalURL: pubB}, nil
	}
	primary := &countedBlockProvider{Provider: p, name: "scrapling"}
	service := blockTestService(t, primary)
	defer service.Close()
	first, err := service.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err != nil || first.Provider != "scrapling" || first.ContentSafety.Risk != core.ContentRiskHigh {
		t.Fatalf("rendered safety missing: provider=%s risk=%s err=%v", first.Provider, first.ContentSafety.Risk, err)
	}
	var attempt core.RouteAttempt
	for _, a := range first.RouteTrace {
		if a.Provider == "scrapling" {
			attempt = a
			break
		}
	}
	if attempt.Mode != "stealthy" || attempt.EscalationReason != "empty_content" || attempt.Status != "success" {
		t.Fatalf("successful escalation trace missing: %#v", attempt)
	}
	url, err := os.ReadFile(loadedURL)
	if err != nil || string(url) != pubB {
		t.Fatal("browser did not open the checked redirect target")
	}
	second, err := service.Extract(context.Background(), core.ExtractRequest{URL: pubA})
	if err != nil || second.ContentSafety.Risk != first.ContentSafety.Risk || len(second.RouteTrace) == 0 || second.RouteTrace[0].CacheStatus != core.CacheStatusHit {
		t.Fatalf("rendered cache/safety missing: %v", err)
	}
	if hops != 2 || primary.calls != 1 || browserCalls(t) != "stealthy\n" {
		t.Fatal("cached rendered page was fetched again")
	}
}
