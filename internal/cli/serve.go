package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var listen string
	var mcp bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server (optional persistent MCP + REST API)",
		Long: `Start a persistent HTTP server with:
  --mcp    Streamable HTTP MCP endpoint at /mcp
  --listen Bind address (default: 127.0.0.1:8765)

This is an advanced surface for team/shared/remote usage.
For local agent usage, prefer 'searchmcp mcp' (stdio).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !mcp {
				return fmt.Errorf("specify --mcp to enable MCP HTTP endpoint. REST API coming soon.")
			}

			svc := defaultService()
			handler, err := newHTTPHandler(svc)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "searchmcp serve: listening on %s\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  MCP endpoint: http://%s/mcp\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  Health: http://%s/health\n", listen)
			return handler.start(listen)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8765", "bind address (host:port)")
	cmd.Flags().BoolVar(&mcp, "mcp", false, "enable Streamable HTTP MCP endpoint")
	return cmd
}
