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
  /mcp          Streamable HTTP MCP endpoint
  /api/search   /api/extract   /api/search_and_extract   /api/research
  /api/providers   /api/budget   /health (REST)

  --mcp    enable the server (also serves the /api/* REST endpoints)
  --listen bind address (default: 127.0.0.1:8765)

This is an advanced surface for team/shared/remote usage (e.g. one keyed Nólë
serving several machines). For local single-user agent usage, 'nole mcp' (stdio)
is simpler.

AUTH: set NOLE_SERVE_TOKEN to require "Authorization: Bearer <token>" on every
endpoint except /health. The endpoints serve your BYOK keys + quota, so a
NON-loopback bind (e.g. 0.0.0.0) REQUIRES a token: without NOLE_SERVE_TOKEN set,
'serve' refuses to start on a non-loopback bind (fail closed) rather than exposing
your keys. The default loopback bind needs no token (only local processes reach it).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !mcp {
				return fmt.Errorf("specify --mcp to start the HTTP server (serves the MCP endpoint at /mcp and the REST API at /api/*; see docs/CLIENTS/README.md)")
			}

			// The bearer token (NOLE_SERVE_TOKEN) is read from the process env; the
			// local env file is loaded by defaultService() below, but the token must
			// gate startup BEFORE that, so we read it directly here.
			token := strings.TrimSpace(os.Getenv("NOLE_SERVE_TOKEN"))

			// SECURITY (fail closed): a non-loopback bind exposes the key-bearing
			// endpoints beyond this host, so it MUST require a bearer token. Refuse to
			// start otherwise rather than serve your provider keys + quota to the
			// network. Loopback binds and token-protected binds are allowed.
			if err := serveSecurityPreflight(listen, token); err != nil {
				return err
			}

			// Build the service first: defaultService() loads the local env file
			// (~/.config/nole/.env), so NOLE_LOG set only there is honored by the
			// handler's diagnostic logger (encode failures + server lifecycle), not
			// just a process-env NOLE_LOG. Always os.Stderr — stdout stays MCP/REST.
			svc := defaultService()
			logger := nolelog.FromEnv(os.Stderr)

			handler, err := newHTTPHandler(svc, logger, token)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "nole serve: listening on %s\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  MCP endpoint: http://%s/mcp\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  REST API:     http://%s/api/{search,extract,search_and_extract,research,providers,budget}\n", listen)
			fmt.Fprintf(cmd.OutOrStdout(), "  Health:       http://%s/health\n", listen)
			if token != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "  Auth:         bearer token REQUIRED on all endpoints except /health (send the NOLE_SERVE_TOKEN value as a Bearer credential)")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "  Auth:         none — loopback bind only. Set NOLE_SERVE_TOKEN to require a bearer token (and to allow a non-loopback bind).")
			}
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

// serveSecurityPreflight enforces the fail-closed rule for the HTTP surface: a
// NON-loopback bind exposes the host's BYOK keys + quota to anyone who can reach
// it, so it MUST require a bearer token (NOLE_SERVE_TOKEN). It returns an error
// (refuse to start) when addr is non-loopback and no token is configured;
// loopback binds and token-protected binds are allowed. This is the resolve-time
// guard; httpHandler.withAuth enforces the token per request. Returning an error
// instead of warning-and-serving is deliberate: serving provider keys to a
// network without auth is exactly the failure this prevents.
func serveSecurityPreflight(addr, token string) error {
	if bindIsLoopback(addr) || strings.TrimSpace(token) != "" {
		return nil
	}
	return fmt.Errorf("refusing to start: %s is not a loopback bind and NOLE_SERVE_TOKEN is not set — these endpoints expose your provider keys and quota to anyone who can reach the bind. Set NOLE_SERVE_TOKEN to require a bearer token, or bind to loopback (e.g. 127.0.0.1:8765)", addr)
}

// bindIsLoopback reports whether addr binds only to a loopback interface. A
// non-loopback bind exposes the key-bearing endpoints beyond the host, so
// serveSecurityPreflight requires a bearer token for it (refusing to start
// otherwise). An unparseable/hostname bind is treated as non-loopback (fail safe
// toward requiring the token).
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
