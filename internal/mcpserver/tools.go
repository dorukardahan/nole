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
	// tipState tracks whether the setup_tip has already been emitted, keyed by
	// MCP session ID. This gives each MCP client session its own once-per-session
	// tip, which is the correct semantics for both transport modes:
	//
	//   - stdio (nole mcp): one process = one client = one session. The context
	//     carries no ClientSession, so we fall back to the fixed key
	//     "stdio-default", preserving the existing once-per-process behavior.
	//
	//   - HTTP (nole serve --mcp): two sub-cases:
	//       a) Client supplies Mcp-Session-Id header → persistent session. The
	//          ID is stored in the map and the tip is emitted once per session.
	//       b) Client omits the header → ephemeral session. The request ID is
	//          generated with the "http-ephemeral-" prefix (see internal/cli/http.go).
	//          Ephemeral sessions bypass the map entirely: the tip is always
	//          emitted (each request is a different client) and nothing is stored,
	//          keeping memory bounded regardless of traffic volume.
	//
	// The map grows up to one entry per unique CLIENT-supplied session ID over
	// the server lifetime. Ephemeral (generated) IDs never enter the map.
	// Each entry is a string key + bool, so 10k persistent sessions ≈ a few KB;
	// negligible. Session-close cleanup via OnUnregisterSession hooks is a
	// possible future improvement but is not required for correctness.
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
		// Determine the session key. In HTTP MCP mode the library injects a
		// ClientSession into ctx; extract its ID. In stdio mode no session is
		// injected, so fall back to a fixed key so stdio behaves as before
		// (once per process lifetime).
		sessionID := "stdio-default"
		if sess := server.ClientSessionFromContext(ctx); sess != nil {
			sessionID = sess.SessionID()
		}
		// Ephemeral HTTP sessions (generated per-request when the client omits
		// Mcp-Session-Id) carry the "http-ephemeral-" prefix (see
		// internal/cli/http.go). For these, always emit the tip and skip the
		// map entirely — each request is a different client so suppression is
		// wrong, and storing nothing keeps memory bounded.
		const ephemeralPrefix = "http-ephemeral-"
		isEphemeral := strings.HasPrefix(sessionID, ephemeralPrefix)

		var shouldEmit bool
		if isEphemeral {
			// Ephemeral sessions always get the tip; no map entry is written.
			shouldEmit = true
		} else {
			// Persistent sessions (stdio or client-pinned HTTP): emit once per
			// session. The lock ensures concurrent callers for the same session
			// cannot both observe false; the map entry is written before release.
			tipState.Lock()
			shouldEmit = !tipState.emitted[sessionID]
			tipState.emitted[sessionID] = true
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
