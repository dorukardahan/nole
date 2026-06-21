package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

func TestTavilyUsageReadsAccountUsage(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" || r.Method != http.MethodGet {
			t.Fatalf("usage endpoint = %s %s, want GET /usage", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": map[string]any{
				"usage": 1000,
				"limit": 1000,
			},
			"account": map[string]any{
				"current_plan": "Researcher",
			},
		})
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	usage, err := p.Usage(context.Background())
	if err != nil {
		t.Fatalf("usage failed: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if usage.Provider != "tavily" || usage.Source != "tavily_usage" || usage.NativeUnit != "credits" {
		t.Fatalf("unexpected usage identity: %#v", usage)
	}
	if usage.NativeRemaining == nil || *usage.NativeRemaining != 0 {
		t.Fatalf("native remaining = %#v, want 0", usage.NativeRemaining)
	}
	if usage.RemainingCalls == nil || *usage.RemainingCalls != 0 {
		t.Fatalf("remaining calls = %#v, want conservative 0", usage.RemainingCalls)
	}
	if usage.LimitCalls == nil || *usage.LimitCalls != 500 {
		t.Fatalf("limit calls = %#v, want 500", usage.LimitCalls)
	}
}

func TestTavilyUsageFailureDoesNotTripSearchBreaker(t *testing.T) {
	t.Setenv("NOLE_RETRY_MAX_ATTEMPTS", "1")
	breaker := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Hour})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/usage":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"usage endpoint throttled"}`))
		case "/search":
			_ = json.NewEncoder(w).Encode(tavilySearchResponse{
				Query:   "nole",
				Results: []tavilyResult{{Title: "Nólë", URL: "https://example.com/nole", Content: "router"}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(WithAPIKey("test-key"), WithBaseURL(srv.URL), WithBreaker(breaker))
	if _, err := p.Usage(context.Background()); err == nil {
		t.Fatal("expected usage endpoint failure")
	}
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Task: core.TaskGeneral, Limit: 1})
	if err != nil {
		t.Fatalf("usage failure must not open search breaker: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("search results = %#v, want one", resp.Results)
	}
}
