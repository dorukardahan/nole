package firecrawl

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

func TestFirecrawlUsageReadsTeamCreditUsage(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/team/credit-usage" || r.Method != http.MethodGet {
			t.Fatalf("usage endpoint = %s %s, want GET /team/credit-usage", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"remainingCredits":   0,
				"planCredits":        1000,
				"billingPeriodStart": "2026-06-01T00:00:00Z",
				"billingPeriodEnd":   "2026-06-30T23:59:59Z",
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
	if usage.Provider != "firecrawl" || usage.Source != "firecrawl_team_credit_usage" || usage.NativeUnit != "credits" {
		t.Fatalf("unexpected usage identity: %#v", usage)
	}
	if usage.NativeRemaining == nil || *usage.NativeRemaining != 0 {
		t.Fatalf("native remaining = %#v, want 0", usage.NativeRemaining)
	}
	if usage.RemainingCalls == nil || *usage.RemainingCalls != 0 {
		t.Fatalf("remaining calls = %#v, want conservative 0", usage.RemainingCalls)
	}
	if usage.LimitCalls == nil || *usage.LimitCalls != 250 {
		t.Fatalf("limit calls = %#v, want 250", usage.LimitCalls)
	}
}

func TestFirecrawlUsageFailureDoesNotTripSearchBreaker(t *testing.T) {
	t.Setenv("NOLE_RETRY_MAX_ATTEMPTS", "1")
	breaker := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Hour})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/team/credit-usage":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"quota endpoint throttled"}`))
		case "/search":
			_ = json.NewEncoder(w).Encode(firecrawlSearchResponse{
				Success: boolPtr(true),
				Data:    firecrawlSearchData{Web: []firecrawlSearchWebResult{{Title: "Nólë", URL: "https://example.com/nole", Description: "router"}}},
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
