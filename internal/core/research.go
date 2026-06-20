package core

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dorukardahan/nole/internal/nolelog"
	"github.com/dorukardahan/nole/internal/safeerr"
)

// maxResearchExtracts is the absolute ceiling on how many sources a single
// research run will extract, regardless of how large a caller's max_steps is.
// It bounds extract-quota burn and response size for depth-seeking callers; the
// per-call extract count is min(unique sources, max_steps, this).
const maxResearchExtracts = 5

// ResearchReport is the full output of a research run: multi-source evidence for
// the calling agent to synthesize. Nólë deliberately returns NO composed summary
// or answer — the gateway hands over clean sources + extracts; the agent thinks.
type ResearchReport struct {
	Question      string                 `json:"question"`
	Sources       []ResearchSource       `json:"sources"`
	Extracts      []ResearchExtract      `json:"extracts"`
	Providers     []string               `json:"providers_used"`
	Steps         int                    `json:"steps"`
	EvidenceSteps []ResearchEvidenceStep `json:"evidence_steps,omitempty"`
}

// ResearchEvidenceStep is a compact receipt for one search/extract/skip event in
// the research pipeline. It is observability only: it does not rank providers,
// judge source quality, or synthesize an answer.
type ResearchEvidenceStep struct {
	Kind           string   `json:"kind"`
	Task           TaskType `json:"task,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	URL            string   `json:"url,omitempty"`
	Status         string   `json:"status"`
	ResultCount    int      `json:"result_count,omitempty"`
	ContentPresent bool     `json:"content_present,omitempty"`
	Error          string   `json:"error,omitempty"`
	SkipReason     string   `json:"skip_reason,omitempty"`
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

// Research runs a multi-step search + extract pass over a question and returns
// the deduplicated sources and extracted content. It composes NO answer (that is
// the agent's job). A cancelled context (Ctrl-C / SIGTERM) is surfaced, not
// swallowed into a partial report.
func (s *Service) Research(ctx context.Context, question string, maxSteps int) (*ResearchReport, error) {
	report := &ResearchReport{Question: question}
	providerSet := make(map[string]bool)

	// Step 1: classified, deterministic fan-out for task-fit coverage.
	searchTasks := classifiedResearchTasks(question)
	var allSources []ResearchSource
	seenURLs := make(map[string]bool)

	for i, task := range searchTasks {
		if i >= maxSteps {
			break
		}
		resp, err := s.Search(ctx, SearchRequest{Query: question, Task: task, Limit: 5})
		provider := researchEvidenceProvider(resp.Provider, resp.RouteTrace)
		if err != nil {
			// A cancelled/expired context is fatal: surface it instead of logging a
			// "failed step" and returning a partial report with a success exit.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			report.EvidenceSteps = append(report.EvidenceSteps, ResearchEvidenceStep{
				Kind:        "search",
				Task:        task,
				Provider:    provider,
				Status:      "failed",
				ResultCount: len(resp.Results),
				Error:       safeerr.Message(err),
			})
			s.log.Warn("research.search_step_failed",
				nolelog.F("step", strconv.Itoa(i+1)),
				nolelog.F("task", string(task)),
				nolelog.Err(err))
			continue
		}
		if provider == "" {
			provider = resp.Provider
		}
		report.EvidenceSteps = append(report.EvidenceSteps, ResearchEvidenceStep{
			Kind:        "search",
			Task:        task,
			Provider:    provider,
			Status:      "success",
			ResultCount: len(resp.Results),
		})
		providerSet[resp.Provider] = true
		report.Steps++
		for _, r := range resp.Results {
			if seenURLs[r.URL] {
				continue
			}
			seenURLs[r.URL] = true
			allSources = append(allSources, ResearchSource{Title: r.Title, URL: r.URL, Snippet: r.Snippet, From: r.Provider})
		}
	}

	report.Sources = allSources

	// Step 2: extract top sources, bounded by BOTH maxSteps (so a small budget
	// like max_steps=1 doesn't fan out to many extracts) AND an absolute ceiling
	// maxResearchExtracts (so a large max_steps raised for search depth doesn't
	// explode the extract quota or response size). The .pdf/reddit skip is a
	// pre-existing research-pipeline heuristic, intentionally NOT shared with
	// SearchAndExtract (the dumb primitive that extracts the top results as-is).
	extractLimit := min(len(allSources), maxSteps, maxResearchExtracts)
	var toExtract []ResearchSource
	for _, src := range allSources {
		if len(toExtract) >= extractLimit {
			break
		}
		if skipReason, skip := researchSkipReason(src.URL); skip {
			report.EvidenceSteps = append(report.EvidenceSteps, ResearchEvidenceStep{
				Kind:       "skip",
				Provider:   src.From,
				URL:        src.URL,
				Status:     "skipped",
				SkipReason: skipReason,
			})
			continue
		}
		toExtract = append(toExtract, src)
	}

	for _, src := range toExtract {
		resp, err := s.Extract(ctx, ExtractRequest{URL: src.URL, Format: "markdown"})
		provider := researchEvidenceProvider(resp.Provider, resp.RouteTrace)
		if provider == "" {
			provider = src.From
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			report.EvidenceSteps = append(report.EvidenceSteps, ResearchEvidenceStep{
				Kind:     "extract",
				Provider: provider,
				URL:      src.URL,
				Status:   "failed",
				Error:    safeerr.Message(err),
			})
			s.log.Warn("research.extract_step_failed", nolelog.Err(err))
			continue
		}
		providerSet[resp.Provider] = true
		report.EvidenceSteps = append(report.EvidenceSteps, ResearchEvidenceStep{
			Kind:           "extract",
			Provider:       resp.Provider,
			URL:            src.URL,
			Status:         "success",
			ContentPresent: strings.TrimSpace(resp.Content) != "",
		})

		// Truncate on a rune boundary, not a byte boundary: a raw content[:2000]
		// can split a multibyte UTF-8 sequence and emit U+FFFD mojibake. The
		// Truncated flag signals the cut.
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
	// Map iteration order is randomized; sort so providers_used is stable.
	sort.Strings(report.Providers)

	return report, nil
}

func researchSkipReason(url string) (string, bool) {
	switch {
	case strings.HasSuffix(url, ".pdf"):
		return "pdf_source", true
	case strings.Contains(url, "reddit.com"):
		return "reddit_source", true
	default:
		return "", false
	}
}

func researchEvidenceProvider(provider string, trace []RouteAttempt) string {
	if provider != "" {
		return provider
	}
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Provider == "" || trace[i].Provider == "cache" || trace[i].Status != "failed" {
			continue
		}
		return trace[i].Provider
	}
	for i := len(trace) - 1; i >= 0; i-- {
		if trace[i].Provider == "" || trace[i].Provider == "cache" {
			continue
		}
		return trace[i].Provider
	}
	return ""
}

// classifiedResearchTasks builds a deterministic, task-fit fan-out for research
// by classifying the question. Order follows the planner's score-sorted intents;
// a membership set de-dups; general+research are appended for breadth on
// single-intent questions. The caller's maxSteps guard trims the list.
func classifiedResearchTasks(question string) []TaskType {
	classification := ClassifyQuery(question, PlanOptions{})
	seen := make(map[TaskType]bool)
	var tasks []TaskType
	add := func(t TaskType) {
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
	add(TaskGeneral)
	add(TaskResearch)
	return tasks
}
