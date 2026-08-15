package mcpserver

import (
	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/version"
	"github.com/mark3labs/mcp-go/server"
)

func New(svc *core.Service) *server.MCPServer {
	s := server.NewMCPServer("nole", version.Version, server.WithToolCapabilities(false))
	RegisterTools(s, svc)
	return s
}

// NewCompact creates the opt-in single-tool MCP surface. New retains the
// stable six-tool 1.x surface unchanged for existing clients.
func NewCompact(svc *core.Service) *server.MCPServer {
	s := server.NewMCPServer("nole", version.Version, server.WithToolCapabilities(false))
	RegisterCompactTools(s, svc)
	return s
}
