package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/mark3labs/mcp-go/server"
)

// callToolText invokes any tool via HandleMessage and returns the first text
// content item (the MarshalIndent JSON the handler produced).
func callToolText(t *testing.T, srv *server.MCPServer, ctx context.Context, name string, args map[string]any) string {
	t.Helper()
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		t.Fatalf("marshal %s request: %v", name, err)
	}
	raw := srv.HandleMessage(ctx, json.RawMessage(msg))
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal %s result: %v", name, err)
	}
	var env struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("unmarshal %s envelope: %v (%s)", name, err, b)
	}
	if len(env.Result.Content) == 0 {
		t.Fatalf("tool %q returned no content: %s", name, b)
	}
	return env.Result.Content[0].Text
}

func TestMCPSearchTraceOptIn(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	txt := callToolText(t, srv, context.Background(), "search",
		map[string]any{"query": "hello", "task": "general", "limit": 3})
	var resp core.SearchResponse
	if err := json.Unmarshal([]byte(txt), &resp); err != nil {
		t.Fatalf("decode search: %v (%s)", err, txt)
	}
	if len(resp.RouteTrace) != 0 {
		t.Fatalf("default search must omit route_trace, got %d entries", len(resp.RouteTrace))
	}
	if resp.RoutingInsight == "" {
		t.Fatalf("routing_insight must always be present even when route_trace is omitted")
	}

	txt2 := callToolText(t, srv, context.Background(), "search",
		map[string]any{"query": "hello", "task": "general", "limit": 3, "include_trace": true})
	var resp2 core.SearchResponse
	if err := json.Unmarshal([]byte(txt2), &resp2); err != nil {
		t.Fatalf("decode search (trace): %v", err)
	}
	if len(resp2.RouteTrace) == 0 {
		t.Fatalf("include_trace:true must include route_trace")
	}
}

func TestMCPResearchToolReturnsEvidenceNoSummary(t *testing.T) {
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	if !callToolsList(t, srv)["research"] {
		t.Fatalf("research tool must be advertised unconditionally")
	}
	txt := callToolText(t, srv, context.Background(), "research",
		map[string]any{"question": "what is model context protocol", "max_steps": 1})
	var m map[string]any
	if err := json.Unmarshal([]byte(txt), &m); err != nil {
		t.Fatalf("decode research: %v (%s)", err, txt)
	}
	if _, ok := m["summary"]; ok {
		t.Fatalf("research output must carry NO summary key: %s", txt)
	}
	for _, want := range []string{"sources", "extracts", "providers_used"} {
		if _, ok := m[want]; !ok {
			t.Fatalf("research output missing %q key: %s", want, txt)
		}
	}
}

func TestMCPSearchAndExtractGatedAndTraceOptIn(t *testing.T) {
	// Absent without an extract-capable configuration.
	srvNo := newTestMCPServerWithProviders(t, mock.New("mock"))
	if callToolsList(t, srvNo)["search_and_extract"] {
		t.Fatalf("search_and_extract must be absent when no extract provider is configured")
	}

	// Present once an extract provider key is configured.
	t.Setenv("TAVILY_API_KEY", "test-key")
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	if !callToolsList(t, srv)["search_and_extract"] {
		t.Fatalf("search_and_extract must be advertised when an extract provider is configured")
	}

	txt := callToolText(t, srv, context.Background(), "search_and_extract",
		map[string]any{"query": "hello", "task": "general", "extract_top": 1})
	var resp core.SearchAndExtractResponse
	if err := json.Unmarshal([]byte(txt), &resp); err != nil {
		t.Fatalf("decode search_and_extract: %v (%s)", err, txt)
	}
	if len(resp.Search.Results) == 0 {
		t.Fatalf("expected search results in search_and_extract")
	}
	// An extract was attempted (success or recorded error, independent of network).
	if len(resp.Extracts)+len(resp.ExtractErrors) == 0 {
		t.Fatalf("expected at least one extract attempt")
	}
	// Trace omitted by default on both halves.
	if len(resp.Search.RouteTrace) != 0 {
		t.Fatalf("default must omit the search route_trace")
	}
	for _, e := range resp.Extracts {
		if len(e.RouteTrace) != 0 {
			t.Fatalf("default must omit each extract route_trace")
		}
	}
}
