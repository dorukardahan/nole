package firecrawl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

type capturedOpenClawCall struct {
	command string
	args    []string
}

func TestOpenClawBridgeSearchUsesGatewayToolAndMapsResults(t *testing.T) {
	var call capturedOpenClawCall
	runner := func(_ context.Context, command string, args ...string) ([]byte, error) {
		call = capturedOpenClawCall{command: command, args: append([]string(nil), args...)}
		return []byte(`{
			"ok": true,
			"toolName": "web_search",
			"output": {
				"content": [{"type":"text","text":"ignored model-facing copy"}],
				"details": {
					"query": "nole",
					"provider": "firecrawl-free",
					"results": [{
						"title": "Nole",
						"url": "https://example.com/nole",
						"description": "A result",
						"published": "2026-07-01"
					}]
				}
			}
		}`), nil
	}
	p := New(
		WithOpenClawBridge("/usr/bin/openclaw"),
		WithOpenClawCommandRunner(runner),
	)

	resp, err := p.Search(context.Background(), core.SearchRequest{
		Query: "nole openclaw",
		Task:  core.TaskNews,
		Limit: 20,
		Options: core.SearchOptions{
			Country:   "tr",
			Freshness: "pd",
		},
	})
	if err != nil {
		t.Fatalf("bridge search failed: %v", err)
	}
	if call.command != "/usr/bin/openclaw" {
		t.Fatalf("command = %q", call.command)
	}
	params := decodeToolsInvokeParams(t, call.args)
	if params.Name != "web_search" {
		t.Fatalf("tool name = %q", params.Name)
	}
	if params.Args["query"] != "nole openclaw" || params.Args["count"] != float64(10) {
		t.Fatalf("unexpected web_search args: %#v", params.Args)
	}
	if _, ok := params.Args["country"]; ok {
		t.Fatalf("unsupported country filter forwarded to Firecrawl host: %#v", params.Args)
	}
	if _, ok := params.Args["freshness"]; ok {
		t.Fatalf("unsupported freshness filter forwarded to Firecrawl host: %#v", params.Args)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.com/nole" {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	if resp.Provider != "firecrawl" || resp.Results[0].Provider != "firecrawl" {
		t.Fatalf("provider identity must stay registry-compatible: %+v", resp)
	}
	if resp.Results[0].PublishedAt != "2026-07-01" {
		t.Fatalf("publishedAt not preserved: %+v", resp.Results[0])
	}
}

func TestOpenClawBridgeExtractUsesGatewayToolAndMapsContent(t *testing.T) {
	var call capturedOpenClawCall
	runner := func(_ context.Context, command string, args ...string) ([]byte, error) {
		call = capturedOpenClawCall{command: command, args: append([]string(nil), args...)}
		return []byte(`{
			"ok": true,
			"toolName": "web_fetch",
			"output": {
				"content": [{"type":"text","text":"ignored"}],
				"details": {
					"url": "https://example.com/page",
					"finalUrl": "https://example.com/final",
					"status": 200,
					"title": "Page",
					"extractor": "readability",
					"text": "rendered markdown",
					"externalContent": {"source":"web_fetch"}
				}
			}
		}`), nil
	}
	p := New(
		WithOpenClawBridgeMode("openclaw", OpenClawBridgeFetchOnly),
		WithOpenClawCommandRunner(runner),
	)

	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com/page"})
	if err != nil {
		t.Fatalf("bridge extract failed: %v", err)
	}
	params := decodeToolsInvokeParams(t, call.args)
	if params.Name != "web_fetch" || params.Args["url"] != "https://example.com/page" {
		t.Fatalf("unexpected web_fetch invocation: %+v", params)
	}
	if resp.Provider != "firecrawl" || resp.Content != "rendered markdown" {
		t.Fatalf("unexpected extract response: %+v", resp)
	}
	if resp.Metadata["final_url"] != "https://example.com/final" || resp.Metadata["title"] != "Page" {
		t.Fatalf("metadata mapping lost: %+v", resp.Metadata)
	}
	if resp.Metadata["extractor"] != "readability" || resp.Metadata["host_provider"] != "web_fetch" {
		t.Fatalf("host extractor identity lost: %+v", resp.Metadata)
	}
}

func TestOpenClawFetchOnlyBridgeDoesNotAdvertiseOrInvokeSearch(t *testing.T) {
	called := false
	p := New(
		WithOpenClawBridgeMode("openclaw", OpenClawBridgeFetchOnly),
		WithOpenClawCommandRunner(func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return nil, errors.New("must not run")
		}),
	)
	for _, capability := range p.Capabilities() {
		if capability == core.CapabilitySearch {
			t.Fatalf("fetch-only bridge advertised search: %#v", p.Capabilities())
		}
	}
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil {
		t.Fatal("direct fetch-only search should fail closed")
	}
	if called {
		t.Fatal("fetch-only search invoked OpenClaw CLI")
	}
	if reason := p.Status(context.Background()).Reason; !strings.Contains(reason, "OpenClaw web_fetch bridge") {
		t.Fatalf("fetch-only status is misleading: %q", reason)
	}
}

func TestOpenClawBridgeRejectsUnexpectedSearchProvider(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{
			"ok": true,
			"toolName": "web_search",
			"output": {"details":{"provider":"brave","results":[]}}
		}`), nil
	}
	p := New(WithOpenClawBridge("openclaw"), WithOpenClawCommandRunner(runner))
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"}); err == nil || !strings.Contains(err.Error(), "unexpected OpenClaw web_search provider") {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
}

func TestOpenClawBridgeIsOptInAndDirectAPIPathRemains(t *testing.T) {
	var directCalls, bridgeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"web":[{"title":"Direct","url":"https://example.com/direct","description":"direct"}]}}`))
	}))
	defer srv.Close()
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		bridgeCalls++
		return nil, nil
	}
	p := New(WithBaseURL(srv.URL), WithOpenClawCommandRunner(runner))
	if _, err := p.Search(context.Background(), core.SearchRequest{Query: "direct"}); err != nil {
		t.Fatalf("direct search failed: %v", err)
	}
	if directCalls != 1 || bridgeCalls != 0 {
		t.Fatalf("direct=%d bridge=%d; bridge must be explicit", directCalls, bridgeCalls)
	}
}

func TestOpenClawBridgeProductionRunnerExecutesCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	t.Setenv("FIRECRAWL_API_KEY", "must-not-reach-openclaw")
	t.Setenv("BRAVE_API_KEY", "must-not-reach-openclaw")
	t.Setenv("BRAVE_SEARCH_API_KEY", "must-not-reach-openclaw")
	t.Setenv("TAVILY_API_KEY", "must-not-reach-openclaw")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "gateway-test-token")
	script := filepath.Join(t.TempDir(), "openclaw")
	body := `#!/bin/sh
if [ -n "${FIRECRAWL_API_KEY:-}" ] || [ -n "${BRAVE_API_KEY:-}" ] || [ -n "${BRAVE_SEARCH_API_KEY:-}" ] || [ -n "${TAVILY_API_KEY:-}" ]; then
  exit 9
fi
if [ "${OPENCLAW_GATEWAY_TOKEN:-}" != "gateway-test-token" ]; then
  exit 8
fi
printf '%s' '{"ok":true,"toolName":"web_search","output":{"details":{"provider":"firecrawl-free","results":[{"title":"CLI","url":"https://example.com/cli","description":"from cli"}]}}}'
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatalf("write fake OpenClaw CLI: %v", err)
	}
	p := New(WithOpenClawBridge(script))
	resp, err := p.Search(context.Background(), core.SearchRequest{Query: "nole", Limit: 1})
	if err != nil {
		t.Fatalf("production runner search failed: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "CLI" {
		t.Fatalf("unexpected subprocess response: %+v", resp)
	}
}

func TestOpenClawBridgeSanitizesFailureAndTripsBreaker(t *testing.T) {
	breaker := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Hour})
	calls := 0
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		return nil, errors.New("Bearer private-gateway-token")
	}
	p := New(
		WithOpenClawBridge("openclaw"),
		WithOpenClawCommandRunner(runner),
		WithBreaker(breaker),
	)
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "nole"})
	if err == nil || strings.Contains(err.Error(), "private-gateway-token") || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("bridge error must be sanitized, got %v", err)
	}
	_, err = p.Search(context.Background(), core.SearchRequest{Query: "nole"})
	if !errors.Is(err, providerhttp.ErrCircuitOpen) {
		t.Fatalf("second call should short-circuit, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}
}

func TestOpenClawBridgeCallerCancellationDoesNotTripBreaker(t *testing.T) {
	breaker := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Hour})
	runner := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p := New(
		WithOpenClawBridge("openclaw"),
		WithOpenClawCommandRunner(runner),
		WithBreaker(breaker),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Search(ctx, core.SearchRequest{Query: "nole"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected caller cancellation error, got %v", err)
	}
	if breaker.IsOpen() {
		t.Fatal("caller cancellation must not trip bridge breaker")
	}
}

func TestOpenClawBridgeStatusNamesHostRoute(t *testing.T) {
	p := New(WithOpenClawBridge("openclaw"))
	status := p.Status(context.Background())
	if !status.Available || !strings.Contains(status.Reason, "OpenClaw Firecrawl host bridge") {
		t.Fatalf("unexpected bridge status: %+v", status)
	}
}

type toolsInvokeParamsForTest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func decodeToolsInvokeParams(t *testing.T, args []string) toolsInvokeParamsForTest {
	t.Helper()
	if len(args) < 6 || args[0] != "gateway" || args[1] != "call" || args[2] != "tools.invoke" {
		t.Fatalf("unexpected OpenClaw CLI args: %#v", args)
	}
	var raw string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--params" {
			raw = args[i+1]
			break
		}
	}
	if raw == "" {
		t.Fatalf("--params missing: %#v", args)
	}
	var params toolsInvokeParamsForTest
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatalf("decode --params: %v", err)
	}
	return params
}
