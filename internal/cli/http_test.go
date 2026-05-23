package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/mark3labs/mcp-go/server"
)

// newTestHTTPHandler builds an httpHandler backed by a mock registry that has
// no BYOK-named providers, so ProviderStatus always returns setup suggestions
// and BuildSetupTip fires on every first-per-session search call.
func newTestHTTPHandler(t *testing.T) *httpHandler {
	t.Helper()
	registry := core.NewRegistry()
	ledger := core.NewMemoryQuotaLedger()
	p := mock.New("mock")
	if err := registry.Register(p); err != nil {
		t.Fatalf("register mock provider: %v", err)
	}
	ledger.Set(core.QuotaEntry{
		Provider:    "mock",
		CostClass:   core.CostClassKeylessFree,
		KeylessFree: true,
	})
	matrix := core.RouteMatrix{
		core.TaskGeneral: {"mock"},
		core.TaskExtract: {"mock"},
	}
	svc := core.NewService(registry, ledger, matrix)
	h := &httpHandler{
		svc: svc,
		mcp: buildMCPServer(svc),
	}
	return h
}

// mcpSearchBody builds a minimal tools/call JSON-RPC body for the search tool.
func mcpSearchBody(t *testing.T, query string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
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
		t.Fatalf("marshal MCP request: %v", err)
	}
	return body
}

// postMCP sends one POST /mcp request to h, optionally setting a
// Mcp-Session-Id header (pass "" to omit), and returns the response recorder.
func postMCP(t *testing.T, h *httpHandler, sessionHeader string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sessionHeader != "" {
		req.Header.Set("Mcp-Session-Id", sessionHeader)
	}
	rec := httptest.NewRecorder()
	h.handleMCP(rec, req)
	return rec
}

// decodeSetupTip extracts the setup_tip field (nil-able) from a raw
// tools/call JSON-RPC response body.
func decodeSetupTip(t *testing.T, body []byte) *core.SetupTip {
	t.Helper()
	var envelope struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal JSON-RPC envelope: %v\nbody: %s", err, body)
	}
	if envelope.Error != nil {
		t.Fatalf("JSON-RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.IsError {
		t.Fatalf("tool returned isError=true; body: %s", body)
	}
	if len(envelope.Result.Content) == 0 {
		t.Fatalf("tool returned empty content; body: %s", body)
	}
	var resp core.SearchResponse
	if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal SearchResponse: %v\ntext: %s", err, envelope.Result.Content[0].Text)
	}
	return resp.SetupTip
}

// TestHTTPMCPSessionIDEchoedInResponse verifies that the handler always sets
// the Mcp-Session-Id response header regardless of whether the client supplied
// one in the request.
func TestHTTPMCPSessionIDEchoedInResponse(t *testing.T) {
	h := newTestHTTPHandler(t)
	body := mcpSearchBody(t, "test session echo")

	// Without a client-supplied ID: response must still carry a generated one
	// with the "http-ephemeral-" prefix.
	rec1 := postMCP(t, h, "", body)
	id1 := rec1.Header().Get("Mcp-Session-Id")
	if id1 == "" {
		t.Fatal("expected Mcp-Session-Id header in response, got empty string")
	}
	if !strings.HasPrefix(id1, "http-ephemeral-") {
		t.Errorf("generated session ID %q does not start with 'http-ephemeral-'", id1)
	}

	// With a client-supplied ID: response must echo it back unchanged.
	clientID := "my-stable-session-42"
	rec2 := postMCP(t, h, clientID, body)
	got := rec2.Header().Get("Mcp-Session-Id")
	if got != clientID {
		t.Errorf("echoed session ID = %q, want %q", got, clientID)
	}
}

// TestHTTPMCPNoSessionHeaderGivesFreshSessionPerRequest verifies that two
// requests without Mcp-Session-Id each receive the setup_tip (independent
// sessions). This is the core regression test for the bug where all HTTP
// requests shared the "stdio-default" key: the second request was silently
// suppressed even though it came from a different client.
func TestHTTPMCPNoSessionHeaderGivesFreshSessionPerRequest(t *testing.T) {
	// Clear BYOK env vars so BuildSetupTip fires (no configured providers).
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	h := newTestHTTPHandler(t)

	// First request: no session header → fresh random session → tip expected.
	rec1 := postMCP(t, h, "", mcpSearchBody(t, "request 1"))
	tip1 := decodeSetupTip(t, rec1.Body.Bytes())
	if tip1 == nil {
		t.Fatal("request 1 (no session header): expected setup_tip, got nil")
	}

	// Second request: also no session header → different random session → tip
	// must appear again. Without the fix this was suppressed because both
	// requests collapsed onto "stdio-default".
	rec2 := postMCP(t, h, "", mcpSearchBody(t, "request 2"))
	tip2 := decodeSetupTip(t, rec2.Body.Bytes())
	if tip2 == nil {
		t.Fatal("request 2 (no session header, different client): expected setup_tip, got nil — HTTP session isolation broken")
	}
}

// TestHTTPMCPSameSessionIDSuppressesTipOnSecondRequest verifies that when a
// client sends the same Mcp-Session-Id on two consecutive requests, the tip
// appears only on the first — the correct "once per session" behaviour.
func TestHTTPMCPSameSessionIDSuppressesTipOnSecondRequest(t *testing.T) {
	// Clear BYOK env vars so BuildSetupTip fires (no configured providers).
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	h := newTestHTTPHandler(t)
	pinnedID := "pinned-client-session"

	// First request with pinned ID → tip expected.
	rec1 := postMCP(t, h, pinnedID, mcpSearchBody(t, "session pinned query 1"))
	tip1 := decodeSetupTip(t, rec1.Body.Bytes())
	if tip1 == nil {
		t.Fatal("first request with pinned session: expected setup_tip, got nil")
	}

	// Second request with same pinned ID → tip must be suppressed.
	rec2 := postMCP(t, h, pinnedID, mcpSearchBody(t, "session pinned query 2"))
	tip2 := decodeSetupTip(t, rec2.Body.Bytes())
	if tip2 != nil {
		t.Errorf("second request with same session ID: setup_tip should be nil, got %+v", tip2)
	}

	// Third request also with same ID → still suppressed.
	rec3 := postMCP(t, h, pinnedID, mcpSearchBody(t, "session pinned query 3"))
	tip3 := decodeSetupTip(t, rec3.Body.Bytes())
	if tip3 != nil {
		t.Errorf("third request with same session ID: setup_tip should be nil, got %+v", tip3)
	}
}

// TestHTTPMCPDifferentPinnedSessionsEachGetTip verifies that two different
// clients using distinct pinned session IDs each independently receive the tip
// on their respective first request.
func TestHTTPMCPDifferentPinnedSessionsEachGetTip(t *testing.T) {
	// Clear BYOK env vars so BuildSetupTip fires (no configured providers).
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	h := newTestHTTPHandler(t)

	// Client A, first request.
	tipA1 := decodeSetupTip(t, postMCP(t, h, "client-A", mcpSearchBody(t, "A query 1")).Body.Bytes())
	if tipA1 == nil {
		t.Fatal("client A first request: expected setup_tip, got nil")
	}

	// Client A, second request — tip suppressed.
	tipA2 := decodeSetupTip(t, postMCP(t, h, "client-A", mcpSearchBody(t, "A query 2")).Body.Bytes())
	if tipA2 != nil {
		t.Errorf("client A second request: setup_tip should be nil, got %+v", tipA2)
	}

	// Client B, first request — tip must fire independently.
	tipB1 := decodeSetupTip(t, postMCP(t, h, "client-B", mcpSearchBody(t, "B query 1")).Body.Bytes())
	if tipB1 == nil {
		t.Fatal("client B first request: expected setup_tip on independent session, got nil")
	}

	// Client B, second request — suppressed.
	tipB2 := decodeSetupTip(t, postMCP(t, h, "client-B", mcpSearchBody(t, "B query 2")).Body.Bytes())
	if tipB2 != nil {
		t.Errorf("client B second request: setup_tip should be nil, got %+v", tipB2)
	}
}

// TestHTTPSessionForRequest_SessionIDExtraction tests the session ID extraction
// helper directly, without standing up a full HTTP server.
func TestHTTPSessionForRequest_SessionIDExtraction(t *testing.T) {
	t.Run("no header generates http-ephemeral- prefixed ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		id, sess := httpSessionForRequest(req)
		if !strings.HasPrefix(id, "http-ephemeral-") {
			t.Errorf("generated ID %q does not start with 'http-ephemeral-'", id)
		}
		if sess.SessionID() != id {
			t.Errorf("sess.SessionID() = %q, want %q", sess.SessionID(), id)
		}
	})

	t.Run("client header is used as-is", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "my-client-id")
		id, sess := httpSessionForRequest(req)
		if id != "my-client-id" {
			t.Errorf("id = %q, want %q", id, "my-client-id")
		}
		if sess.SessionID() != "my-client-id" {
			t.Errorf("sess.SessionID() = %q, want %q", sess.SessionID(), "my-client-id")
		}
	})

	t.Run("two requests without header get distinct IDs", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		id1, _ := httpSessionForRequest(req1)
		id2, _ := httpSessionForRequest(req2)
		if id1 == id2 {
			t.Errorf("two requests without header produced the same ID: %q", id1)
		}
	})
}

// TestHTTPSessionForRequest_ContextInjection confirms that WithContext puts a
// session into the context that ClientSessionFromContext can retrieve.
func TestHTTPSessionForRequest_ContextInjection(t *testing.T) {
	h := newTestHTTPHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "inject-test")
	_, sess := httpSessionForRequest(req)
	ctx := h.mcp.WithContext(context.Background(), sess)
	got := server.ClientSessionFromContext(ctx)
	if got == nil {
		t.Fatal("ClientSessionFromContext returned nil after WithContext injection")
	}
	if got.SessionID() != "inject-test" {
		t.Errorf("session ID = %q, want %q", got.SessionID(), "inject-test")
	}
}
