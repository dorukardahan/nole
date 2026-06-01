package cli

import (
	"fmt"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newSearchCommand() *cobra.Command {
	var taskRaw string
	var limit int
	var jsonOut bool
	var insightRaw string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the web using task-based free-tier routing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			insightMode, err := parseInsightModeFlag(insightRaw)
			if err != nil {
				return err
			}
			resp, err := runSearch(cmd.Context(), args[0], resolveCLITask(cmd, taskRaw), limit)
			resp = applySearchInsightMode(resp, insightMode)
			if err != nil {
				if jsonOut {
					_ = writeJSONTo(cmd.OutOrStdout(), buildCLIErrorWithInsightMode("search", err, resp.Route, resp.RouteTrace, insightMode))
				}
				return err
			}
			if jsonOut {
				return writeJSONTo(cmd.OutOrStdout(), resp)
			}
			writeHumanRoutingInsight(cmd.OutOrStdout(), resp.RoutingInsight, resp.RouteTrace, insightMode)
			if resp.TaskSource != "" && insightMode != core.InsightOff {
				fmt.Fprintf(cmd.OutOrStdout(), "Task: %s (%s)\n", resp.Task, resp.TaskSource)
			}
			for _, result := range resp.Results {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n%s\n\n", result.Title, result.URL, result.Snippet)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&taskRaw, "task", "", taskHelpText())
	cmd.Flags().IntVar(&limit, "limit", 5, "maximum results")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	cmd.Flags().StringVar(&insightRaw, "insight", string(core.InsightCompact), "routing insight output: compact, off, or verbose")
	return cmd
}

func taskHelpText() string {
	parts := make([]string, 0, len(core.TaskTypes()))
	for _, t := range core.TaskTypes() {
		if t == core.TaskExtract {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", string(t), core.TaskDescription(t)))
	}
	return "task type: " + strings.Join(parts, ", ")
}

// resolveCLITask returns the task to route on from the --task flag. An unset flag
// yields the empty task so Service.Search auto-classifies the query; an explicit
// flag (including --task general) is parsed and honored. parseTask("") collapses
// to general, so distinguishing "omitted" from "explicit --task general" requires
// cmd.Flags().Changed rather than inspecting the raw value.
func resolveCLITask(cmd *cobra.Command, raw string) core.TaskType {
	if !cmd.Flags().Changed("task") {
		return ""
	}
	return parseTask(raw)
}
