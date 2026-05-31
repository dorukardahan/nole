package cli

import (
	"context"
	"errors"
	"os"

	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Drive the stdio server with the root signal context (main installs
			// signal.NotifyContext) so Ctrl-C / SIGTERM cancels the read loop and
			// exits cleanly — consistent with `nole serve`. We call Listen directly
			// rather than server.ServeStdio because ServeStdio installs its own,
			// redundant SIGINT/SIGTERM handler on a separate context that ignores
			// cmd.Context(). NewStdioServer logs errors to stderr, so MCP stdout
			// stays JSON-RPC only.
			srv := server.NewStdioServer(mcpserver.New(defaultService()))
			if err := srv.Listen(cmd.Context(), os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}
}
