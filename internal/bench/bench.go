package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

type Mode string

const (
	ModeOffline Mode = "offline"
	ModeLive    Mode = "live"
)

type Kind string

const (
	KindSearch  Kind = "search"
	KindExtract Kind = "extract"
)

type EvidenceMetadata struct {
	ArtifactKind       string   `json:"artifact_kind"`
	Methodology        string   `json:"methodology"`
	DataSource         string   `json:"data_source"`
	Measures           []string `json:"measures"`
	DoesNotMeasure     []string `json:"does_not_measure"`
	Reproduction       []string `json:"reproduction"`
	RawArtifactsPolicy string   `json:"raw_artifacts_policy"`
	NetworkRequired    bool     `json:"network_required"`
	SecretsRequired    bool     `json:"secrets_required"`
	CostPolicy         string   `json:"cost_policy,omitempty"`
	CostCaveat         string   `json:"cost_caveat,omitempty"`
	Sanitized          bool     `json:"sanitized"`
}

type FixtureSet struct {
	Version  string    `json:"version"`
	Fixtures []Fixture `json:"fixtures"`
}

type Fixture struct {
	ID        string        `json:"id"`
	Task      core.TaskType `json:"task"`
	Kind      Kind          `json:"kind"`
	Query     string        `json:"query,omitempty"`
	TargetURL string        `json:"target_url,omitempty"`
	Language  string        `json:"language"`
	Category  string        `json:"category"`
}

type Observation struct {
	Success             bool    `json:"success"`
	ResultCount         int     `json:"result_count"`
	Relevance           float64 `json:"relevance"`
	Freshness           float64 `json:"freshness"`
	CitationQuality     float64 `json:"citation_source_url_quality"`
	Diversity           float64 `json:"duplicate_result_diversity"`
	ExtractionSuccess   float64 `json:"extraction_success"`
	LatencyMS           int64   `json:"latency_ms"`
	EmptyResultBehavior float64 `json:"empty_result_behavior"`
	FallbackBehavior    float64 `json:"fallback_behavior"`
	ErrorHandling       float64 `json:"provider_error_handling"`
}

type Metrics struct {
	Relevance           float64 `json:"relevance"`
	Freshness           float64 `json:"freshness_currentness"`
	CitationQuality     float64 `json:"citation_source_url_quality"`
	Diversity           float64 `json:"duplicate_result_diversity"`
	ExtractionSuccess   float64 `json:"extraction_success"`
	Latency             float64 `json:"latency"`
	EmptyResultBehavior float64 `json:"empty_result_behavior"`
	FallbackBehavior    float64 `json:"fallback_behavior"`
	ErrorHandling       float64 `json:"provider_error_handling"`
}

type Attempt struct {
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	LatencyMS   int64  `json:"latency_ms,omitempty"`
	ResultCount int    `json:"result_count,omitempty"`
}

type CaseResult struct {
	ID               string        `json:"id"`
	Task             core.TaskType `json:"task"`
	Kind             Kind          `json:"kind"`
	Language         string        `json:"language"`
	Category         string        `json:"category"`
	Route            []string      `json:"route"`
	SelectedProvider string        `json:"selected_provider"`
	Attempts         []Attempt     `json:"attempts"`
	Metrics          Metrics       `json:"metrics"`
	Score            float64       `json:"score"`
}

type Summary struct {
	TotalCases   int     `json:"total_cases"`
	PassedCases  int     `json:"passed_cases"`
	FailedCases  int     `json:"failed_cases"`
	AverageScore float64 `json:"average_score"`
}

type Report struct {
	SchemaVersion  string              `json:"schema_version"`
	Mode           Mode                `json:"mode"`
	FixtureVersion string              `json:"fixture_version"`
	GeneratedAt    string              `json:"generated_at"`
	Evidence       EvidenceMetadata    `json:"evidence"`
	Summary        Summary             `json:"summary"`
	Cases          []CaseResult        `json:"cases"`
	RouteMatrix    map[string][]string `json:"route_matrix"`
}

func OfflineEvidenceMetadata() EvidenceMetadata {
	return EvidenceMetadata{
		ArtifactKind: "deterministic_fixture_eval",
		Methodology:  "Scores a versioned fixture set against deterministic observations and the configured route matrix; it makes no provider network calls.",
		DataSource:   "Repository fixtures plus deterministic in-code observations; not live provider data.",
		Measures: []string{
			"routing and fallback contract coverage",
			"fixture coverage by task and language",
			"deterministic selected-provider behavior for the route matrix",
		},
		DoesNotMeasure: []string{
			"live web result quality",
			"currentness of real provider indexes",
			"provider uptime or production availability",
			"actual cost/quota behavior or provider account balances",
			"statistically significant provider ranking",
		},
		Reproduction: []string{
			"go test ./internal/bench ./internal/cli",
			"nole bench --json",
			"nole bench --evidence-md",
		},
		RawArtifactsPolicy: "No raw provider payloads exist in offline mode; generated summaries are public-safe and fixture-only.",
		NetworkRequired:    false,
		SecretsRequired:    false,
		Sanitized:          true,
	}
}

func LiveEvidenceMetadata(costPolicy string, maxCases int) EvidenceMetadata {
	if maxCases <= 0 || maxCases > 10 {
		maxCases = 3
	}
	if strings.TrimSpace(costPolicy) == "" {
		costPolicy = string(core.CostPolicyFreeFirst)
	}
	return EvidenceMetadata{
		ArtifactKind: "live_smoke_summary",
		Methodology:  fmt.Sprintf("Runs at most %d explicit live fixture case(s) through the normal service/router and records only summarized route_trace outcomes.", maxCases),
		DataSource:   "Live provider calls made by the local user environment, summarized without raw response bodies.",
		Measures: []string{
			"success/failure count for this small fixture sample",
			"selected provider and sanitized route attempts",
			"result count and coarse latency buckets",
			"sanitized error categories",
		},
		DoesNotMeasure: []string{
			"comprehensive live web quality",
			"current provider ranking across all tasks",
			"statistical significance",
			"provider uptime guarantees",
			"actual provider billing beyond local estimates and account dashboards",
		},
		Reproduction: []string{
			fmt.Sprintf("NOLE_COST_POLICY=%s nole bench --live --max-live-cases %d --json", costPolicy, maxCases),
			fmt.Sprintf("NOLE_COST_POLICY=%s nole bench --live --max-live-cases %d --evidence-md", costPolicy, maxCases),
		},
		RawArtifactsPolicy: "Do not commit raw provider responses, headers, private queries, private URLs or credentials; publish only sanitized summaries.",
		NetworkRequired:    true,
		SecretsRequired:    false,
		CostPolicy:         costPolicy,
		CostCaveat:         "Live calls require network access and may use configured provider keys; they can consume quota or incur provider-account cost according to the provider dashboard and Nólë cost policy.",
		Sanitized:          true,
	}
}

func MarkdownEvidenceSummary(report Report) string {
	if report.Evidence.ArtifactKind == "" {
		report.Evidence = evidenceMetadataForMode(report.Mode, "", len(report.Cases))
	}
	generatedAt := report.GeneratedAt
	if generatedAt == "" {
		generatedAt = DeterministicTimestamp()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Route evidence summary %s\n\n", sanitizeMarkdownCell(generatedAt))
	fmt.Fprintf(&b, "Fixture version: %s\n", sanitizeMarkdownCell(report.FixtureVersion))
	fmt.Fprintf(&b, "Mode: %s\n", sanitizeMarkdownCell(string(report.Mode)))
	fmt.Fprintf(&b, "Artifact kind: %s\n", sanitizeMarkdownCell(report.Evidence.ArtifactKind))
	fmt.Fprintf(&b, "Private data: none included\n")
	fmt.Fprintf(&b, "Keys: presence/status only, no values\n")
	fmt.Fprintf(&b, "Network required: %t\n", report.Evidence.NetworkRequired)
	fmt.Fprintf(&b, "Secrets required: %t\n", report.Evidence.SecretsRequired)
	if report.Evidence.CostPolicy != "" {
		fmt.Fprintf(&b, "Cost policy: %s\n", sanitizeMarkdownCell(report.Evidence.CostPolicy))
	}
	fmt.Fprintf(&b, "\n## Methodology\n\n%s\n\n", sanitizeMarkdownCell(report.Evidence.Methodology))
	fmt.Fprintf(&b, "Data source: %s\n\n", sanitizeMarkdownCell(report.Evidence.DataSource))
	writeEvidenceList(&b, "Measures", report.Evidence.Measures)
	writeEvidenceList(&b, "Does not measure", report.Evidence.DoesNotMeasure)
	writeEvidenceList(&b, "Reproduction", report.Evidence.Reproduction)
	fmt.Fprintf(&b, "## Raw artifact policy\n\n%s\n\n", sanitizeMarkdownCell(report.Evidence.RawArtifactsPolicy))
	if report.Evidence.CostCaveat != "" {
		fmt.Fprintf(&b, "## Cost caveat\n\n%s\n\n", sanitizeMarkdownCell(report.Evidence.CostCaveat))
	}
	fmt.Fprintf(&b, "## Case summary\n\n")
	fmt.Fprintf(&b, "| task | provider | cases | success | result range | latency bucket | notes |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range evidenceRows(report) {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | %s |\n",
			sanitizeMarkdownCell(row.Task), sanitizeMarkdownCell(row.Provider), row.Cases, row.Successes,
			sanitizeMarkdownCell(row.ResultRange), sanitizeMarkdownCell(row.LatencyBucket), sanitizeMarkdownCell(row.Notes))
	}
	return b.String()
}

func evidenceMetadataForMode(mode Mode, costPolicy string, maxCases int) EvidenceMetadata {
	if mode == ModeLive {
		return LiveEvidenceMetadata(costPolicy, maxCases)
	}
	return OfflineEvidenceMetadata()
}

func writeEvidenceList(b *strings.Builder, title string, values []string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(values) == 0 {
		fmt.Fprintf(b, "- not recorded\n\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", sanitizeMarkdownCell(value))
	}
	fmt.Fprintf(b, "\n")
}

type evidenceRow struct {
	Task          string
	Provider      string
	Cases         int
	Successes     int
	ResultRange   string
	LatencyBucket string
	Notes         string
}

func evidenceRows(report Report) []evidenceRow {
	rows := make([]evidenceRow, 0, len(report.Cases))
	for _, c := range report.Cases {
		provider := c.SelectedProvider
		if provider == "" {
			provider = "none"
		}
		rows = append(rows, evidenceRow{
			Task:          string(c.Task),
			Provider:      provider,
			Cases:         1,
			Successes:     boolToInt(c.SelectedProvider != ""),
			ResultRange:   resultRange(c.Attempts),
			LatencyBucket: latencyBucket(maxLatency(c.Attempts)),
			Notes:         evidenceNotes(report.Mode, c.Attempts),
		})
	}
	return rows
}

func evidenceNotes(mode Mode, attempts []Attempt) string {
	if mode == ModeOffline {
		return "offline fixture summary; does not measure live web result quality"
	}
	if len(attempts) == 0 {
		return "no route attempts recorded"
	}
	reasons := make(map[string]bool)
	for _, attempt := range attempts {
		if attempt.Reason != "" {
			reasons[publicReason(attempt.Reason)] = true
		}
	}
	keys := make([]string, 0, len(reasons))
	for reason := range reasons {
		keys = append(keys, reason)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "live smoke summary"
	}
	return "live smoke summary: " + strings.Join(keys, ", ")
}

func publicReason(reason string) string {
	switch reason {
	case "success", "success_after_fallback", "empty_results", "provider_error", "extract_failed", "quota_blocked", "disabled_no_key", "premium_blocked_free_first", "cost_cap_exceeded", "unknown_cost_blocked", "within_cost_cap":
		return reason
	default:
		return "sanitized_error"
	}
}

func resultRange(attempts []Attempt) string {
	maxResults := 0
	for _, attempt := range attempts {
		if attempt.ResultCount > maxResults {
			maxResults = attempt.ResultCount
		}
	}
	return fmt.Sprintf("0-%d", maxResults)
}

func maxLatency(attempts []Attempt) int64 {
	var max int64
	for _, attempt := range attempts {
		if attempt.LatencyMS > max {
			max = attempt.LatencyMS
		}
	}
	return max
}

func latencyBucket(ms int64) string {
	switch {
	case ms <= 0:
		return "not-recorded"
	case ms <= 500:
		return "<=500ms"
	case ms <= 1000:
		return "<=1s"
	case ms <= 3000:
		return "<=3s"
	case ms <= 8000:
		return "<=8s"
	default:
		return ">8s"
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sanitizeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	for _, forbidden := range []string{"Authorization", "Bearer", "SECRET", "TOKEN", "token=", "api_key", "API_KEY"} {
		value = strings.ReplaceAll(value, forbidden, "[redacted]")
	}
	words := strings.Fields(value)
	for i, word := range words {
		trimmed := strings.Trim(word, "()[]{}<>,.;:'\"")
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			words[i] = strings.Replace(word, trimmed, "[redacted-url]", 1)
		}
		if strings.HasPrefix(trimmed, "/home/") || strings.HasPrefix(trimmed, "/Users/") {
			words[i] = strings.Replace(word, trimmed, "[redacted-path]", 1)
		}
	}
	if len(words) > 0 {
		value = strings.Join(words, " ")
	}
	return value
}

func DefaultFixtureSet() FixtureSet {
	return FixtureSet{
		Version: "2026-05-17.offline.v1",
		Fixtures: []Fixture{
			{ID: "general-web-en", Task: core.TaskGeneral, Kind: KindSearch, Query: "what is Model Context Protocol", Language: "en", Category: "general web search"},
			{ID: "recent-current-en", Task: core.TaskNews, Kind: KindSearch, Query: "latest Go stable release notes", Language: "en", Category: "recent/current info"},
			{ID: "docs-api-en", Task: core.TaskDocs, Kind: KindSearch, Query: "Go net/http Client Timeout documentation", Language: "en", Category: "docs/API lookup"},
			{ID: "github-release-en", Task: core.TaskCode, Kind: KindSearch, Query: "github cli latest release notes", Language: "en", Category: "GitHub issue/PR/release lookup"},
			{ID: "academic-technical-en", Task: core.TaskAcademic, Kind: KindSearch, Query: "retrieval augmented generation survey paper", Language: "en", Category: "academic/technical source lookup"},
			{ID: "factcheck-en", Task: core.TaskFactcheck, Kind: KindSearch, Query: "did NASA confirm alien life in 2025", Language: "en", Category: "fact-check style query"},
			{ID: "product-pricing-en", Task: core.TaskPricing, Kind: KindSearch, Query: "Cloudflare Workers pricing limits", Language: "en", Category: "product/pricing/status page lookup"},
			{ID: "people-company-en", Task: core.TaskPeople, Kind: KindSearch, Query: "who is the founder of Anthropic profile", Language: "en", Category: "people/company lookup"},
			{ID: "social-community-en", Task: core.TaskSocial, Kind: KindSearch, Query: "Reddit discussions about local LLM routers", Language: "en", Category: "social/community discussion"},
			{ID: "semantic-discovery-en", Task: core.TaskSemantic, Kind: KindSearch, Query: "semantic search tools for research discovery", Language: "en", Category: "semantic discovery"},
			{ID: "troubleshooting-en", Task: core.TaskDocs, Kind: KindSearch, Query: "Go http client timeout exceeded while awaiting headers", Language: "en", Category: "troubleshooting/error-message query"},
			{ID: "extract-doc-en", Task: core.TaskExtract, Kind: KindExtract, TargetURL: "https://go.dev/doc/", Language: "en", Category: "extraction/summarization target URL"},
			{ID: "general-web-tr", Task: core.TaskGeneral, Kind: KindSearch, Query: "Model Context Protocol nedir", Language: "tr", Category: "multilingual general web search"},
			{ID: "docs-api-tr", Task: core.TaskDocs, Kind: KindSearch, Query: "Go context paketi dokümantasyonu", Language: "tr", Category: "multilingual docs/API lookup"},
			{ID: "factcheck-tr", Task: core.TaskFactcheck, Kind: KindSearch, Query: "2025 yılında NASA uzaylı yaşamı doğruladı mı", Language: "tr", Category: "multilingual fact-check style query"},
			{ID: "pricing-es", Task: core.TaskPricing, Kind: KindSearch, Query: "precios de Vercel funciones edge", Language: "es", Category: "multilingual product/pricing lookup"},
			{ID: "docs-de", Task: core.TaskDocs, Kind: KindSearch, Query: "Python asyncio gather Dokumentation", Language: "de", Category: "multilingual docs/API lookup"},
		},
	}
}

func RunOffline(set FixtureSet, matrix core.RouteMatrix) Report {
	return RunOfflineWithObservations(set, matrix, defaultOfflineObservations())
}

func RunOfflineWithObservations(set FixtureSet, matrix core.RouteMatrix, observations map[string]map[core.TaskType]Observation) Report {
	report := Report{
		SchemaVersion:  "2",
		Mode:           ModeOffline,
		FixtureVersion: set.Version,
		GeneratedAt:    "deterministic-offline",
		Evidence:       OfflineEvidenceMetadata(),
		RouteMatrix:    stringifyMatrix(matrix),
		Cases:          make([]CaseResult, 0, len(set.Fixtures)),
	}
	var total float64
	for _, fixture := range set.Fixtures {
		caseResult := evalOfflineCase(fixture, matrix, observations)
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
		report.Summary.AverageScore = round2(total / float64(len(report.Cases)))
	}
	return report
}

func evalOfflineCase(fixture Fixture, matrix core.RouteMatrix, observations map[string]map[core.TaskType]Observation) CaseResult {
	route := append([]string(nil), matrix[fixture.Task]...)
	if len(route) == 0 {
		route = append([]string(nil), matrix[core.TaskGeneral]...)
	}
	result := CaseResult{
		ID:       fixture.ID,
		Task:     fixture.Task,
		Kind:     fixture.Kind,
		Language: fixture.Language,
		Category: fixture.Category,
		Route:    route,
	}
	for i, provider := range route {
		obs, ok := observationFor(observations, provider, fixture.Task)
		attempt := Attempt{Provider: provider, Status: "skipped", Reason: "no_fixture_observation"}
		if !ok {
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		attempt.Status = "failed"
		attempt.LatencyMS = obs.LatencyMS
		attempt.ResultCount = obs.ResultCount
		if !obs.Success {
			attempt.Reason = "provider_error"
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		if fixture.Kind == KindSearch && obs.ResultCount == 0 {
			attempt.Reason = "empty_results"
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		if fixture.Kind == KindExtract && obs.ExtractionSuccess <= 0 {
			attempt.Reason = "extract_failed"
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		attempt.Status = "success"
		attempt.Reason = "success"
		metrics := metricsFromObservation(obs, fixture.Kind)
		if i > 0 {
			attempt.Reason = "success_after_fallback"
			metrics.FallbackBehavior = 1
		}
		result.Attempts = append(result.Attempts, attempt)
		result.SelectedProvider = provider
		result.Metrics = metrics
		result.Score = scoreMetrics(metrics)
		return result
	}
	result.Score = 0
	return result
}

func observationFor(observations map[string]map[core.TaskType]Observation, provider string, task core.TaskType) (Observation, bool) {
	byTask, ok := observations[provider]
	if !ok {
		return Observation{}, false
	}
	obs, ok := byTask[task]
	if ok {
		return obs, true
	}
	obs, ok = byTask[core.TaskGeneral]
	return obs, ok
}

func metricsFromObservation(obs Observation, kind Kind) Metrics {
	empty := obs.EmptyResultBehavior
	if obs.ResultCount > 0 {
		empty = 1
	}
	extraction := obs.ExtractionSuccess
	if kind == KindSearch {
		extraction = 1
	}
	return Metrics{
		Relevance:           clamp01(obs.Relevance),
		Freshness:           clamp01(obs.Freshness),
		CitationQuality:     clamp01(obs.CitationQuality),
		Diversity:           clamp01(obs.Diversity),
		ExtractionSuccess:   clamp01(extraction),
		Latency:             latencyScore(obs.LatencyMS),
		EmptyResultBehavior: clamp01(empty),
		FallbackBehavior:    clamp01(obs.FallbackBehavior),
		ErrorHandling:       clamp01(obs.ErrorHandling),
	}
}

func scoreMetrics(m Metrics) float64 {
	values := []float64{m.Relevance, m.Freshness, m.CitationQuality, m.Diversity, m.ExtractionSuccess, m.Latency, m.EmptyResultBehavior, m.FallbackBehavior, m.ErrorHandling}
	var total float64
	for _, v := range values {
		total += v
	}
	return round2((total / float64(len(values))) * 100)
}

func latencyScore(ms int64) float64 {
	switch {
	case ms <= 0:
		return 0
	case ms <= 500:
		return 1
	case ms <= 1000:
		return 0.8
	case ms <= 2000:
		return 0.6
	case ms <= 5000:
		return 0.4
	default:
		return 0.2
	}
}

func defaultOfflineObservations() map[string]map[core.TaskType]Observation {
	return map[string]map[core.TaskType]Observation{
		"brave": {
			core.TaskGeneral:   obs(5, .92, .80, .92, .85, 480),
			core.TaskNews:      obs(5, .90, .95, .90, .82, 520),
			core.TaskDocs:      obs(5, .88, .80, .90, .80, 510),
			core.TaskAcademic:  obs(5, .86, .72, .86, .80, 530),
			core.TaskFactcheck: obs(5, .88, .82, .90, .86, 500),
			core.TaskCode:      obs(5, .89, .78, .90, .82, 490),
			core.TaskPricing:   obs(5, .91, .90, .92, .84, 500),
			core.TaskResearch:  obs(5, .90, .84, .91, .84, 520),
		},
		"firecrawl": {
			core.TaskGeneral:  obs(5, .86, .76, .86, .80, 850),
			core.TaskDocs:     obs(5, .88, .82, .88, .78, 820),
			core.TaskCode:     obs(5, .87, .80, .86, .78, 830),
			core.TaskSocial:   obs(5, .88, .84, .84, .76, 900),
			core.TaskResearch: obs(5, .84, .80, .84, .78, 880),
			core.TaskExtract:  extractObs(.88, .80, .88, .78, 1200),
		},
		"tavily": {
			core.TaskGeneral:   obs(5, .82, .82, .84, .78, 1100),
			core.TaskSemantic:  obs(5, .91, .82, .86, .82, 1100),
			core.TaskPeople:    obs(5, .90, .86, .88, .80, 1050),
			core.TaskFactcheck: obs(5, .86, .84, .86, .82, 1150),
			core.TaskPricing:   obs(5, .86, .86, .86, .78, 1150),
			core.TaskAcademic:  obs(5, .84, .76, .84, .78, 1200),
			core.TaskExtract:   extractObs(.90, .82, .90, .80, 1000),
		},
		"ddgs": {
			core.TaskGeneral:   obs(5, .78, .74, .76, .72, 900),
			core.TaskNews:      obs(5, .82, .86, .78, .72, 930),
			core.TaskDocs:      obs(5, .76, .70, .74, .70, 920),
			core.TaskCode:      obs(5, .78, .72, .76, .72, 920),
			core.TaskFactcheck: obs(5, .78, .76, .76, .74, 940),
			core.TaskPricing:   obs(5, .78, .80, .78, .72, 930),
			core.TaskResearch:  obs(5, .76, .74, .76, .72, 940),
		},
		"jina": {
			core.TaskGeneral:   obs(4, .70, .68, .72, .70, 1800),
			core.TaskDocs:      obs(4, .74, .70, .74, .70, 1700),
			core.TaskAcademic:  obs(4, .72, .68, .74, .70, 1750),
			core.TaskFactcheck: obs(4, .70, .68, .72, .70, 1800),
			core.TaskPricing:   obs(4, .70, .70, .72, .70, 1800),
			core.TaskExtract:   extractObs(.86, .76, .86, .74, 900),
		},
	}
}

func obs(count int, relevance, freshness, citation, diversity float64, latency int64) Observation {
	return Observation{Success: true, ResultCount: count, Relevance: relevance, Freshness: freshness, CitationQuality: citation, Diversity: diversity, ExtractionSuccess: 1, LatencyMS: latency, EmptyResultBehavior: 1, ErrorHandling: .8}
}

func extractObs(relevance, freshness, citation, diversity float64, latency int64) Observation {
	return Observation{Success: true, ResultCount: 1, Relevance: relevance, Freshness: freshness, CitationQuality: citation, Diversity: diversity, ExtractionSuccess: 1, LatencyMS: latency, EmptyResultBehavior: 1, ErrorHandling: .8}
}

func stringifyMatrix(matrix core.RouteMatrix) map[string][]string {
	out := make(map[string][]string, len(matrix))
	keys := make([]string, 0, len(matrix))
	for task := range matrix {
		keys = append(keys, string(task))
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = append([]string(nil), matrix[core.TaskType(key)]...)
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func DeterministicTimestamp() string {
	return time.Unix(0, 0).UTC().Format(time.RFC3339)
}
