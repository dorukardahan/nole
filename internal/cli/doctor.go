package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check searchmcp configuration and provider health",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "searchmcp doctor")
			fmt.Fprintln(cmd.OutOrStdout(), "- binary: ok")
			fmt.Fprintln(cmd.OutOrStdout(), "- stdio: logs must go to stderr; stdout reserved for MCP protocol")
			fmt.Fprintln(cmd.OutOrStdout(), "- secrets: not printed")
			statuses := defaultService().ProviderStatus(context.Background())
			fmt.Fprintf(cmd.OutOrStdout(), "- providers: %d registered\n", len(statuses))
			return nil
		},
	}
}
