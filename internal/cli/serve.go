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

			// Build the service FIRST: defaultService() loads the local env file
			// (~/.config/nole/.env), so NOLE_LOG set only there is in the
			// environment before we read it. Constructing the logger earlier would
			// pin it to the process-env default and ignore an env-file NOLE_LOG
			// (Codex review on PR #41).
			svc := defaultService()

			// One logger for the whole serve lifecycle (binding warning + the
			// handler's encode/lifecycle diagnostics), so NOLE_LOG governs all of
			// it consistently. Always os.Stderr — stdout stays MCP/REST only.
			logger := nolelog.FromEnv(os.Stderr)

			if !bindIsLoopback(listen) {
				logger.Warn("serve.non_loopback_bind",
					nolelog.F("addr", listen),
					nolelog.F("warning", "endpoints are UNAUTHENTICATED and expose provider keys and quota; front with a reverse proxy / network ACL"))
			}

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
