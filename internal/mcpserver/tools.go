package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	searchToolDescription  = "Search the public web for internet research, current information, technical docs, news, fact-checking, code examples, pricing, people/company lookups, or deep-research source discovery using free-tier task-based provider routing."
	extractToolDescription = "Extract clean readable content from a public web page URL for summarization, citation, documentation lookup, or research context using free-tier routing with local URL safety preflight."
)

// hasExtractCapableConfigured reports whether any BYOK provider that supports
// the extract capability has its key configured in the environment. Used at
// MCP tool-registration time to decide whether mcp__nole__extract should be
// advertised at all. This avoids an expensive provider.Status HTTP probe at
// startup: we read env vars directly instead.
func hasExtractCapableConfigured() bool {
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
	// tipEmitted is captured by the search handler closure. One MCPServer
	// instance = one process = one session, so this flag provides
	// first-of-session emission without any synchronisation overhead (stdio
	// MCP serialises requests; HTTP MCP would need a mutex, but that is a
	// future concern).
	tipEmitted := false
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
		// First-of-session upgrade hint. Once a tip has been emitted on this
		// MCP connection, subsequent search calls omit it so the AI tool is
		// not nagged on every query.
		if !tipEmitted {
			statusResp := svc.ProviderStatus(ctx)
			tip := core.BuildSetupTip(statusResp.SetupSuggestions)
			if tip != nil {
				resp.SetupTip = tip
			}
			tipEmitted = true
		}
		b, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	})

	if hasExtractCapableConfigured() {
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
