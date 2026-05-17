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
		status := h.svc.ProviderStatus(ctx)
		json.NewEncoder(w).Encode(status)
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

	// Delegate to MCP server message handler
	ctx := context.Background()
	result := h.mcp.HandleMessage(ctx, body)
	if result == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
func buildMCPServer(svc *core.Service) *server.MCPServer {
	return mcpserver.New(svc)
}

func writeHTTPJSONError(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
