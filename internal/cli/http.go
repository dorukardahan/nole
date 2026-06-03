package cli

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/dorukardahan/nole/internal/nolelog"
	"github.com/dorukardahan/nole/internal/safeerr"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type httpHandler struct {
	svc    *core.Service
	mcp    *server.MCPServer
	server *http.Server
	// log carries the server's diagnostic events (encode failures, lifecycle) to
	// stderr in the NOLE_LOG format. A nil log is a safe no-op, so the
	// http_test.go / v070_test.go struct-literal handlers stay silent without
	// wiring one.
	log *nolelog.Logger
	// token is the optional bearer token from NOLE_SERVE_TOKEN. When non-empty,
	// withAuth requires "Authorization: Bearer <token>" on every endpoint except
	// /health. Empty (the loopback-default case, and the struct-literal test
	// handlers) means no auth. Never logged.
	token string
}

// healthResponse is the body of GET/HEAD /health. Status is "ready" (HTTP 200)
// or "not_ready" (HTTP 503); Reason is set only when not ready;
// AvailableProviders lists the search-capable providers currently ready.
type healthResponse struct {
	Status             string   `json:"status"`
	Timestamp          string   `json:"timestamp"`
	Reason             string   `json:"reason,omitempty"`
	AvailableProviders []string `json:"available_providers"`
}

func newHTTPHandler(svc *core.Service, log *nolelog.Logger, token string) (*httpHandler, error) {
	// Use the existing MCP server builder from mcpserver package
	// We import it indirectly via a constructor
	mcpSrv := buildMCPServer(svc)
	cleaned := strings.TrimSpace(token)
	return &httpHandler{
		svc:   svc,
		mcp:   mcpSrv,
		log:   log,
		token: cleaned,
	}, nil
}

// withAuth wraps the mux with bearer-token authentication. When no token is
// configured (h.token == ""), every request passes through unchanged — the
// loopback-default convenience path (the startup preflight guarantees this only
// happens on a loopback bind). When a token IS configured, every endpoint EXCEPT
// /health requires "Authorization: Bearer <token>" (constant-time compared);
// /health stays open so readiness probes work and because it exposes no secrets.
func (h *httpHandler) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.token == "" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !h.validBearer(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			// Sanitized envelope; never echoes the configured token or the supplied one.
			h.writeHTTPJSONError(w, "auth", http.StatusUnauthorized, map[string]string{
				"operation": "auth",
				"error":     "missing or invalid bearer token (set the Authorization: Bearer header to the NOLE_SERVE_TOKEN value)",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validBearer reports whether the request carries the configured bearer token.
// Uses a constant-time comparison so a timing side-channel cannot reveal the
// token byte-by-byte. A length mismatch returns false (only the token length,
// not its content, could leak from that — acceptable).
func (h *httpHandler) validBearer(r *http.Request) bool {
	const prefix = "Bearer "
	hdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(hdr, prefix) {
		return false
	}
	got := strings.TrimPrefix(hdr, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) == 1
}

// httpErrorStatus maps a service error to the HTTP status for the error envelope.
// An exhausted/blocked free-tier (NoFreeQuotaError) is 402 Payment Required — an
// honest "this would require paid usage you have not authorized" — rather than a
// generic 500. Everything else is 500.
func httpErrorStatus(err error) int {
	if core.IsNoFreeQuota(err) {
		return http.StatusPaymentRequired
	}
	return http.StatusInternalServerError
}

// allowReadMethods gates a read-only endpoint to GET/HEAD, writing a 405 and
// returning false otherwise. The POST /api/{search,extract} handlers gate
// themselves; this keeps the read endpoints consistent with them on the
// remote-exposed surface instead of executing on any method.
func allowReadMethods(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (h *httpHandler) logEncodeErr(endpoint string, err error) {
	if err != nil {
		h.log.Error("serve.encode_failed", err, nolelog.F("endpoint", endpoint))
	}
}

// buildMux registers every route on a fresh ServeMux. Split out from start so
// the routing table is constructed independently of listener/lifecycle setup.
func (h *httpHandler) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint — a REAL readiness check (not an always-200 stub). Ready
	// iff at least one search-capable provider is available AND allowed by the
	// cost policy. "Available" already folds in circuit-breaker state (a
	// breakered provider that is currently short-circuiting reports
	// Available=false), so a degraded upstream flips /health to 503 without this
	// handler needing any breaker handle. Keyless DDGS is always available and
	// allowed, so a zero-key deployment is correctly "ready" — that is the
	// honest default. Readiness is orthogonal to budget: a hard-cap hit is a
	// /api/budget concern, not a health one.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !allowReadMethods(w, r) {
			return
		}
		resp := h.svc.ProviderStatus(r.Context())
		ready := make([]string, 0, len(resp.Providers))
		for _, p := range resp.Providers {
			if core.HasCapability(p.Capabilities, core.CapabilitySearch) && p.Available && p.AllowedByPolicy {
				ready = append(ready, p.Name)
			}
		}
		body := healthResponse{
			Status:             "ready",
			Timestamp:          time.Now().UTC().Format(time.RFC3339),
			AvailableProviders: ready,
		}
		code := http.StatusOK
		if len(ready) == 0 {
			body.Status = "not_ready"
			body.Reason = "no search-capable provider is available and allowed by policy"
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		h.logEncodeErr("/health", json.NewEncoder(w).Encode(body))
	})

	// MCP Streamable HTTP endpoint
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		h.handleMCP(w, r)
	})

	// Provider status endpoint
	mux.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) {
		if !allowReadMethods(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := h.svc.ProviderStatus(r.Context())
		h.logEncodeErr("/api/providers", json.NewEncoder(w).Encode(resp))
	})

	// Budget status endpoint
	mux.HandleFunc("/api/budget", func(w http.ResponseWriter, r *http.Request) {
		if !allowReadMethods(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h.logEncodeErr("/api/budget", json.NewEncoder(w).Encode(h.svc.BudgetStatus()))
	})

	// Search API endpoint
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Cap request body at 1 MiB to prevent unbounded memory growth on
		// untrusted input (slowloris / large-payload DoS). Default bind is
		// loopback; this matters when the user passes --listen 0.0.0.0.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req struct {
			Query        string `json:"query"`
			Task         string `json:"task"`
			Limit        int    `json:"limit"`
			IncludeTrace bool   `json:"include_trace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeHTTPDecodeError(w, "search", err)
			return
		}
		if req.Limit == 0 {
			req.Limit = 5
		}
		resp, err := h.svc.Search(r.Context(), core.SearchRequest{
			Query: req.Query,
			// NormalizeTaskParam (not a raw cast) so REST matches MCP: aliases like
			// "community"→social resolve correctly and an unknown/blank task falls
			// through to classification instead of misrouting + lying about
			// task_source. Keeps CLI/MCP/REST in lockstep (spec D1).
			Task:  core.NormalizeTaskParam(req.Task),
			Limit: req.Limit,
		})
		if err != nil {
			h.writeHTTPJSONError(w, "search", httpErrorStatus(err), buildCLIError("search", err, resp.Route, resp.RouteTrace))
			return
		}
		// route_trace is opt-in on the agent surface; omit by default (the compact
		// routing_insight stays). The error path above keeps its trace.
		if !req.IncludeTrace {
			resp.RouteTrace = nil
		}
		w.Header().Set("Content-Type", "application/json")
		h.logEncodeErr("/api/search", json.NewEncoder(w).Encode(resp))
	})

	// Extract API endpoint
	mux.HandleFunc("/api/extract", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req struct {
			URL          string `json:"url"`
			Format       string `json:"format"`
			IncludeTrace bool   `json:"include_trace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeHTTPDecodeError(w, "extract", err)
			return
		}
		resp, err := h.svc.Extract(r.Context(), core.ExtractRequest{
			URL:    req.URL,
			Format: req.Format,
		})
		if err != nil {
			h.writeHTTPJSONError(w, "extract", httpErrorStatus(err), buildCLIError("extract", err, resp.Route, resp.RouteTrace))
			return
		}
		if !req.IncludeTrace {
			resp.RouteTrace = nil
		}
		w.Header().Set("Content-Type", "application/json")
		h.logEncodeErr("/api/extract", json.NewEncoder(w).Encode(resp))
	})

	// Combined search→read primitive: search, then extract the top result(s).
	mux.HandleFunc("/api/search_and_extract", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req struct {
			Query        string `json:"query"`
			Task         string `json:"task"`
			Limit        int    `json:"limit"`
			ExtractTop   int    `json:"extract_top"`
			IncludeTrace bool   `json:"include_trace"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeHTTPDecodeError(w, "search_and_extract", err)
			return
		}
		resp, err := h.svc.SearchAndExtract(r.Context(), core.SearchAndExtractRequest{
			Query:      req.Query,
			Task:       core.NormalizeTaskParam(req.Task),
			Limit:      req.Limit,
			ExtractTop: req.ExtractTop,
		})
		if err != nil {
			h.writeHTTPJSONError(w, "search_and_extract", httpErrorStatus(err), buildCLIError("search_and_extract", err, resp.Search.Route, resp.Search.RouteTrace))
			return
		}
		if !req.IncludeTrace {
			resp.Search.RouteTrace = nil
			for i := range resp.Extracts {
				resp.Extracts[i].RouteTrace = nil
			}
		}
		w.Header().Set("Content-Type", "application/json")
		h.logEncodeErr("/api/search_and_extract", json.NewEncoder(w).Encode(resp))
	})

	// Multi-step research → structured evidence (sources + extracts), no answer.
	mux.HandleFunc("/api/research", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req struct {
			Question string `json:"question"`
			MaxSteps int    `json:"max_steps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeHTTPDecodeError(w, "research", err)
			return
		}
		if req.MaxSteps == 0 {
			req.MaxSteps = 3
		}
		report, err := h.svc.Research(r.Context(), req.Question, req.MaxSteps)
		if err != nil {
			// ResearchReport has no route/trace, so buildCLIError (which needs them)
			// does not apply; return a minimal sanitized envelope (operation+error —
			// the documented research-shape divergence, locked by a test).
			h.writeHTTPJSONError(w, "research", httpErrorStatus(err), map[string]string{"operation": "research", "error": safeerr.Message(err)})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		h.logEncodeErr("/api/research", json.NewEncoder(w).Encode(report))
	})

	return mux
}

// httpServerTimeouts returns the server's slowloris-class timeouts. Extracted so
// a test regression-locks them — especially the deliberate 300s WriteTimeout,
// sized for the worst-case provider fallback chain (Service.Search/Extract try
// providers sequentially, each with a 20-30s client timeout + up to 2 retries; a
// naive 60s cap guillotines a legitimate handler mid-fallback — Codex review on
// PR #25). 300s leaves room for the full route chain + DDGS Retry-After waits;
// the Read/Header timeouts bound the client side independently. They matter most
// under `--listen 0.0.0.0` but are cheap to set unconditionally. A future
// "tidy-up" must not silently shrink WriteTimeout.
func httpServerTimeouts() (readHeader, read, write, idle time.Duration) {
	return 5 * time.Second, 30 * time.Second, 300 * time.Second, 120 * time.Second
}

func (h *httpHandler) start(ctx context.Context, addr string) error {
	readHeader, read, write, idle := httpServerTimeouts()
	h.server = &http.Server{
		Handler:           h.withAuth(h.buildMux()),
		ReadHeaderTimeout: readHeader,
		ReadTimeout:       read,
		WriteTimeout:      write,
		IdleTimeout:       idle,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	h.log.Info("serve.ready", nolelog.F("addr", addr))

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- h.server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		// Serve only returns ErrServerClosed after a Shutdown; any other error
		// is a real listen/serve failure.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// SIGINT/SIGTERM: stop accepting new connections and give in-flight
		// handlers a bounded window to drain instead of hard-killing them. The
		// drain budget is intentionally shorter than WriteTimeout (300s): if a
		// slow provider-backed handler outlasts it, Shutdown returns
		// DeadlineExceeded — we log that and still exit cleanly (the listener is
		// already closed and the process is going away), rather than reporting a
		// shutdown failure for what is a normal Ctrl-C during a slow request.
		h.log.Info("serve.draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.server.Shutdown(shutdownCtx); err != nil {
			h.log.Warn("serve.drain_incomplete", nolelog.Err(err))
		}
		return nil
	}
}

func (h *httpHandler) handleMCP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST with JSON-RPC
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse JSON-RPC request
	var jsonrpcReq mcpgo.JSONRPCRequest
	if err := json.Unmarshal(body, &jsonrpcReq); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`))
		return
	}

	// Build a context that carries the MCP session (when the client supplied
	// a session header) and/or the ephemeral marker (when they did not).
	//
	// Design (fixes P2 "prefix-based ephemerality is fragile"):
	//
	//   - Client sends Mcp-Session-Id header → persistent session. Inject the
	//     session into ctx so ClientSessionFromContext returns a non-nil value
	//     with the client's ID; echo the ID back so the client knows it was
	//     accepted. The tip is emitted once for this session ID and suppressed
	//     on subsequent requests.
	//
	//   - Client omits Mcp-Session-Id header → ephemeral request. Do NOT
	//     generate or echo a server-side session ID. Instead set the
	//     EphemeralCtxKey context value to true; the tool handler reads that
	//     flag and always emits the tip without touching the map. Memory stays
	//     bounded regardless of traffic volume, and a client who later decides
	//     to pin a session cannot accidentally inherit "ephemeral" semantics
	//     just because the server happened to generate a matching-prefix ID.
	if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
		w.Header().Set("Mcp-Session-Id", sid) // echo client-supplied ID back
	}
	ctx := httpBuildContext(r.Context(), h.mcp, r)

	result := h.mcp.HandleMessage(ctx, body)
	if result == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// httpBuildContext prepares the request context for an MCP tool dispatch.
//
// If the client supplied an Mcp-Session-Id header the session is injected so
// that server.ClientSessionFromContext returns it inside the tool handler,
// enabling per-session tip deduplication. No ephemeral marker is set.
//
// If the header is absent the EphemeralCtxKey marker is set to true instead.
// No session is injected and no ID is generated. The tool handler reads the
// marker and always emits the tip without adding a map entry, keeping server
// memory bounded regardless of how many headerless requests arrive.
//
// We deliberately do NOT echo a server-generated ID back when the header is
// absent. Echoing a generated ID would invite clients to pin it in subsequent
// requests, which would then be incorrectly treated as ephemeral — the very
// fragility this function is designed to eliminate.
func httpBuildContext(base context.Context, srv *server.MCPServer, r *http.Request) context.Context {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		// Ephemeral request: mark it so the tip handler always emits.
		return context.WithValue(base, mcpserver.EphemeralCtxKey{}, true)
	}
	// Persistent session: inject so ClientSessionFromContext finds it.
	sess := server.NewInProcessSession(sessionID, nil)
	return srv.WithContext(base, sess)
}

// httpSessionForRequest is retained for the http_test.go tests that exercise
// session-ID extraction behaviour directly. New code should use httpBuildContext.
//
// Returns the session ID that would be used for the request (the client-supplied
// header value) and a corresponding InProcessSession. If no header is present the
// returned ID is empty string and the session is also empty — callers must handle
// the ephemeral case via httpBuildContext instead.
func httpSessionForRequest(r *http.Request) (string, *server.InProcessSession) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	sess := server.NewInProcessSession(sessionID, nil)
	return sessionID, sess
}

func buildMCPServer(svc *core.Service) *server.MCPServer {
	return mcpserver.New(svc)
}

// writeHTTPJSONError writes a JSON error body at status. It LOGS any encode
// failure (via the same logEncodeErr path as the success encoders) instead of
// swallowing it — a partial/truncated error body would otherwise vanish with no
// server-side trace. endpoint labels the diagnostic log line.
func (h *httpHandler) writeHTTPJSONError(w http.ResponseWriter, endpoint string, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	h.logEncodeErr(endpoint, json.NewEncoder(w).Encode(payload))
}

// writeHTTPDecodeError emits the 400 request-decode error body
// {"operation":op,"error":...}. It is a strict subset of the cliErrorEnvelope
// shape (no route/route_trace exist before dispatch), so a consumer parses
// operation+error uniformly across 400 (decode) and 500 (service) responses.
func (h *httpHandler) writeHTTPDecodeError(w http.ResponseWriter, op string, err error) {
	h.writeHTTPJSONError(w, op, http.StatusBadRequest, map[string]string{
		"operation": op,
		"error":     safeerr.Message(err),
	})
}
