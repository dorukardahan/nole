package mcpserver

import (
	"testing"

	"github.com/dorukardahan/nole/internal/providers/mock"
)

// stableMCPToolsAlways is the v1.0.0-frozen set of MCP tools advertised on EVERY
// configuration. stableMCPToolsExtract are advertised ONLY when an extract-capable
// provider (Tavily/Firecrawl key, or local Scrapling) is configured. Under the
// stability commitment (docs/STABILITY.md), the tool names + their meaning are
// frozen for 1.x; this lock fails on any silent drift so adding/removing a tool is
// a conscious, documented decision.
var (
	stableMCPToolsAlways  = map[string]bool{"search": true, "research": true, "provider_status": true, "budget_status": true}
	stableMCPToolsExtract = map[string]bool{"extract": true, "search_and_extract": true}
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

func TestStableMCPToolSurfaceWithExtract(t *testing.T) {
	// An extract-capable key (Tavily) -> the full 6-tool surface.
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "fake-tavily-key")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("NOLE_SCRAPLING_PYTHON", "")

	want := map[string]bool{}
	for n := range stableMCPToolsAlways {
		want[n] = true
	}
	for n := range stableMCPToolsExtract {
		want[n] = true
	}
	tools := callToolsList(t, newTestMCPServerWithProviders(t, mock.New("mock"), mock.New("tavily")))
	for n := range want {
		if !tools[n] {
			t.Errorf("frozen MCP tool %q is MISSING with an extract-capable provider configured — update docs/STABILITY.md + this lock", n)
		}
	}
	for n := range tools {
		if !want[n] {
			t.Errorf("UNEXPECTED MCP tool %q — adding a tool is a surface change; document it in docs/STABILITY.md and update this lock", n)
		}
	}
}
