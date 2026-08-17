package cli

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

type mcpRunner func(context.Context, io.Reader, io.Writer, io.Writer, bool) error

func newMCPCommand() *cobra.Command {
	return newMCPCommandWithRunner(runMCP)
}

func newMCPCommandWithRunner(run mcpRunner) *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := run(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), compact); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&compact, "compact", false, "advertise only the single web_evidence tool")
	return cmd
}

func runMCP(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, compact bool) error {
	// Drive the stdio server with the root signal context (main installs
	// signal.NotifyContext) so Ctrl-C / SIGTERM cancels the read loop and
	// exits cleanly — consistent with `nole serve`. We call Listen directly
	// rather than server.ServeStdio because ServeStdio installs its own,
	// redundant SIGINT/SIGTERM handler on a separate context that ignores ctx.
	// Keep diagnostics on the command's stderr while stdout remains JSON-RPC only.
	svc := defaultService()
	mcpSrv := mcpserver.New(svc)
	if compact {
		mcpSrv = mcpserver.NewCompact(svc)
	}
	srv := server.NewStdioServer(mcpSrv)
	srv.SetErrorLogger(log.New(stderr, "", log.LstdFlags))
	return srv.Listen(ctx, stdin, stdout)
}
