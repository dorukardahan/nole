package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/safeerr"
	"github.com/spf13/cobra"
)

func newResearchCommand() *cobra.Command {
	var jsonOutput bool
	var maxSteps int

	cmd := &cobra.Command{
		Use:   "research <question>",
		Short: "Multi-step search + extract + synthesis with citations",
		Long: `Performs deep research on a question:
  1. Searches across providers for relevant sources
  2. Extracts key content from top results
  3. Synthesizes a cited summary

Uses free-tier routing. No paid requests.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			svc := defaultService()
			ctx := context.Background()

			report, err := researchPipeline(ctx, svc, question, maxSteps)
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

// ResearchReport is the full output of a research pipeline run.
type ResearchReport struct {
	Question  string            `json:"question"`
	Summary   string            `json:"summary"`
	Sources   []ResearchSource  `json:"sources"`
	Extracts  []ResearchExtract `json:"extracts"`
	Providers []string          `json:"providers_used"`
	Steps     int               `json:"steps"`
}

// ResearchSource is a search result used in research.
type ResearchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	From    string `json:"from"`
}

// ResearchExtract is extracted content from a source.
type ResearchExtract struct {
	URL       string `json:"url"`
	Provider  string `json:"provider"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

func researchPipeline(ctx context.Context, svc *core.Service, question string, maxSteps int) (*ResearchReport, error) {
	report := &ResearchReport{
		Question: question,
	}
	providerSet := make(map[string]bool)

	// Step 1: Broad search across multiple task types for coverage
	searchTasks := []core.TaskType{core.TaskGeneral, core.TaskResearch, core.TaskDocs}
	var allSources []ResearchSource
	seenURLs := make(map[string]bool)

	for i, task := range searchTasks {
		if i >= maxSteps {
			break
		}
		resp, err := svc.Search(ctx, core.SearchRequest{
			Query: question,
			Task:  task,
			Limit: 5,
		})
		if err != nil {
			// Log but continue with other task types
			fmt.Fprintf(os.Stderr, "research: search step %d (%s) failed: %s\n", i+1, task, safeerr.Message(err))
			continue
		}

		providerSet[resp.Provider] = true
		report.Steps++

		for _, r := range resp.Results {
			if seenURLs[r.URL] {
				continue
			}
			seenURLs[r.URL] = true
			allSources = append(allSources, ResearchSource{
				Title:   r.Title,
				URL:     r.URL,
				Snippet: r.Snippet,
				From:    r.Provider,
			})
		}
	}

	report.Sources = allSources

	// Step 2: Extract top N unique sources
	extractLimit := min(len(allSources), 5)
	var toExtract []ResearchSource
	for _, s := range allSources {
		if len(toExtract) >= extractLimit {
			break
		}
		// Skip URLs that look like non-extractable (PDF, social, etc.)
		if strings.HasSuffix(s.URL, ".pdf") || strings.Contains(s.URL, "reddit.com") {
			continue
		}
		toExtract = append(toExtract, s)
	}

	for _, src := range toExtract {
		resp, err := svc.Extract(ctx, core.ExtractRequest{
			URL:    src.URL,
			Format: "markdown",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "research: extract step failed: %s\n", safeerr.Message(err))
			continue
		}
		providerSet[resp.Provider] = true

		content := resp.Content
		truncated := false
		if len(content) > 2000 {
			content = content[:2000]
			truncated = true
		}

		report.Extracts = append(report.Extracts, ResearchExtract{
			URL:       src.URL,
			Provider:  resp.Provider,
			Content:   content,
			Truncated: truncated,
		})
		report.Steps++
	}

	for p := range providerSet {
		report.Providers = append(report.Providers, p)
	}

	// Step 3: Synthesize summary from extracts and source snippets
	report.Summary = synthesizeSummary(question, report.Sources, report.Extracts)

	return report, nil
}

func synthesizeSummary(question string, sources []ResearchSource, extracts []ResearchExtract) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Research findings for: %s\n\n", question))

	if len(extracts) > 0 {
		sb.WriteString("Key findings:\n")
		for i, ext := range extracts {
			// Find source title
			title := ext.URL
			for _, s := range sources {
				if s.URL == ext.URL {
					title = s.Title
					break
				}
			}
			// Extract first meaningful paragraph
			content := ext.Content
			// Skip markdown headers at start
			for strings.HasPrefix(content, "#") {
				if idx := strings.Index(content, "\n"); idx >= 0 {
					content = content[idx+1:]
				} else {
					break
				}
			}
			content = strings.TrimSpace(content)
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("%d. [%s](%s): %s\n\n", i+1, title, ext.URL, content))
		}
	}

	if len(sources) > len(extracts) {
		sb.WriteString("Additional sources:\n")
		count := 0
		for _, s := range sources {
			if count >= 5 {
				break
			}
			// Skip already extracted
			found := false
			for _, e := range extracts {
				if e.URL == s.URL {
					found = true
					break
				}
			}
			if found {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", s.Title, s.URL, s.Snippet))
			count++
		}
	}

	return sb.String()
}

func printResearchReport(cmd *cobra.Command, report *ResearchReport) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Research: %s\n", report.Question)
	fmt.Fprintf(cmd.OutOrStdout(), "Steps: %d | Sources: %d | Extracts: %d | Providers: %v\n\n",
		report.Steps, len(report.Sources), len(report.Extracts), report.Providers)
	fmt.Fprintln(cmd.OutOrStdout(), report.Summary)
	return nil
}
