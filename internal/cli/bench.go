package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dorukardahan/nole/internal/bench"
	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/arxiv"
	"github.com/dorukardahan/nole/internal/providers/brave"
	"github.com/dorukardahan/nole/internal/providers/ddgs"
	"github.com/dorukardahan/nole/internal/providers/firecrawl"
	"github.com/dorukardahan/nole/internal/providers/httpfetch"
	"github.com/dorukardahan/nole/internal/providers/scrapling"
	"github.com/dorukardahan/nole/internal/providers/tavily"
	"github.com/dorukardahan/nole/internal/providers/wikipedia"
	"github.com/dorukardahan/nole/internal/safeerr"
	"github.com/spf13/cobra"
)

func newBenchCommand() *cobra.Command {
	var jsonOut bool
	var evidenceMD bool
	var live bool
	var maxLiveCases int
	var comprehensive bool
	var maxComprehensiveCases int
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run deterministic offline routing evals, or optional low-limit live smoke benchmarks",
		Long: `Run Nólë benchmark/eval fixtures.

Default mode is offline and deterministic: no provider network calls and no API keys required.
Use --live for a low-limit smoke run against configured free-tier/keyless providers.
Use --live --comprehensive to force every provider to run every fixture (capability-permitting),
bypassing the route matrix, cost policy and quota ledger for direct per-provider measurement.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if comprehensive && !live {
				return fmt.Errorf("--comprehensive requires --live")
			}
			var report bench.Report
			switch {
			case comprehensive:
				report = runComprehensiveBench(cmd.Context(), maxComprehensiveCases)
			case live:
				report = runLiveBench(cmd.Context(), maxLiveCases)
			default:
				report = bench.RunOffline(bench.DefaultFixtureSet(), core.DefaultRouteMatrix())
			}
			if evidenceMD {
				var out string
				if report.Mode == bench.ModeComprehensiveLive {
					out = bench.MarkdownComprehensiveSummary(report)
				} else {
					out = bench.MarkdownEvidenceSummary(report)
				}
				_, err := fmt.Fprint(cmd.OutOrStdout(), out)
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			printBenchReport(cmd, report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output benchmark report as JSON")
	cmd.Flags().BoolVar(&evidenceMD, "evidence-md", false, "output a sanitized Markdown route-evidence summary")
	cmd.Flags().BoolVar(&live, "live", false, "run optional low-limit live smoke benchmark against configured providers")
	cmd.Flags().IntVar(&maxLiveCases, "max-live-cases", 3, "maximum live fixture cases to run when --live is set")
	cmd.Flags().BoolVar(&comprehensive, "comprehensive", false, "with --live, force every provider to run every fixture (bypasses router/policy/ledger)")
	cmd.Flags().IntVar(&maxComprehensiveCases, "max-comprehensive-cases", 0, "with --live --comprehensive, bound the fixture set (0 = all fixtures)")
	return cmd
}

func printBenchReport(cmd *cobra.Command, report bench.Report) {
	fmt.Fprintf(cmd.OutOrStdout(), "nole bench: %s fixture %s\n", report.Mode, report.FixtureVersion)
	if report.Mode == bench.ModeComprehensiveLive {
		fmt.Fprintf(cmd.OutOrStdout(), "measurements: %d passed: %d failed: %d\n",
			report.Summary.TotalCases, report.Summary.PassedCases, report.Summary.FailedCases)
		provs := sortedKeys(report.ProviderSummary)
		for _, prov := range provs {
			s := report.ProviderSummary[prov]
			if s.Successes == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s: 0/%d success (no latency stats); errors=%v\n",
					prov, s.Calls, s.ErrorClasses)
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "- %s: %d/%d success, p50=%dms p95=%dms avg=%dms\n",
				prov, s.Successes, s.Calls, s.P50LatencyMS, s.P95LatencyMS, s.AvgLatencyMS)
		}
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cases: %d passed: %d failed: %d avg_score: %.2f\n",
		report.Summary.TotalCases, report.Summary.PassedCases, report.Summary.FailedCases, report.Summary.AverageScore)
	for _, c := range report.Cases {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s [%s/%s] selected=%s score=%.2f route=%v\n",
			c.ID, c.Task, c.Language, c.SelectedProvider, c.Score, c.Route)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func runComprehensiveBench(ctx context.Context, maxCases int) bench.Report {
	loadDefaultNoleEnvFile()
	return bench.RunComprehensiveLive(ctx, bench.DefaultFixtureSet(), comprehensiveBenchProviders(), bench.ComprehensiveOptions{
		MaxFixtures:    maxCases,
		NetworkContext: os.Getenv("BENCH_NETWORK_CONTEXT"),
		CostPolicy:     string(defaultQuotaPolicyFromEnv().Policy),
	})
}

func comprehensiveBenchProviders() map[string]core.Provider {
	return map[string]core.Provider{
		"brave":     brave.New(),
		"ddgs":      ddgs.New(),
		"firecrawl": firecrawl.New(),
		"scrapling": scrapling.New(),
		"tavily":    tavily.New(),
		"wikipedia": wikipedia.New(),
		"arxiv":     arxiv.New(),
		"httpfetch": httpfetch.New(),
	}
}

func runLiveBench(ctx context.Context, maxCases int) bench.Report {
	if maxCases <= 0 || maxCases > 10 {
		maxCases = 3
	}
	set := bench.DefaultFixtureSet()
	if maxCases < len(set.Fixtures) {
		set.Fixtures = set.Fixtures[:maxCases]
	}
	svc := defaultService()
	report := bench.Report{
		SchemaVersion:  "2",
		Mode:           bench.ModeLive,
		FixtureVersion: set.Version,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Evidence:       bench.LiveEvidenceMetadata(string(defaultQuotaPolicyFromEnv().Policy), maxCases),
		RouteMatrix:    map[string][]string{},
		Cases:          make([]bench.CaseResult, 0, len(set.Fixtures)),
	}
	var total float64
	for _, fixture := range set.Fixtures {
		caseResult := runLiveBenchCase(ctx, svc, fixture)
		report.Cases = append(report.Cases, caseResult)
		total += caseResult.Score
		if caseResult.SelectedProvider != "" {
			report.Summary.PassedCases++
		} else {
			report.Summary.FailedCases++
		}
	}
	report.Summary.TotalCases = len(report.Cases)
	if len(report.Cases) > 0 {
		report.Summary.AverageScore = float64(int((total/float64(len(report.Cases)))*100+0.5)) / 100
	}
	return report
}

func runLiveBenchCase(ctx context.Context, svc *core.Service, fixture bench.Fixture) bench.CaseResult {
	caseResult := bench.CaseResult{ID: fixture.ID, Task: fixture.Task, Kind: fixture.Kind, Language: fixture.Language, Category: fixture.Category}
	start := time.Now()
	if fixture.Kind == bench.KindExtract {
		resp, err := svc.Extract(ctx, core.ExtractRequest{URL: fixture.TargetURL, Format: "markdown"})
		latency := time.Since(start).Milliseconds()
		caseResult.Route = resp.Route
		caseResult.Attempts = attemptsFromTrace(resp.RouteTrace)
		if err != nil {
			caseResult.Attempts = append(caseResult.Attempts, bench.Attempt{Status: "failed", Reason: sanitizedBenchError(err), LatencyMS: latency})
			return caseResult
		}
		caseResult.SelectedProvider = resp.Provider
		caseResult.Score = liveScore(1, latency, resp.Content != "")
		return caseResult
	}
	resp, err := svc.Search(ctx, core.SearchRequest{Query: fixture.Query, Task: fixture.Task, Limit: 5})
	latency := time.Since(start).Milliseconds()
	caseResult.Route = resp.Route
	caseResult.Attempts = attemptsFromTrace(resp.RouteTrace)
	if err != nil {
		caseResult.Attempts = append(caseResult.Attempts, bench.Attempt{Status: "failed", Reason: sanitizedBenchError(err), LatencyMS: latency})
		return caseResult
	}
	caseResult.SelectedProvider = resp.Provider
	caseResult.Score = liveScore(len(resp.Results), latency, len(resp.Results) > 0)
	return caseResult
}

func attemptsFromTrace(trace []core.RouteAttempt) []bench.Attempt {
	attempts := make([]bench.Attempt, 0, len(trace))
	for _, a := range trace {
		attempts = append(attempts, bench.Attempt{Provider: a.Provider, Status: a.Status, Reason: a.Reason, LatencyMS: a.LatencyMS, ResultCount: a.ResultCount})
	}
	return attempts
}

func liveScore(resultCount int, latencyMS int64, success bool) float64 {
	if !success {
		return 0
	}
	score := 40.0
	if resultCount >= 3 {
		score += 30
	} else {
		score += float64(resultCount) * 10
	}
	if latencyMS <= 1000 {
		score += 30
	} else if latencyMS <= 3000 {
		score += 20
	} else if latencyMS <= 8000 {
		score += 10
	}
	if score > 100 {
		return 100
	}
	return score
}

func sanitizedBenchError(err error) string {
	if err == nil {
		return ""
	}
	msg := safeerr.Message(err)
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return msg
}
