package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/mark3labs/mcp-go/server"
)

// stableMCPToolsAlways is the v1.0.0-frozen set of MCP tools advertised on EVERY
// configuration. stableMCPToolsExtract are advertised ONLY when an extract-capable
// provider (Tavily/Firecrawl key, or local Scrapling) is configured. Under the
// stability commitment (docs/STABILITY.md), the tool names + their parameters are
// frozen for 1.x; these locks fail on any silent drift so adding/removing a tool or
// parameter is a conscious, documented decision.
var (
	stableMCPToolsAlways  = map[string]bool{"search": true, "research": true, "provider_status": true, "budget_status": true}
	stableMCPToolsExtract = map[string]bool{"extract": true, "search_and_extract": true}

	// stableMCPToolParams pins each tool's input parameter NAME set (the full
	// 6-tool surface, i.e. with an extract-capable provider configured).
	stableMCPToolParams = map[string][]string{
		"search":             {"query", "task", "limit", "include_trace"},
		"extract":            {"url", "format", "include_trace"},
		"search_and_extract": {"query", "task", "limit", "extract_top", "include_trace"},
		"research":           {"question", "max_steps"},
		"provider_status":    {},
		"budget_status":      {},
	}

	// stableTaskEnum pins the advertised `task` vocabulary (search tasks; extract is
	// a routing key, not a search task, and is excluded).
	stableTaskEnum = []string{"general", "news", "docs", "academic", "factcheck", "semantic", "code", "social", "people", "pricing", "research"}
)

func TestStableMCPToolSurfaceWithoutExtract(t *testing.T) {
	// Only a non-extract-capable key (brave) -> extract tools hidden.
	t.Setenv("BRAVE_API_KEY", "fake-brave-key")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("NOLE_SCRAPLING_PYTHON", "")

	tools := callToolsList(t, newTestMCPServerWithProviders(t, mock.New("mock"), mock.New("brave")))
	for n := range stableMCPToolsAlways {
		if !tools[n] {
			t.Errorf("frozen MCP tool %q is MISSING — removing a v1.0.0 tool is BREAKING; update docs/STABILITY.md + this lock", n)
		}
	}
	for n := range stableMCPToolsExtract {
		if tools[n] {
			t.Errorf("extract-gated tool %q must be hidden when no extract-capable provider is configured", n)
		}
	}
	for n := range tools {
		if !stableMCPToolsAlways[n] && !stableMCPToolsExtract[n] {
			t.Errorf("UNEXPECTED MCP tool %q — adding a tool is a surface change; document it in docs/STABILITY.md and update this lock", n)
		}
	}
}

// TestStableMCPToolSurfaceWithExtract locks the full 6-tool surface for EVERY way
// an extract-capable provider can be configured (Tavily key, Firecrawl key, or
// local Scrapling), so the extract-gating itself is pinned, not just one path.
func TestStableMCPToolSurfaceWithExtract(t *testing.T) {
	want := map[string]bool{}
	for n := range stableMCPToolsAlways {
		want[n] = true
	}
	for n := range stableMCPToolsExtract {
		want[n] = true
	}

	cases := []struct {
		name string
		env  map[string]string
	}{
		{"tavily-key", map[string]string{"TAVILY_API_KEY": "fake-tavily-key"}},
		{"firecrawl-key", map[string]string{"FIRECRAWL_API_KEY": "fake-firecrawl-key"}},
		{"local-scrapling", map[string]string{"NOLE_SCRAPLING_PYTHON": "/usr/bin/python3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Clear all extract-capable signals, then set this case's.
			t.Setenv("BRAVE_API_KEY", "")
			t.Setenv("BRAVE_SEARCH_API_KEY", "")
			t.Setenv("TAVILY_API_KEY", "")
			t.Setenv("FIRECRAWL_API_KEY", "")
			t.Setenv("NOLE_SCRAPLING_PYTHON", "")
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			tools := callToolsList(t, newTestMCPServerWithProviders(t, mock.New("mock")))
			for n := range want {
				if !tools[n] {
					t.Errorf("[%s] frozen MCP tool %q is MISSING with an extract-capable provider — update docs/STABILITY.md + this lock", c.name, n)
				}
			}
			for n := range tools {
				if !want[n] {
					t.Errorf("[%s] UNEXPECTED MCP tool %q — adding a tool is a surface change; document it + update this lock", c.name, n)
				}
			}
		})
	}
}

// TestStableMCPToolParams pins each tool's input-parameter name set. A renamed or
// removed param (e.g. search's `query`, research's `max_steps`) is a breaking
// surface change and must fail here, not slip past the name-only tool lock.
func TestStableMCPToolParams(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "fake-tavily-key") // extract-capable -> full surface
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("NOLE_SCRAPLING_PYTHON", "")

	got := callToolParams(t, newTestMCPServerWithProviders(t, mock.New("mock")))
	for tool, wantParams := range stableMCPToolParams {
		gotParams, ok := got[tool]
		if !ok {
			t.Errorf("tool %q not advertised", tool)
			continue
		}
		// []string{} (non-nil) so a no-param tool compares equal to callToolParams'
		// non-nil empty slice (reflect.DeepEqual(nil, []string{}) is false).
		w := append([]string{}, wantParams...)
		sort.Strings(w)
		sort.Strings(gotParams)
		if !reflect.DeepEqual(w, gotParams) {
			t.Errorf("tool %q params drifted: want %v, got %v — a param add/remove/rename is a v1.0.0 surface change; update docs/STABILITY.md + this lock", tool, w, gotParams)
		}
	}
}

func TestStableTaskEnum(t *testing.T) {
	got := buildTaskEnumValues()
	want := append([]string(nil), stableTaskEnum...)
	g := append([]string(nil), got...)
	sort.Strings(want)
	sort.Strings(g)
	if !reflect.DeepEqual(want, g) {
		t.Errorf("task enum drifted: want %v, got %v — adding/removing a task value changes the advertised vocabulary; update docs/STABILITY.md + this lock", want, g)
	}
}

// callToolParams sends tools/list and returns, per tool, its input parameter names.
func callToolParams(t *testing.T, srv *server.MCPServer) map[string][]string {
	t.Helper()
	msg, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/list", "params": map[string]any{}})
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	raw := srv.HandleMessage(context.Background(), json.RawMessage(msg))
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal result: %v", err)
	}
	var env struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("unmarshal tools/list: %v\nraw: %s", err, b)
	}
	out := map[string][]string{}
	for _, tl := range env.Result.Tools {
		params := make([]string, 0, len(tl.InputSchema.Properties))
		for p := range tl.InputSchema.Properties {
			params = append(params, p)
		}
		out[tl.Name] = params
	}
	return out
}
