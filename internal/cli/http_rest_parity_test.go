package cli

// REST/MCP/CLI parity + hardening coverage for `nole serve`. These tests close
// the gaps the v1.x REST surface had: they PROVE the error-envelope parity that
// already exists (REST reuses core.Service + buildCLIError + safeerr), exercise
// the previously-untested routes (search_and_extract, research) and the new 400
// `operation` key, and lock the security guarantees end-to-end through the REAL
// buildMux — most importantly that a PROVIDER-originated secret is redacted on
// the live HTTP path (the existing loopback test only proved envelope shape).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// secretErrProvider is a search-capable provider whose Search fails with an error
// embedding fake secrets. Service.Search returns that error verbatim as lastErr
// (service.go:208,269), so it flows through the real handler →
// buildCLIError → safeerr.Message → HTTP body — exercising redaction end-to-end.
type secretErrProvider struct{}

func (secretErrProvider) Name() string { return "secretprov" }
func (secretErrProvider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}
func (secretErrProvider) Search(context.Context, core.SearchRequest) (core.SearchResponse, error) {
	return core.SearchResponse{}, fmt.Errorf("fixture upstream failure: provider echoed token=SECRET Authorization: Bearer deadbeef123 https://private.example/path api_key=KEY999")
}
func (secretErrProvider) Extract(context.Context, core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, fmt.Errorf("secretprov: extract not supported")
}
func (secretErrProvider) Status(context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: "secretprov", Available: true, Capabilities: []core.Capability{core.CapabilitySearch, core.CapabilityStatus}}
}

// newTestHTTPHandlerWith builds a handler backed by exactly the given provider,
// seeded keyless-free (so the cost policy allows it and the provider's own error,
// not a NoFreeQuotaError, is what surfaces), routed for general+extract.
func newTestHTTPHandlerWith(t *testing.T, p core.Provider) *httpHandler {
	t.Helper()
	registry := core.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: p.Name(), CostClass: core.CostClassKeylessFree, KeylessFree: true})
	matrix := core.RouteMatrix{core.TaskGeneral: {p.Name()}, core.TaskExtract: {p.Name()}}
	svc := core.NewService(registry, ledger, matrix)
	return &httpHandler{svc: svc, mcp: buildMCPServer(svc)}
}

// The key gap-closer: a secret that ORIGINATES IN A PROVIDER must be scrubbed
// from the live HTTP error body (not just the envelope-shape, which the existing
// loopback test covers). Drives /api/search through the real buildMux.
func TestRESTSearchProviderErrorRedactsSecretEndToEnd(t *testing.T) {
	h := newTestHTTPHandlerWith(t, secretErrProvider{})
	rec := doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"hello","task":"general"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/search (provider error) = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	var env cliErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Operation != "search" || env.Error == "" {
		t.Fatalf("unexpected error envelope: %#v", env)
	}
	// Non-vacuity: the provider error text propagated (the non-secret marker
	// survives redaction), proving the secret WAS in scope on this path...
	if !strings.Contains(env.Error, "fixture upstream failure") {
		t.Fatalf("provider error did not propagate to the body (test would be vacuous): %q", env.Error)
	}
	// ...and yet every secret token is scrubbed by safeerr.Message.
	raw := rec.Body.String()
	for _, secret := range []string{"SECRET", "Authorization", "Bearer", "deadbeef123", "private.example", "api_key", "KEY999"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("HTTP body leaked provider secret %q: %s", secret, raw)
		}
	}
}

// Locks the 5-field error envelope so REST stays at parity with cliErrorEnvelope/
// toolErrorEnvelope (operation, error, route, routing_insight, route_trace).
func TestRESTSearchErrorEnvelopeShapeParity(t *testing.T) {
	h := newTestHTTPHandlerWith(t, secretErrProvider{})
	rec := doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"x","task":"general"}`))
	var env map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	for _, k := range []string{"operation", "error", "route", "routing_insight", "route_trace"} {
		if _, ok := env[k]; !ok {
			t.Errorf("REST search error envelope missing %q key (parity with CLI/MCP); got keys %v", k, jsonEnvelopeKeys(env))
		}
	}
}

func TestRESTSearchAndExtractHappyPath(t *testing.T) {
	h := newTestHTTPHandler(t) // mock answers search + extract
	rec := doREST(t, h, http.MethodPost, "/api/search_and_extract", []byte(`{"query":"hello","task":"general","limit":2,"extract_top":1}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/search_and_extract = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp core.SearchAndExtractResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search_and_extract response: %v", err)
	}
	if resp.Search.Provider != "mock" || len(resp.Search.Results) == 0 {
		t.Fatalf("unexpected search_and_extract response: %#v", resp.Search)
	}
}

func TestRESTResearchHappyPath(t *testing.T) {
	h := newTestHTTPHandler(t)
	rec := doREST(t, h, http.MethodPost, "/api/research", []byte(`{"question":"what is go","max_steps":2}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/research = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var report core.ResearchReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode research report: %v", err)
	}
}

func TestRESTResearchInvalidOptionsReturns400(t *testing.T) {
	h := newTestHTTPHandler(t)
	rec := doREST(t, h, http.MethodPost, "/api/research", []byte(`{"question":"hello","max_steps":1,"options":{"freshness":"forever"}}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/research invalid options = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("invalid research options Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), "freshness") {
		t.Fatalf("expected invalid field in error body, got %s", rec.Body.String())
	}
}

// Research's error body is intentionally a 2-field {operation,error} (its
// ResearchReport carries no route/trace). Lock that documented divergence so it
// can't silently grow/shrink. This test forces the 500 cancellation path with a
// pre-cancelled request context; caller-controlled invalid options use the 400
// invalid-request envelope tested separately above.
func TestRESTResearchErrorEnvelopeDivergence(t *testing.T) {
	h := newTestHTTPHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/research", strings.NewReader(`{"question":"x","max_steps":1}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.buildMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/research (cancelled) = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode research error: %v", err)
	}
	for _, k := range []string{"operation", "error"} {
		if _, ok := env[k]; !ok {
			t.Errorf("research error missing %q", k)
		}
	}
	for _, k := range []string{"route", "route_trace", "routing_insight"} {
		if _, ok := env[k]; ok {
			t.Errorf("research error unexpectedly has %q (ResearchReport has no route data; shape is documented as divergent)", k)
		}
	}
}

// Research is RESILIENT: when its internal search step fails (here with a
// secret-bearing provider error), it returns a 200 + an empty report rather than
// surfacing the error — and it must NOT leak the swallowed provider secret in
// any field of that report.
func TestRESTResearchResilientNoSecretLeak(t *testing.T) {
	h := newTestHTTPHandlerWith(t, secretErrProvider{})
	rec := doREST(t, h, http.MethodPost, "/api/research", []byte(`{"question":"x","max_steps":1}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/research (search fails) = %d, want 200 (resilient empty report); body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, secret := range []string{"SECRET", "Authorization", "Bearer", "deadbeef123", "private.example", "api_key", "KEY999"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("research report leaked swallowed provider secret %q: %s", secret, raw)
		}
	}
}

// The 400 decode-error body now carries `operation` (additive — a strict subset
// of the 500 envelope) on every POST route.
func TestRESTDecodeErrorEnvelopeHasOperation(t *testing.T) {
	h := newTestHTTPHandler(t)
	cases := map[string]string{
		"/api/search":             "search",
		"/api/extract":            "extract",
		"/api/search_and_extract": "search_and_extract",
		"/api/research":           "research",
	}
	for path, op := range cases {
		rec := doREST(t, h, http.MethodPost, path, []byte("{not json"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %s malformed = %d, want 400", path, rec.Code)
		}
		var env struct {
			Operation string `json:"operation"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s 400 body not JSON: %v", path, err)
		}
		if env.Operation != op || env.Error == "" {
			t.Errorf("%s 400 envelope = %+v, want operation=%q + non-empty error", path, env, op)
		}
	}
}

func TestRESTOversizedBodyAllPostRoutes(t *testing.T) {
	h := newTestHTTPHandler(t)
	big := make([]byte, 2<<20) // >1 MiB trips MaxBytesReader during Decode
	for i := range big {
		big[i] = 'a'
	}
	body := append(append([]byte(`{"query":"`), big...), []byte(`"}`)...)
	for _, path := range []string{"/api/search", "/api/extract", "/api/search_and_extract", "/api/research"} {
		rec := doREST(t, h, http.MethodPost, path, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("oversized POST %s = %d, want 400", path, rec.Code)
		}
	}
}

func TestRESTUnknownPath404(t *testing.T) {
	h := newTestHTTPHandler(t)
	rec := doREST(t, h, http.MethodGet, "/api/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/nope = %d, want 404", rec.Code)
	}
}

func jsonEnvelopeKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
