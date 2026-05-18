package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newProvidersCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Show provider status",
		RunE: func(cmd *cobra.Command, args []string) error {
			statuses := defaultService().ProviderStatus(context.Background())
			if jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), statuses)
			}
			for _, status := range statuses {
				state := "unavailable"
				if status.Available {
					state = "available"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", status.Name, state, status.CostClass, status.PolicyReason, status.Reason)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
