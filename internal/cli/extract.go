package cli

import (
	"context"
	"fmt"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newExtractCommand() *cobra.Command {
	var format string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "extract <url>",
		Short: "Extract clean content from a URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := defaultService().Extract(context.Background(), core.ExtractRequest{URL: args[0], Format: format})
			if err != nil {
				if jsonOut {
					_ = writeJSONTo(cmd.OutOrStdout(), buildCLIError("extract", err, resp.Route, resp.RouteTrace))
				}
				return err
			}
			if jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Content)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "output format")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
