package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSearchCommand() *cobra.Command {
	var taskRaw string
	var limit int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the web using task-based free-tier routing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := runSearch(args[0], parseTask(taskRaw), limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(resp)
			}
			for _, result := range resp.Results {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n%s\n\n", result.Title, result.URL, result.Snippet)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&taskRaw, "task", "general", "task type: general, news, docs, research")
	cmd.Flags().IntVar(&limit, "limit", 5, "maximum results")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	return cmd
}
