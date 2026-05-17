package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/dorukardahan/nole/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var checkMCP bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check nole configuration and provider health",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "nole doctor")
			fmt.Fprintln(cmd.OutOrStdout(), "- binary: ok")
			fmt.Fprintln(cmd.OutOrStdout(), "- stdio: logs must go to stderr; stdout reserved for MCP protocol")

			svc := defaultService()
			statuses := svc.ProviderStatus(context.Background())

			available := 0
			for _, s := range statuses {
				if s.Available {
					available++
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "- providers: %d registered (%d available)\n", len(statuses), available)

			fmt.Fprintln(cmd.OutOrStdout(), "")
			for _, s := range statuses {
				status := "unavailable"
				if s.Available {
					status = "available"
				}
				caps := ""
				for i, c := range s.Capabilities {
					if i > 0 {
						caps += ", "
					}
					caps += string(c)
				}
				line := fmt.Sprintf("  %-12s %s  [%s]", s.Name, status, caps)
				if s.Reason != "" {
					line += fmt.Sprintf("  (%s)", s.Reason)
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "- secrets: not printed")
			keys := []struct {
				env  string
				name string
			}{
				{"JINA_API_KEY", "jina"},
				{"FIRECRAWL_API_KEY", "firecrawl"},
				{"BRAVE_API_KEY", "brave"},
				{"BRAVE_SEARCH_API_KEY", "brave (alt)"},
				{"TAVILY_API_KEY", "tavily"},
			}
			for _, k := range keys {
				set := "not set"
				if os.Getenv(k.env) != "" {
					set = "set"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-22s %s\n", k.env, set)
			}

			budget := svc.BudgetStatus()
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintf(cmd.OutOrStdout(), "- budget: $%d.%02d hard cap\n", budget.HardCapCents/100, budget.HardCapCents%100)
			for _, e := range budget.Entries {
				if e.KeylessFree {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s keyless-free\n", e.Provider)
				} else if e.Unknown {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s unknown (free-tier)\n", e.Provider)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %d remaining\n", e.Provider, e.FreeRemaining)
				}
			}

			if checkMCP {
				fmt.Fprintln(cmd.OutOrStdout(), "")
				result := checkMCPStdioSmoke(cmd.Context())
				status := "ok"
				if !result.OK {
					status = "failed"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "- mcp: %s\n", status)
				fmt.Fprintf(cmd.OutOrStdout(), "  stdout: startup-clean (%d bytes before protocol input)\n", result.StdoutBytes)
				if result.Reason != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  reason: %s\n", result.Reason)
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&checkMCP, "mcp", false, "also check MCP stdio startup/stdout health")
	return cmd
}

type mcpStdioSmokeResult struct {
	OK          bool
	StdoutBytes int
	Reason      string
}

func checkMCPStdioSmoke(ctx context.Context) mcpStdioSmokeResult {
	select {
	case <-ctx.Done():
		return mcpStdioSmokeResult{OK: false, Reason: ctx.Err().Error()}
	default:
	}

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return mcpStdioSmokeResult{OK: false, Reason: fmt.Sprintf("stdout capture: %v", err)}
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	func() {
		defer func() { os.Stdout = oldStdout }()
		_ = mcpserver.New(defaultService())
	}()
	_ = writeEnd.Close()
	captured, err := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if err != nil {
		return mcpStdioSmokeResult{OK: false, Reason: fmt.Sprintf("read captured stdout: %v", err)}
	}
	if len(captured) != 0 {
		return mcpStdioSmokeResult{OK: false, StdoutBytes: len(captured), Reason: "MCP startup wrote non-protocol bytes to stdout"}
	}
	return mcpStdioSmokeResult{OK: true, StdoutBytes: 0}
}
