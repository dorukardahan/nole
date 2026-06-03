package cli

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// TestStableRESTSurface pins the REST/`nole serve` contract that v1.4.0 declares
// stable: the route set, the error-envelope field shape, and the status-code
// mapping. A removed/renamed route, a renamed/added envelope field, or a changed
// status code fails here so the change is conscious and documented (mirrors the
// MCP surface lock). Auth + 402 behaviours are pinned in http_auth_test.go.
func TestStableRESTSurface(t *testing.T) {
	h := newTestHTTPHandler(t) // token == "" -> withAuth passes through

	// 1. Frozen ROUTE SET. Each must respond (NOT 404). Removing/renaming any of
	// these stable routes is BREAKING.
	routes := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/health", ""},
		{http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`},
		{http.MethodGet, "/api/providers", ""},
		{http.MethodGet, "/api/budget", ""},
		{http.MethodPost, "/api/search", `{"query":"x"}`},
		{http.MethodPost, "/api/extract", `{"url":"https://example.com"}`},
		{http.MethodPost, "/api/search_and_extract", `{"query":"x"}`},
		{http.MethodPost, "/api/research", `{"question":"x"}`},
	}
	for _, rt := range routes {
		w := h.testServe(rt.method, rt.path, "", rt.body)
		if w.Code == http.StatusNotFound {
			t.Errorf("frozen REST route %s %s returned 404 — removing/renaming a stable route is BREAKING; update docs/STABILITY.md + this lock", rt.method, rt.path)
		}
	}

	// 2. Unknown path 404s.
	if w := h.testServe(http.MethodGet, "/api/nope", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", w.Code)
	}

	// 3. Method gating: POST-only routes reject GET with 405.
	if w := h.testServe(http.MethodGet, "/api/search", "", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/search = %d, want 405", w.Code)
	}

	// 4. Frozen ERROR-ENVELOPE shape on a service error. Force NoFreeQuotaError so
	// we exercise the full envelope (operation/error/route/routing_insight), and
	// assert NO top-level key outside the frozen set appears.
	errH := errorHandlerNoQuota(t)
	w := errH.testServe(http.MethodPost, "/api/search", "", `{"query":"x"}`)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("forced no-free-quota: status = %d, want 402", w.Code)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("error envelope not JSON: %v", err)
	}
	if _, ok := env["operation"]; !ok {
		t.Error("error envelope missing frozen key: operation")
	}
	if _, ok := env["error"]; !ok {
		t.Error("error envelope missing frozen key: error")
	}
	allowed := map[string]bool{"operation": true, "error": true, "route": true, "routing_insight": true, "route_trace": true}
	var unexpected []string
	for k := range env {
		if !allowed[k] {
			unexpected = append(unexpected, k)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("error envelope has UNEXPECTED top-level key(s) %v — adding/renaming an envelope field is a surface change; document it + update this lock", unexpected)
	}
}

// errorHandlerNoQuota builds a handler whose only route provider is quota-exhausted,
// so Search returns NoFreeQuotaError (→ 402).
func errorHandlerNoQuota(t *testing.T) *httpHandler {
	t.Helper()
	registry := core.NewRegistry()
	_ = registry.Register(mockProviderNamed("tavily"))
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "tavily", FreeRemaining: 0})
	svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskGeneral: {"tavily"}})
	return &httpHandler{svc: svc, mcp: buildMCPServer(svc)}
}
