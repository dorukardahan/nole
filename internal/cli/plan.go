package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newClassifyCommand() *cobra.Command {
	var taskRaw string
	var singleIntent bool
	var jsonOut bool
	var insightRaw string
	cmd := &cobra.Command{
		Use:   "classify <query>",
		Short: "Classify a query with the LLM-free rule-based intent planner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !jsonOut {
				return fmt.Errorf("classify always outputs JSON; omit --json=false")
			}
			insightMode, err := parseInsightModeFlag(insightRaw)
			if err != nil {
				return err
			}
			opts, err := planOptionsFromFlags(taskRaw, "", singleIntent)
			if err != nil {
				return err
			}
			classification := core.ClassifyQuery(args[0], opts)
			classification = applyClassificationInsightMode(classification, insightMode)
			return writeJSONTo(cmd.OutOrStdout(), classification)
		},
	}
	cmd.Flags().StringVar(&taskRaw, "task", "", "override task classification (accepts community as social)")
	cmd.Flags().BoolVar(&singleIntent, "single-intent", false, "keep only the primary intent")
	cmd.Flags().BoolVar(&jsonOut, "json", true, "output JSON (default)")
	cmd.Flags().StringVar(&insightRaw, "insight", string(core.InsightCompact), "routing insight output: compact, off, or verbose")
	return cmd
}

func newRoutePlanCommand() *cobra.Command {
	var taskRaw string
	var providersRaw string
	var singleIntent bool
	var jsonOut bool
	var insightRaw string
	cmd := &cobra.Command{
		Use:   "route-plan <query>",
		Short: "Plan deterministic provider routes for a query without provider calls",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !jsonOut {
				return fmt.Errorf("route-plan always outputs JSON; omit --json=false")
			}
			insightMode, err := parseInsightModeFlag(insightRaw)
			if err != nil {
				return err
			}
			opts, err := planOptionsFromFlags(taskRaw, providersRaw, singleIntent)
			if err != nil {
				return err
			}
			loadDefaultNoleEnvFile()
			plan := core.BuildRoutePlan(args[0], configuredRouteMatrix(os.Getenv("TINYFISH_API_KEY")), opts)
			plan = applyRoutePlanInsightMode(plan, insightMode)
			return writeJSONTo(cmd.OutOrStdout(), plan)
		},
	}
	cmd.Flags().StringVar(&taskRaw, "task", "", "override task classification (accepts community as social)")
	cmd.Flags().StringVar(&providersRaw, "providers", "", "comma-separated provider route override for planning only")
	cmd.Flags().BoolVar(&singleIntent, "single-intent", false, "keep only the primary intent")
	cmd.Flags().BoolVar(&jsonOut, "json", true, "output JSON (default)")
	cmd.Flags().StringVar(&insightRaw, "insight", string(core.InsightCompact), "routing insight output: compact, off, or verbose")
	return cmd
}

func planOptionsFromFlags(taskRaw, providersRaw string, singleIntent bool) (core.PlanOptions, error) {
	var opts core.PlanOptions
	opts.SingleIntent = singleIntent
	if strings.TrimSpace(taskRaw) != "" {
		task, ok := parseTaskStrict(taskRaw)
		if !ok || task == core.TaskExtract {
			return opts, fmt.Errorf("invalid route-planner task override %q", taskRaw)
		}
		opts.TaskOverride = task
	}
	providers := splitProviders(providersRaw)
	for _, provider := range providers {
		if !validPlannerProvider(provider) {
			return opts, fmt.Errorf("invalid provider override %q", provider)
		}
	}
	if len(providers) > 0 {
		opts.ProviderOverride = providers
	}
	return opts, nil
}

func splitProviders(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	providers := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		provider := strings.ToLower(strings.TrimSpace(part))
		if provider == "" {
			continue
		}
		if seen[provider] {
			continue
		}
		seen[provider] = true
		providers = append(providers, provider)
	}
	return providers
}

func validPlannerProvider(provider string) bool {
	switch provider {
	// SEARCH-capable providers only. The route-planner plans search routes (it
	// rejects TaskExtract above), so extract-only providers (scrapling, httpfetch)
	// are intentionally NOT valid here — `--providers httpfetch` correctly errors
	// rather than emitting an unusable search plan for a provider that never searches.
	// "arxiv" IS search-capable (academic route), so it belongs here, like wikipedia.
	case "brave", "tavily", "firecrawl", "tinyfish", "wikipedia", "arxiv", "ddgs":
		return true
	default:
		return false
	}
}
