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
// core.SearchResponse. The base context is context.Background() (no injected
// session), which exercises the stdio-default path.
func callSearch(t *testing.T, srv *server.MCPServer, query string) core.SearchResponse {
	t.Helper()
	return callSearchWithContext(t, srv, context.Background(), query)
}

// callSearchWithSession invokes the "search" tool as if from a specific HTTP
// MCP session. It creates an InProcessSession with the given sessionID,
// injects it into the context via s.WithContext, then calls HandleMessage.
// This exercises the per-session tip-emission path used in nole serve --mcp.
func callSearchWithSession(t *testing.T, srv *server.MCPServer, sessionID string, query string) core.SearchResponse {
	t.Helper()
	sess := server.NewInProcessSession(sessionID, nil)
	ctx := srv.WithContext(context.Background(), sess)
	return callSearchWithContext(t, srv, ctx, query)
}

// callSearchEphemeral invokes the "search" tool simulating an ephemeral HTTP
// request (no Mcp-Session-Id header). It sets EphemeralCtxKey=true in the
// context but does NOT inject a session, which is the exact behaviour of
// internal/cli/http.go's httpBuildContext when the header is absent.
func callSearchEphemeral(t *testing.T, srv *server.MCPServer, query string) core.SearchResponse {
	t.Helper()
	ctx := context.WithValue(context.Background(), EphemeralCtxKey{}, true)
	return callSearchWithContext(t, srv, ctx, query)
}

// callSearchWithContext is the underlying implementation: it marshals the
// tools/call request, calls HandleMessage with the provided context, and
// unmarshals the SearchResponse from the JSON-RPC envelope.
func callSearchWithContext(t *testing.T, srv *server.MCPServer, ctx context.Context, query string) core.SearchResponse {
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

	raw := srv.HandleMessage(ctx, json.RawMessage(msg))

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

// TestMCPSearchTipPerSession verifies that in HTTP MCP mode (nole serve --mcp)
// each distinct client session independently receives the setup_tip on their
// first search, even though they share a single server process. This is the
// core correctness guarantee added by the per-session tip-state map: session A
// consuming the tip must not suppress it for session B.
func TestMCPSearchTipPerSession(t *testing.T) {
	// Clear BYOK env vars so BuildSetupTip fires (no configured providers).
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	// Session A: first call should get the tip.
	aFirst := callSearchWithSession(t, srv, "session-A", "session A first query")
	if aFirst.SetupTip == nil {
		t.Fatal("session A first call: expected setup_tip, got nil")
	}

	// Session A: subsequent call must NOT get the tip again.
	aSecond := callSearchWithSession(t, srv, "session-A", "session A second query")
	if aSecond.SetupTip != nil {
		t.Errorf("session A second call: setup_tip should be nil after first emission, got %+v", aSecond.SetupTip)
	}

	// Session B: a different client connecting to the same server process
	// must still receive the tip on its own first call, even though session A
	// already consumed it. Before this fix, tipState.emitted was a single
	// bool and session B would be silently suppressed.
	bFirst := callSearchWithSession(t, srv, "session-B", "session B first query")
	if bFirst.SetupTip == nil {
		t.Fatal("session B first call: expected setup_tip on fresh session, got nil (HTTP MCP multi-client regression)")
	}

	// Session B: second call should be suppressed for B as well.
	bSecond := callSearchWithSession(t, srv, "session-B", "session B second query")
	if bSecond.SetupTip != nil {
		t.Errorf("session B second call: setup_tip should be nil, got %+v", bSecond.SetupTip)
	}

	// Session C: a third independent client also gets the tip on its first call.
	cFirst := callSearchWithSession(t, srv, "session-C", "session C first query")
	if cFirst.SetupTip == nil {
		t.Fatal("session C first call: expected setup_tip on fresh session, got nil")
	}
}

// TestMCPSearchTipEphemeralAlwaysEmits verifies that ephemeral HTTP requests
// (client omitted Mcp-Session-Id, signaled by EphemeralCtxKey in the context)
// always receive the setup_tip. The tipState map is bypassed entirely, so each
// request is treated as a fresh session and memory cannot grow unbounded from
// stateless client traffic.
//
// Note: ephemerality is now determined by a context value set at HTTP dispatch
// time (internal/cli/http.go), not by any string prefix in the session ID.
// Passing an "http-ephemeral-..." ID via a session injection no longer triggers
// the ephemeral path — only the context flag does.
func TestMCPSearchTipEphemeralAlwaysEmits(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServerWithProviders(t, mock.New("mock"))
	for i := 0; i < 5; i++ {
		// Use the ephemeral context path (no session, EphemeralCtxKey=true).
		resp := callSearchEphemeral(t, srv, "q")
		if resp.SetupTip == nil {
			t.Errorf("ephemeral request #%d: expected tip, got nil", i)
		}
	}
}

// TestMCPSearchTipPersistentEmitsOnce verifies that a client-supplied session
// ID gets the setup_tip on its first call and suppression on subsequent calls.
// This is the standard once-per-session behaviour for persistent HTTP MCP sessions.
//
// We also verify that a session ID that would previously have been treated as
// "ephemeral" due to the old "http-ephemeral-" prefix check is now treated as
// persistent when injected via a real session (not via EphemeralCtxKey). This
// guards against the regression described in P2 Issue 1: if a client had pinned
// an "http-ephemeral-..." ID echoed by an older server, the new server must
// deduplicate it correctly.
func TestMCPSearchTipPersistentEmitsOnce(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	// Standard client-supplied ID.
	first := callSearchWithSession(t, srv, "client-pinned-abc", "q1")
	if first.SetupTip == nil {
		t.Fatal("first call missing tip")
	}
	second := callSearchWithSession(t, srv, "client-pinned-abc", "q2")
	if second.SetupTip != nil {
		t.Errorf("second call should have suppressed tip, got %+v", second.SetupTip)
	}

	// Regression guard: an "http-ephemeral-..." ID passed via session injection
	// (not via EphemeralCtxKey) is treated as a persistent session and deduplicated.
	// Before this fix, the prefix string check would have always emitted the tip.
	prefixFirst := callSearchWithSession(t, srv, "http-ephemeral-fake-pinned", "q3")
	if prefixFirst.SetupTip == nil {
		t.Fatal("first call with legacy-prefixed pinned ID: expected tip, got nil")
	}
	prefixSecond := callSearchWithSession(t, srv, "http-ephemeral-fake-pinned", "q4")
	if prefixSecond.SetupTip != nil {
		t.Errorf("second call with legacy-prefixed pinned ID: tip should be suppressed (not ephemeral), got %+v", prefixSecond.SetupTip)
	}
}

// TestMCPSearchTipMapBoundAtCap verifies the bounded-map policy (Change B):
// once tipStateMaxEntries unique session IDs have been recorded, new unseen IDs
// are emitted but not recorded (fail-open). Already-recorded IDs still get the
// correct once-per-session behaviour.
func TestMCPSearchTipMapBoundAtCap(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	srv := newTestMCPServerWithProviders(t, mock.New("mock"))

	// Fill the map to exactly tipStateMaxEntries using unique IDs. Each first
	// call must emit the tip; a second call with the same ID must suppress it.
	for i := 0; i < tipStateMaxEntries; i++ {
		id := fmt.Sprintf("session-%d", i)
		first := callSearchWithSession(t, srv, id, "q-first")
		if first.SetupTip == nil {
			t.Fatalf("session %q first call: expected tip, got nil (i=%d)", id, i)
		}
	}

	// The map is now at cap. A brand-new session ID must still receive the tip
	// (fail-open) even though nothing is recorded for it.
	newID := "session-at-cap"
	capFirst := callSearchWithSession(t, srv, newID, "q-cap-first")
	if capFirst.SetupTip == nil {
		t.Fatal("first call at cap: expected tip (fail-open), got nil")
	}

	// Because the cap-overflow call was NOT recorded, a second call with the
	// same ID also gets the tip (no deduplication once at cap). This is the
	// documented fail-open policy; clients who care about deduplication should
	// use stable session IDs that pre-date the cap.
	capSecond := callSearchWithSession(t, srv, newID, "q-cap-second")
	if capSecond.SetupTip == nil {
		t.Fatal("second call at cap (not recorded): expected tip (fail-open), got nil")
	}

	// An ID that was recorded BEFORE the cap was reached must still suppress.
	alreadyRecordedID := "session-0"
	suppressed := callSearchWithSession(t, srv, alreadyRecordedID, "q-suppressed")
	if suppressed.SetupTip != nil {
		t.Errorf("already-recorded session after cap: tip should be suppressed, got %+v", suppressed.SetupTip)
	}
}

// Compile-time check: ensure mcp.CallToolRequest is importable via this
// package's test file (avoids silent import-path drift).
var _ mcp.CallToolRequest
