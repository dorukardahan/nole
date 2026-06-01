package cli

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/dorukardahan/nole/internal/nolelog"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var listen string
	var mcp bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server (Streamable HTTP MCP endpoint + REST API)",
		Long: `Start a persistent HTTP server that exposes, on the same bind address:
  /mcp          Streamable HTTP MCP endpoint (when --mcp is passed)
  /api/search   /api/extract   /api/providers   /api/budget   /health (REST)

  --mcp    enable the server (also serves the /api/* REST endpoints)
  --listen bind address (default: 127.0.0.1:8765)

This is an advanced surface for team/shared/remote usage. The endpoints have
NO built-in authentication and expose your BYOK keys and quota to anyone who
can reach the bind address — keep the default loopback bind, or front a
non-loopback bind with a reverse proxy / network ACL. For local agent usage,
prefer 'nole mcp' (stdio).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !mcp {
				return fmt.Errorf("specify --mcp to start the HTTP server (serves the MCP endpoint at /mcp and the REST API at /api/*; see docs/CLIENTS/README.md)")
			}

			// SECURITY notice: a non-loopback bind exposes the unauthenticated
			// endpoints (provider keys + quota) beyond this host. This is the only
			// runtime warning of that exposure, so it is printed UNCONDITIONALLY to
			// stderr and is NOT routed through nolelog — a verbosity knob like
			// NOLE_LOG=off must never silence a safety message (Codex review on
			// PR #41). Same rationale as main.go's raw fatal print.
			if !bindIsLoopback(listen) {
				fmt.Fprintf(os.Stderr, "warning: binding %s is not loopback; these endpoints are UNAUTHENTICATED and expose your provider keys and quota. Front it with a reverse proxy / network ACL.\n", listen)
			}

			// Build the service first: defaultService() loads the local env file
			// (~/.config/nole/.env), so NOLE_LOG set only there is honored by the
			// handler's diagnostic logger (encode failures + server lifecycle), not
			// just a process-env NOLE_LOG. Always os.Stderr — stdout stays MCP/REST.
			svc := defaultService()
			logger := nolelog.FromEnv(os.Stderr)

			handler, err := newHTTPHandler(svc, logger)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "nole serve: listening on %s\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  MCP endpoint: http://%s/mcp\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  REST API:     http://%s/api/{search,extract,providers,budget}\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  Health:       http://%s/health\n", listen)
			// cmd.Context() is the root signal-aware context: main installs
			// signal.NotifyContext (SIGINT/SIGTERM) and restores default signal
			// handling on the first interrupt, so a second Ctrl-C force-exits a slow
			// drain. start() shuts down gracefully when this context is cancelled,
			// letting in-flight provider-backed requests drain instead of being
			// hard-killed mid-fallback. We intentionally do NOT register a second,
			// nested signal handler here (it would consume the force-exit signal).
			return handler.start(cmd.Context(), listen)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8765", "bind address (host:port)")
	cmd.Flags().BoolVar(&mcp, "mcp", false, "enable the HTTP server (MCP endpoint + REST API)")
	return cmd
}

// bindIsLoopback reports whether addr binds only to a loopback interface. A
// non-loopback bind exposes the unauthenticated endpoints beyond the host, so
// the caller warns. An unparseable/hostname bind is treated as non-loopback
// (fail safe toward warning).
func bindIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		// Empty host means "all interfaces" (e.g. ":8765").
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
