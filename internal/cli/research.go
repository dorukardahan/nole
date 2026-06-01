package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/spf13/cobra"
)

func newResearchCommand() *cobra.Command {
	var jsonOutput bool
	var maxSteps int

	cmd := &cobra.Command{
		Use:   "research <question>",
		Short: "Multi-step search + extract returning cited sources for your agent to synthesize",
		Long: `Performs multi-step research on a question:
  1. Searches across task-fit provider routes for relevant sources
  2. Extracts key content from the top results
  3. Returns the deduplicated sources + extracted content

Nólë returns evidence, not a composed answer — you (or your agent) synthesize.
Defaults to free-first/no-hidden-paid-spend routing. Explicit cost policy settings can allow premium-capable providers.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			svc := defaultService()

			report, err := svc.Research(cmd.Context(), question, maxSteps)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}

			return printResearchReport(cmd, report)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().IntVar(&maxSteps, "max-steps", 3, "maximum search+extract iterations")
	return cmd
}

// printResearchReport renders the human view: header, sources, and short extract
// previews. The full extract content is only in --json / MCP / REST output — the
// terminal preview is a display convenience, not a quality judgment on the data.
func printResearchReport(cmd *cobra.Command, report *core.ResearchReport) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Research: %s\n", report.Question)
	fmt.Fprintf(out, "Steps: %d | Sources: %d | Extracts: %d | Providers: %v\n\n",
		report.Steps, len(report.Sources), len(report.Extracts), report.Providers)

	if len(report.Sources) > 0 {
		fmt.Fprintln(out, "Sources:")
		for i, s := range report.Sources {
			fmt.Fprintf(out, "%d. %s\n   %s  (%s)\n", i+1, s.Title, s.URL, s.From)
		}
		fmt.Fprintln(out)
	}

	if len(report.Extracts) > 0 {
		fmt.Fprintln(out, "Extracts (preview — full content in --json):")
		for _, e := range report.Extracts {
			preview := core.TruncateRunes(strings.TrimSpace(e.Content), 200)
			fmt.Fprintf(out, "- %s  (%s)\n  %s\n", e.URL, e.Provider, preview)
		}
	}
	return nil
}
