package cli

import (
	"fmt"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newExtractCommand() *cobra.Command {
	var format string
	var jsonOut bool
	var insightRaw string
	cmd := &cobra.Command{
		Use:   "extract <url>",
		Short: "Extract clean content from a URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			insightMode, err := parseInsightModeFlag(insightRaw)
			if err != nil {
				return err
			}
			resp, err := defaultService().Extract(cmd.Context(), core.ExtractRequest{URL: args[0], Format: format})
			resp = applyExtractInsightMode(resp, insightMode)
			if err != nil {
				if jsonOut {
					_ = writeJSONTo(cmd.OutOrStdout(), buildCLIErrorWithInsightMode("extract", err, resp.Route, resp.RouteTrace, insightMode))
				}
				return err
			}
			if jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), resp)
			}
			writeHumanRoutingInsight(cmd.OutOrStdout(), resp.RoutingInsight, resp.RouteTrace, insightMode)
			fmt.Fprintln(cmd.OutOrStdout(), resp.Content)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "output format")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	cmd.Flags().StringVar(&insightRaw, "insight", string(core.InsightCompact), "routing insight output: compact, off, or verbose")
	return cmd
}
