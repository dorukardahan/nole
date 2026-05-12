package mcpserver

import (
	"github.com/dorukardahan/searchmcp/internal/core"
	"github.com/dorukardahan/searchmcp/internal/version"
	"github.com/mark3labs/mcp-go/server"
)

func New(svc *core.Service) *server.MCPServer {
	s := server.NewMCPServer("searchmcp", version.Version, server.WithToolCapabilities(false))
	RegisterTools(s, svc)
	return s
}
