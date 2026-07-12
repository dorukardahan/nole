package cli

import (
	"fmt"
	"strings"

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
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join([]string{
					status.Name,
					state,
					string(status.CostClass),
					status.PolicyReason,
					providerHumanReason(status, liveUsage),
				}, string(rune(9))))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	cmd.Flags().BoolVar(&liveUsage, "live-usage", false, "query provider usage APIs when available and sync the local quota ledger")
	return cmd
}

func providerHumanReason(status core.ProviderStatus, liveUsage bool) string {
	reason := strings.TrimSpace(status.Reason)
	if !liveUsage || strings.TrimSpace(status.RemoteUsageError) == "" {
		return reason
	}
	if reason == "" {
		return "remote_usage_error"
	}
	return reason + "; remote_usage_error"
}
