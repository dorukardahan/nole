package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newTestMCPServerWithProviders builds an MCP server backed by the supplied
// providers. The caller controls which provider names appear in the registry;
// names that match BYOK provider names ("brave", "tavily", "firecrawl") are
// counted as configured when those mock providers report Available=true.
//
// The route matrix routes all tasks to "mock". Each registered provider gets a
// keyless-free ledger entry so the quota gate does not block test calls.
func newTestMCPServerWithProviders(t *testing.T, providers ...core.Provider) *server.MCPServer {
	t.Helper()
	registry := core.NewRegistry()
	ledger := core.NewMemoryQuotaLedger()
	for _, p := range providers {
		if err := registry.Register(p); err != nil {
			t.Fatalf("register provider %q: %v", p.Name(), err)
		}
		// Seed a keyless-free entry so the quota gate allows calls.
		ledger.Set(core.QuotaEntry{
			Provider:   p.Name(),
			CostClass:  core.CostClassKeylessFree,
			KeylessFree: true,
		})
	}
	matrix := core.RouteMatrix{
		core.TaskGeneral: {"mock"},
		core.TaskExtract: {"mock"},
	}
	svc := core.NewService(registry, ledger, matrix)
	return New(svc)
}

// callSearch invokes the "search" tool on srv via the mark3labs/mcp-go
// in-process HandleMessage API and unmarshals the first content item as a
// core.SearchResponse.
func callSearch(t *testing.T, srv *server.MCPServer, query string) core.SearchResponse {
	t.Helper()

	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search",
			"arguments": map[string]any{
				"query": query,
				"task":  "general",
				"limit": 3,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal search request: %v", err)
	}

	raw := srv.HandleMessage(context.Background(), json.RawMessage(msg))

	// raw is a mcp.JSONRPCResponse or mcp.JSONRPCError.
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal HandleMessage result: %v", err)
	}

	// Unwrap the JSON-RPC envelope to get at the CallToolResult.
	var envelope struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				// The search handler produces json.MarshalIndent output and
				// passes it to mcp.NewToolResultText. mcp-go then re-encodes
				// that string into JSON, so the Text field arrives here as a
				// JSON-quoted string (not a raw JSON object).
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("unmarshal JSON-RPC envelope: %v\nraw: %s", err, b)
	}
	if envelope.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.IsError {
		t.Fatalf("tool returned isError=true; content: %s", b)
	}
	if len(envelope.Result.Content) == 0 {
		t.Fatalf("tool returned empty content; raw: %s", b)
	}

	// Text is the plain string returned by mcp.NewToolResultText — it is
	// already the JSON-serialised SearchResponse, so unmarshal it directly.
	var resp core.SearchResponse
	if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal SearchResponse from tool text: %v\ntext: %s", err, envelope.Result.Content[0].Text)
	}
	return resp
}

// TestMCPSearchTipEmittedOncePerSession verifies that:
//   - the first search on a new MCP server includes a non-nil SetupTip when
//     BYOK keys are absent from the registry;
//   - subsequent searches on the SAME server omit the tip.
func TestMCPSearchTipEmittedOncePerSession(t *testing.T) {
	// Register only a plain "mock" provider — not named after any BYOK key, so
	// ProviderStatus sees no configured BYOK providers and BuildSetupTip fires.
	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	first := callSearch(t, srv, "what is mcp")
	if first.SetupTip == nil {
		t.Fatal("first search response: expected setup_tip, got nil")
	}
	if first.SetupTip.Summary == "" {
		t.Error("first search response: setup_tip.summary is empty")
	}
	if first.SetupTip.SeeAlso == "" {
		t.Error("first search response: setup_tip.see_also is empty")
	}

	second := callSearch(t, srv, "another query")
	if second.SetupTip != nil {
		t.Errorf("second search response: setup_tip should be nil on subsequent calls, got %+v", second.SetupTip)
	}

	// A third call should also omit the tip.
	third := callSearch(t, srv, "yet another query")
	if third.SetupTip != nil {
		t.Errorf("third search response: setup_tip should be nil, got %+v", third.SetupTip)
	}
}

// TestMCPSearchTipOmittedWhenAllBYOKConfigured verifies that setup_tip is
// absent from the first search when all BYOK providers are considered
// configured. We achieve this by registering mock providers whose names match
// the canonical BYOK names ("brave", "tavily", "firecrawl"), so ProviderStatus
// marks all of them as configured=true and BuildSetupTip returns nil.
func TestMCPSearchTipOmittedWhenAllBYOKConfigured(t *testing.T) {
	// Named "mock" for the actual route, plus the BYOK-named mocks for status
	// detection. All are available=true.
	srv := newTestMCPServerWithProviders(t,
		mock.New("mock"),
		mock.New("brave"),
		mock.New("tavily"),
		mock.New("firecrawl"),
	)

	resp := callSearch(t, srv, "anything")
	if resp.SetupTip != nil {
		t.Errorf("setup_tip should be nil when all BYOK keys are configured, got %+v", resp.SetupTip)
	}
}

// TestMCPSearchTipNewServerNewSession verifies that a fresh MCP server
// (simulating a new process / new session) emits the tip again on its first
// search, even after a previous server already emitted once.
func TestMCPSearchTipNewServerNewSession(t *testing.T) {
	srv1 := newTestMCPServerWithProviders(t, mock.New("mock"))
	first := callSearch(t, srv1, "session one query")
	if first.SetupTip == nil {
		t.Fatal("srv1 first call: expected setup_tip, got nil")
	}
	second := callSearch(t, srv1, "session one query 2")
	if second.SetupTip != nil {
		t.Errorf("srv1 second call: expected no tip, got %+v", second.SetupTip)
	}

	// New server = new tipEmitted flag = tip fires again.
	srv2 := newTestMCPServerWithProviders(t, mock.New("mock"))
	newSession := callSearch(t, srv2, "session two query")
	if newSession.SetupTip == nil {
		t.Fatal("srv2 first call: expected setup_tip on fresh server, got nil")
	}
}

// callToolsList sends a tools/list JSON-RPC request to the server and returns
// the set of tool names that are advertised.
func callToolsList(t *testing.T, srv *server.MCPServer) map[string]bool {
	t.Helper()

	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal tools/list request: %v", err)
	}

	raw := srv.HandleMessage(context.Background(), json.RawMessage(msg))
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal HandleMessage result: %v", err)
	}

	var envelope struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("unmarshal tools/list envelope: %v\nraw: %s", err, b)
	}
	if envelope.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}

	names := make(map[string]bool, len(envelope.Result.Tools))
	for _, tool := range envelope.Result.Tools {
		names[tool.Name] = true
	}
	return names
}

// TestMCPExtractToolHiddenWhenNoExtractCapableKey verifies that the extract
// tool is NOT advertised when only a non-extract-capable provider key is set.
// Brave has SupportsExtract=false, so the extract tool must be hidden.
func TestMCPExtractToolHiddenWhenNoExtractCapableKey(t *testing.T) {
	// Only brave key set; brave has no extract capability.
	t.Setenv("BRAVE_API_KEY", "fake-brave-key")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	if HasExtractCapableConfigured() {
		t.Fatal("with only BRAVE_API_KEY set, HasExtractCapableConfigured must be false")
	}

	// Also verify at the server level: the extract tool must not appear.
	srv := newTestMCPServerWithProviders(t, mock.New("mock"), mock.New("brave"))
	tools := callToolsList(t, srv)
	if tools["extract"] {
		t.Error("extract tool should not be advertised when no extract-capable key is configured")
	}
	if !tools["search"] {
		t.Error("search tool should always be advertised")
	}
}

// TestMCPExtractToolPresentWhenExtractCapableKeyExists verifies that the
// extract tool IS advertised when TAVILY_API_KEY is set (Tavily supports extract).
func TestMCPExtractToolPresentWhenExtractCapableKeyExists(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "fake-tavily-key")
	t.Setenv("FIRECRAWL_API_KEY", "")

	if !HasExtractCapableConfigured() {
		t.Fatal("with TAVILY_API_KEY set, HasExtractCapableConfigured must be true")
	}

	// Server-level: extract must appear in tools/list.
	srv := newTestMCPServerWithProviders(t, mock.New("mock"), mock.New("tavily"))
	tools := callToolsList(t, srv)
	if !tools["extract"] {
		t.Error("extract tool should be advertised when TAVILY_API_KEY is configured")
	}
}

// TestMCPExtractToolPresentWithFirecrawlOnly verifies that the extract tool IS
// advertised when only FIRECRAWL_API_KEY is set (Firecrawl supports extract).
func TestMCPExtractToolPresentWithFirecrawlOnly(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "fake-fc-key")

	if !HasExtractCapableConfigured() {
		t.Fatal("with FIRECRAWL_API_KEY set, HasExtractCapableConfigured must be true")
	}

	// Server-level: extract must appear in tools/list.
	srv := newTestMCPServerWithProviders(t, mock.New("mock"), mock.New("firecrawl"))
	tools := callToolsList(t, srv)
	if !tools["extract"] {
		t.Error("extract tool should be advertised when FIRECRAWL_API_KEY is configured")
	}
}

// TestMCPSearchTipConcurrencySafe drives the search handler from multiple
// goroutines and verifies (a) no panic or data race and (b) at most one tip
// emitted across all concurrent calls. The race detector (go test -race) will
// catch any unsynchronized writes to tipState that survive the mutex.
func TestMCPSearchTipConcurrencySafe(t *testing.T) {
	// Clear all BYOK env vars so BuildSetupTip fires (no configured providers).
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	const N = 50
	var tipSeen int32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := callSearch(t, srv, fmt.Sprintf("concurrent query %d", i))
			if resp.SetupTip != nil {
				atomic.AddInt32(&tipSeen, 1)
			}
		}(i)
	}
	wg.Wait()

	if tipSeen != 1 {
		t.Errorf("expected exactly 1 tip across %d concurrent calls, got %d", N, tipSeen)
	}
}

// Compile-time check: ensure mcp.CallToolRequest is importable via this
// package's test file (avoids silent import-path drift).
var _ mcp.CallToolRequest
