package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	searchToolDescription  = "Search the public web for internet research, current information, technical docs, news, fact-checking, code examples, pricing, people/company lookups, or deep-research source discovery using free-tier task-based provider routing."
	extractToolDescription = "Extract clean readable content from a public web page URL for summarization, citation, documentation lookup, or research context using free-tier routing with local URL safety preflight."
)

// tipStateMaxEntries caps the number of client-supplied session IDs tracked in
// the tip-deduplication map. Once this limit is reached, new (unseen) session
// IDs are treated as fail-open: the tip is emitted but not recorded. Legitimate
// stable-session clients whose IDs are already in the map are unaffected.
//
// 1000 persistent sessions × (≈50 bytes/entry) ≈ 50 KB — negligible. The cap
// primarily guards against a rotating-header client that sends a different
// Mcp-Session-Id on every request, which would otherwise cause the map to grow
// linearly with traffic volume.
const tipStateMaxEntries = 1000

// HasExtractCapableConfigured reports whether any BYOK provider that supports
// the extract capability has its key configured in the environment. Used at
// MCP tool-registration time to decide whether mcp__nole__extract should be
// advertised at all. This avoids an expensive provider.Status HTTP probe at
// startup: we read env vars directly instead. Exported so the doctor command
// can mirror the MCP server's registration decision for its conditional smoke
// assertion.
func HasExtractCapableConfigured() bool {
	for _, p := range core.BYOKProviders() {
		if !p.SupportsExtract {
			continue
		}
		for _, ev := range p.EnvVars {
			if os.Getenv(ev) != "" {
				return true
			}
		}
	}
	return false
}

func RegisterTools(s *server.MCPServer, svc *core.Service) {
	taskDesc := buildTaskDescription()
	searchTool := mcp.NewTool(
		"search",
		mcp.WithDescription(searchToolDescription),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language web search or internet research query")),
		mcp.WithString("task", mcp.Description(taskDesc)),
		mcp.WithNumber("limit", mcp.Description("Maximum number of search results to return")),
	)
	// tipState tracks which client-supplied session IDs have already received
	// the setup_tip. Only persistent sessions (client-supplied Mcp-Session-Id
	// or stdio) use this map. Ephemeral HTTP requests (client omitted the
	// header) are identified by the EphemeralCtxKey context value set in
	// internal/cli/http.go and always receive the tip without touching the map.
	//
	// Transport breakdown:
	//
	//   - stdio (nole mcp): one process = one client. No session is injected
	//     into ctx, so we fall back to the fixed key "stdio-default". Tip is
	//     emitted once per process lifetime.
	//
	//   - HTTP (nole serve --mcp), client sends Mcp-Session-Id: persistent
	//     session. ID stored in map, tip emitted once per session.
	//
	//   - HTTP, no Mcp-Session-Id header: ephemeral request. EphemeralCtxKey
	//     is set to true in the context by internal/cli/http.go. The tip is
	//     always emitted; nothing is written to the map. Memory is bounded
	//     regardless of traffic volume, and a client that later decides to pin
	//     a session cannot accidentally inherit ephemeral semantics from a
	//     server-generated ID (the previous "http-ephemeral-" prefix approach).
	//
	// Map is capped at tipStateMaxEntries (see constant above). New IDs seen
	// after the cap is reached are emitted but not recorded (fail-open).
	var tipState struct {
		sync.Mutex
		emitted map[string]bool // session ID → already emitted (persistent sessions only)
	}
	tipState.emitted = make(map[string]bool)
	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		task := core.TaskType(req.GetString("task", "general"))
		limit := int(req.GetFloat("limit", 5))
		resp, err := svc.Search(ctx, core.SearchRequest{Query: query, Task: task, Limit: limit})
		if err != nil {
			return mcp.NewToolResultError(string(toolErrorJSON("search", err, resp.Route, resp.RouteTrace))), nil
		}

		// Check whether this is an ephemeral HTTP request (no Mcp-Session-Id
		// header). The flag is set at dispatch time in internal/cli/http.go
		// so ephemerality is determined by how the request arrived, not by any
		// string pattern in the session ID.
		ephemeral, _ := ctx.Value(EphemeralCtxKey{}).(bool)

		var shouldEmit bool
		if ephemeral {
			// Ephemeral requests always get the tip; nothing is written to the
			// map, so memory stays bounded regardless of traffic volume.
			shouldEmit = true
		} else {
			// Persistent sessions (stdio or client-pinned HTTP): emit once per
			// session. Determine the session key: if the mcp-go library injected
			// a ClientSession use its ID; otherwise fall back to "stdio-default"
			// so stdio preserves its once-per-process behaviour.
			sessionID := "stdio-default"
			if sess := server.ClientSessionFromContext(ctx); sess != nil {
				sessionID = sess.SessionID()
			}

			// Bounded-map logic (fixes P2 Issue 2).
			// - Already seen: respect the recorded value (emit once, suppress forever).
			// - Not seen, room in map: record and emit.
			// - Not seen, map at cap: emit but do not record (fail-open). A
			//   rotating-header client gets potentially-repeated tips but the
			//   server's memory is capped. Legitimate stable-session clients
			//   already recorded are unaffected.
			tipState.Lock()
			if seen, ok := tipState.emitted[sessionID]; ok {
				shouldEmit = !seen
				if shouldEmit {
					tipState.emitted[sessionID] = true
				}
			} else if len(tipState.emitted) < tipStateMaxEntries {
				tipState.emitted[sessionID] = true
				shouldEmit = true
			} else {
				// Cap reached: emit but don't record.
				shouldEmit = true
			}
			tipState.Unlock()
		}

		if shouldEmit {
			statusResp := svc.ProviderStatus(ctx)
			if tip := core.BuildSetupTip(statusResp.SetupSuggestions); tip != nil {
				resp.SetupTip = tip
			}
		}
		b, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	if HasExtractCapableConfigured() {
		extractTool := mcp.NewTool(
			"extract",
			mcp.WithDescription(extractToolDescription),
			mcp.WithString("url", mcp.Required(), mcp.Description("Public http(s) web page URL to read and extract")),
			mcp.WithString("format", mcp.Description("Output format, default markdown")),
		)
		s.AddTool(extractTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			url, err := req.RequireString("url")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			format := req.GetString("format", "markdown")
			resp, err := svc.Extract(ctx, core.ExtractRequest{URL: url, Format: format})
			if err != nil {
				return mcp.NewToolResultError(string(toolErrorJSON("extract", err, resp.Route, resp.RouteTrace))), nil
			}
			b, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		})
	}

	statusTool := mcp.NewTool("provider_status", mcp.WithDescription("Show configured provider health plus sanitized cost policy/class status"))
	s.AddTool(statusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, err := json.MarshalIndent(svc.ProviderStatus(ctx), "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	budgetTool := mcp.NewTool("budget_status", mcp.WithDescription("Show local cost policy, cap, spend, and free-tier budget/quota status"))
	s.AddTool(budgetTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, err := json.MarshalIndent(svc.BudgetStatus(), "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})
}

// buildTaskDescription generates the task parameter description from the canonical task list.
func buildTaskDescription() string {
	parts := make([]string, 0, len(core.TaskTypes()))
	for _, t := range core.TaskTypes() {
		if t == core.TaskExtract {
			continue // extract is not a search task
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", string(t), core.TaskDescription(t)))
	}
	return "Task type: " + strings.Join(parts, ", ")
}
