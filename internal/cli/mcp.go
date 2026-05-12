package cli

import (
	"github.com/dorukardahan/searchmcp/internal/mcpserver"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return server.ServeStdio(mcpserver.New(defaultService()))
		},
	}
}
