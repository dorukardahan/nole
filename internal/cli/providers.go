package cli

import (
	"fmt"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newProvidersCommand() *cobra.Command {
	var jsonOut bool
	var liveUsage bool
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Show provider status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := defaultService().ProviderStatusWithOptions(cmd.Context(), core.ProviderStatusOptions{LiveUsage: liveUsage, SyncLedger: liveUsage})
			if jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), resp)
			}
			for _, status := range resp.Providers {
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
	cmd.Flags().BoolVar(&liveUsage, "live-usage", false, "query provider usage APIs when available and sync the local quota ledger")
	return cmd
}
