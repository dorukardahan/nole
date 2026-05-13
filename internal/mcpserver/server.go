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
