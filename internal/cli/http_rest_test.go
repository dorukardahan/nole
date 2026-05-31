package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

// doREST drives a request through the REAL routing table (buildMux), which the
// existing http_test.go MCP tests bypass by calling handleMCP directly. All
// cases are net-free: the mock provider answers search/extract, and extract
// URLs use public literal IPs so safenet.ValidateURL passes without DNS.
func doREST(t *testing.T, h *httpHandler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	h.buildMux().ServeHTTP(rec, req)
	return rec
}

func TestRESTReadEndpointsRejectNonGET(t *testing.T) {
	h := newTestHTTPHandler(t)
	for _, path := range []string{"/health", "/api/providers", "/api/budget"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rec := doREST(t, h, method, path, nil)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want 405", method, path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "method not allowed") {
				t.Fatalf("%s %s body = %q, want 'method not allowed'", method, path, rec.Body.String())
			}
		}
	}
}

func TestRESTReadEndpointsServeGET(t *testing.T) {
	h := newTestHTTPHandler(t)
	for _, path := range []string{"/health", "/api/providers", "/api/budget"} {
		rec := doREST(t, h, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("GET %s Content-Type = %q, want application/json", path, ct)
		}
		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s body is not valid JSON: %v", path, err)
		}
	}

	// /health carries a status field.
	rec := doREST(t, h, http.MethodGet, "/health", nil)
	var health struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("/health status = %q, want ok", health.Status)
	}
}

func TestRESTHealthHEAD(t *testing.T) {
	h := newTestHTTPHandler(t)
	rec := doREST(t, h, http.MethodHead, "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD /health = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("HEAD /health Content-Type = %q, want application/json", ct)
	}
}

func TestRESTWriteEndpointsRejectNonPOST(t *testing.T) {
	h := newTestHTTPHandler(t)
	for _, path := range []string{"/api/search", "/api/extract"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			rec := doREST(t, h, method, path, nil)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want 405", method, path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "method not allowed") {
				t.Fatalf("%s %s body = %q, want 'method not allowed'", method, path, rec.Body.String())
			}
		}
	}
}

func TestRESTSearchPostHappyPath(t *testing.T) {
	h := newTestHTTPHandler(t)

	rec := doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"hello","task":"general","limit":3}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/search = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("POST /api/search Content-Type = %q, want application/json", ct)
	}
	var resp core.SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if resp.Provider != "mock" || len(resp.Results) != 3 {
		t.Fatalf("search response = provider %q, %d results; want mock, 3", resp.Provider, len(resp.Results))
	}

	// Omitting limit defaults to 5 (http.go: req.Limit == 0 -> 5).
	rec = doREST(t, h, http.MethodPost, "/api/search", []byte(`{"query":"hello","task":"general"}`))
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode default-limit search response: %v", err)
	}
	if len(resp.Results) != 5 {
		t.Fatalf("default-limit search returned %d results, want 5", len(resp.Results))
	}
}

func TestRESTExtractPostHappyPath(t *testing.T) {
	h := newTestHTTPHandler(t)
	// Public literal IP passes safenet.ValidateURL without DNS.
	rec := doREST(t, h, http.MethodPost, "/api/extract", []byte(`{"url":"http://93.184.216.34/","format":"markdown"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/extract = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("POST /api/extract Content-Type = %q, want application/json", ct)
	}
	var resp core.ExtractResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode extract response: %v", err)
	}
	if resp.Provider != "mock" || resp.Content == "" {
		t.Fatalf("extract response = provider %q, content %q; want mock, non-empty", resp.Provider, resp.Content)
	}
}

func TestRESTMalformedJSONReturns400(t *testing.T) {
	h := newTestHTTPHandler(t)
	for _, body := range [][]byte{[]byte("{not json"), {}} {
		rec := doREST(t, h, http.MethodPost, "/api/search", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/search body=%q = %d, want 400", body, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("malformed body Content-Type = %q, want application/json", ct)
		}
		var env map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("400 body is not JSON: %v", err)
		}
		if _, ok := env["error"]; !ok {
			t.Fatalf("400 body missing 'error' key: %v", env)
		}
	}
}

func TestRESTOversizedBodyReturns400(t *testing.T) {
	h := newTestHTTPHandler(t)
	// >1 MiB body trips http.MaxBytesReader during Decode -> 400.
	big := append([]byte(`{"query":"`), bytes.Repeat([]byte("a"), 2<<20)...)
	big = append(big, []byte(`"}`)...)
	rec := doREST(t, h, http.MethodPost, "/api/search", big)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized POST /api/search = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("oversized body Content-Type = %q, want application/json", ct)
	}
}

func TestRESTExtractErrorResponseDoesNotLeakSecrets(t *testing.T) {
	h := newTestHTTPHandler(t)
	// A loopback URL fails safenet.ValidateURL before any provider call, so the
	// 500 path runs through buildCLIError + safeerr.Message — proving the REST
	// error surface is redaction-safe.
	rec := doREST(t, h, http.MethodPost, "/api/extract", []byte(`{"url":"http://127.0.0.1/","format":"markdown"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/extract (loopback) = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("extract error Content-Type = %q, want application/json", ct)
	}
	var env struct {
		Operation string `json:"operation"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode extract error envelope: %v", err)
	}
	if env.Operation != "extract" {
		t.Fatalf("error envelope operation = %q, want extract", env.Operation)
	}
	if env.Error == "" {
		t.Fatal("error envelope has empty error message")
	}
	raw := rec.Body.String()
	for _, secret := range []string{"Bearer ", "api_key", "apikey", "Authorization"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("extract error response leaked %q: %s", secret, raw)
		}
	}
}
