package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

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

Defaults to free-first/no-hidden-paid-spend routing. Explicit cost policy settings can allow premium-capable providers.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			svc := defaultService()
			ctx := cmd.Context()

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

	// Step 1: classify the question to drive a task-fit, deterministic fan-out.
	// The old hardcoded [general,research,docs] all routed to the same providers
	// reordered, so the fan-out self-deduped and wasted steps.
	searchTasks := classifiedResearchTasks(question)
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
			// A cancelled/expired context (Ctrl-C / SIGTERM) is fatal: surface it
			// instead of logging a "failed step" and returning a partial report
			// with a success exit code.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
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
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			fmt.Fprintf(os.Stderr, "research: extract step failed: %s\n", safeerr.Message(err))
			continue
		}
		providerSet[resp.Provider] = true

		// Truncate on a rune boundary, not a byte boundary: a raw content[:2000]
		// can split a multibyte UTF-8 sequence and emit U+FFFD mojibake into the
		// JSON an agent consumes (the same class of bug v0.3.0 fixed for provider
		// snippets via core.TruncateRunes). The Truncated flag already signals the
		// cut, so we trim to a rune budget without appending an ellipsis.
		const researchContentRuneBudget = 2000
		content := resp.Content
		truncated := false
		if utf8.RuneCountInString(content) > researchContentRuneBudget {
			content = string([]rune(content)[:researchContentRuneBudget])
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
	// Map iteration order is randomized; sort so the providers_used array in the
	// --json output (and the human report) is stable across otherwise-identical
	// runs.
	sort.Strings(report.Providers)

	// Step 3: Synthesize summary from extracts and source snippets
	report.Summary = synthesizeSummary(question, report.Sources, report.Extracts)

	return report, nil
}

// classifiedResearchTasks builds a deterministic, task-fit fan-out for research
// by classifying the question (replacing the old hardcoded [general,research,
// docs], whose routes overlapped so the fan-out self-deduped). Order follows the
// planner's score-sorted intents; a membership set de-dups; general+research are
// appended for breadth on single-intent questions. The caller's maxSteps guard
// trims the list. Slice-order build (never iterate the map) keeps it stable.
func classifiedResearchTasks(question string) []core.TaskType {
	classification := core.ClassifyQuery(question, core.PlanOptions{})
	seen := make(map[core.TaskType]bool)
	var tasks []core.TaskType
	add := func(t core.TaskType) {
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		tasks = append(tasks, t)
	}
	add(classification.PrimaryTask)
	for _, intent := range classification.Intents {
		add(intent.Task)
	}
	add(core.TaskGeneral)
	add(core.TaskResearch)
	return tasks
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
			content = core.TruncateRunes(content, 300)
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
