package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// authedTestHandler builds a test handler with a configured bearer token, so the
// withAuth middleware enforces it.
func authedTestHandler(t *testing.T, token string) *httpHandler {
	t.Helper()
	h := newTestHTTPHandler(t)
	h.token = token
	return h
}

// doReq runs a request through the FULL server handler (withAuth(buildMux)).
func (h *httpHandler) testServe(method, path, auth string, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.withAuth(h.buildMux()).ServeHTTP(w, r)
	return w
}

func TestAuthRequiredWhenTokenConfigured(t *testing.T) {
	h := authedTestHandler(t, "s3cr3t-token")

	// No Authorization header -> 401 on a key-bearing endpoint.
	if w := h.testServe(http.MethodGet, "/api/providers", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no token, on /api/providers = %d, want 401", w.Code)
	}
	// Wrong token -> 401.
	if w := h.testServe(http.MethodGet, "/api/providers", "Bearer wrong", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token, on /api/providers = %d, want 401", w.Code)
	}
	// Correct token -> 200.
	if w := h.testServe(http.MethodGet, "/api/providers", "Bearer s3cr3t-token", ""); w.Code != http.StatusOK {
		t.Errorf("correct token, on /api/providers = %d, want 200", w.Code)
	}
	// Case-insensitive auth scheme (RFC 9110 §11.1): lowercase "bearer" -> 200.
	if w := h.testServe(http.MethodGet, "/api/providers", "bearer s3cr3t-token", ""); w.Code != http.StatusOK {
		t.Errorf("lowercase bearer scheme = %d, want 200 (scheme is case-insensitive)", w.Code)
	}
	// 401 body is a sanitized envelope and must NOT echo the configured token.
	w := h.testServe(http.MethodPost, "/api/search", "", `{"query":"x"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/search without token = %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "s3cr3t-token") {
		t.Fatalf("401 body leaked the configured token: %s", w.Body.String())
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("401 should set WWW-Authenticate")
	}
}

func TestAuthHealthAlwaysOpen(t *testing.T) {
	// /health must stay reachable WITHOUT a token (readiness probes), even when a
	// token is configured — it exposes no keys/secrets.
	h := authedTestHandler(t, "s3cr3t-token")
	if w := h.testServe(http.MethodGet, "/health", "", ""); w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("/health without token = %d, want 200 or 503 (always reachable)", w.Code)
	}
}

func TestAuthOpenWhenNoTokenConfigured(t *testing.T) {
	// No token configured (the loopback-default case) -> endpoints open, as before.
	h := newTestHTTPHandler(t) // token == ""
	if w := h.testServe(http.MethodGet, "/api/providers", "", ""); w.Code != http.StatusOK {
		t.Errorf("no token configured: /api/providers = %d, want 200 (open)", w.Code)
	}
}

func TestServeSecurityPreflightFailsClosed(t *testing.T) {
	cases := []struct {
		addr    string
		token   string
		wantErr bool
	}{
		{"127.0.0.1:8765", "", false},       // loopback, no token -> OK
		{"localhost:8765", "", false},       // loopback, no token -> OK
		{"0.0.0.0:8765", "", true},          // exposed, no token -> REFUSE
		{":8765", "", true},                 // all interfaces, no token -> REFUSE
		{"192.168.1.10:8765", "", true},     // exposed, no token -> REFUSE
		{"0.0.0.0:8765", "tok", false},      // exposed WITH token -> OK
		{"192.168.1.10:8765", "tok", false}, // exposed WITH token -> OK
	}
	for _, c := range cases {
		err := serveSecurityPreflight(c.addr, c.token)
		if c.wantErr && err == nil {
			t.Errorf("serveSecurityPreflight(%q, token=%q) = nil, want refuse-to-start error", c.addr, c.token)
		}
		if !c.wantErr && err != nil {
			t.Errorf("serveSecurityPreflight(%q, token=%q) = %v, want nil", c.addr, c.token, err)
		}
	}
	// The refuse error must name NOLE_SERVE_TOKEN and not leak any token value.
	err := serveSecurityPreflight("0.0.0.0:8765", "")
	if err == nil || !strings.Contains(err.Error(), "NOLE_SERVE_TOKEN") {
		t.Fatalf("refuse error should mention NOLE_SERVE_TOKEN, got: %v", err)
	}
}

func TestRESTNoFreeQuotaMapsTo402(t *testing.T) {
	// When the service returns NoFreeQuotaError (all providers exhausted/blocked),
	// REST must answer 402 Payment Required, not 500.
	registry := core.NewRegistry()
	_ = registry.Register(mockProviderNamed("tavily"))
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "tavily", FreeRemaining: 0}) // exhausted
	svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskGeneral: {"tavily"}})
	h := &httpHandler{svc: svc, mcp: buildMCPServer(svc)}

	w := h.testServe(http.MethodPost, "/api/search", "", `{"query":"x"}`)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("no-free-quota: /api/search = %d, want 402; body=%s", w.Code, w.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("402 body not JSON: %v (%s)", err, w.Body.String())
	}
	if env["operation"] != "search" || env["error"] == nil {
		t.Errorf("402 envelope should carry operation+error: %v", env)
	}
}

func mockProviderNamed(name string) core.Provider {
	return mockSearchProvider{name: name}
}

type mockSearchProvider struct{ name string }

func (m mockSearchProvider) Name() string { return m.name }
func (m mockSearchProvider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}
func (m mockSearchProvider) Search(context.Context, core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{Provider: m.name, Results: []core.SearchResult{{Title: "r", URL: "https://e.com", Provider: m.name}}}, nil
}
func (m mockSearchProvider) Extract(context.Context, core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{Provider: m.name, Content: "c"}, nil
}
func (m mockSearchProvider) Status(context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: m.name, Available: true}
}
