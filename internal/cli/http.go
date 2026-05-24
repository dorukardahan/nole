package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/dorukardahan/nole/internal/safeerr"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type httpHandler struct {
	svc    *core.Service
	mcp    *server.MCPServer
	server *http.Server
}

func newHTTPHandler(svc *core.Service) (*httpHandler, error) {
	// Use the existing MCP server builder from mcpserver package
	// We import it indirectly via a constructor
	mcpSrv := buildMCPServer(svc)
	return &httpHandler{
		svc: svc,
		mcp: mcpSrv,
	}, nil
}

func (h *httpHandler) start(addr string) error {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := map[string]interface{}{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		json.NewEncoder(w).Encode(status)
	})

	// MCP Streamable HTTP endpoint
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		h.handleMCP(w, r)
	})

	// Provider status endpoint
	mux.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		resp := h.svc.ProviderStatus(ctx)
		json.NewEncoder(w).Encode(resp)
	})

	// Budget status endpoint
	mux.HandleFunc("/api/budget", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.svc.BudgetStatus())
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
			Query string `json:"query"`
			Task  string `json:"task"`
			Limit int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeHTTPJSONError(w, http.StatusBadRequest, map[string]string{"error": safeerr.Message(err)})
			return
		}
		if req.Limit == 0 {
			req.Limit = 5
		}
		resp, err := h.svc.Search(r.Context(), core.SearchRequest{
			Query: req.Query,
			Task:  core.TaskType(req.Task),
			Limit: req.Limit,
		})
		if err != nil {
			writeHTTPJSONError(w, http.StatusInternalServerError, buildCLIError("search", err, resp.Route, resp.RouteTrace))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Extract API endpoint
	mux.HandleFunc("/api/extract", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req struct {
			URL    string `json:"url"`
			Format string `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeHTTPJSONError(w, http.StatusBadRequest, map[string]string{"error": safeerr.Message(err)})
			return
		}
		resp, err := h.svc.Extract(r.Context(), core.ExtractRequest{
			URL:    req.URL,
			Format: req.Format,
		})
		if err != nil {
			writeHTTPJSONError(w, http.StatusInternalServerError, buildCLIError("extract", err, resp.Route, resp.RouteTrace))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	h.server = &http.Server{
		Handler: mux,
		// Slowloris-class hardening. Defaults are 0 which means "no limit";
		// matter most when the user passes --listen 0.0.0.0:port, but cheap
		// to set unconditionally.
		//
		// WriteTimeout sized for the worst-case provider-backed handler:
		// Service.Search / Service.Extract try providers sequentially, each
		// with a 20-30s provider client timeout and up to 2 retry attempts.
		// A naive 60s cap can guillotine a legitimate handler mid-fallback
		// (Codex review on PR #25). 300s leaves room for the full route
		// chain plus DDGS rate-limit Retry-After waits; Read/Header timeouts
		// still bound the client side independently.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	fmt.Fprintf(os.Stderr, "nole HTTP server ready on %s\n", addr)
	return h.server.Serve(listener)
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

func writeHTTPJSONError(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
