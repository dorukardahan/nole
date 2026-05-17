package bench

import (
	"sort"
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
	Summary        Summary             `json:"summary"`
	Cases          []CaseResult        `json:"cases"`
	RouteMatrix    map[string][]string `json:"route_matrix"`
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
		SchemaVersion:  "1",
		Mode:           ModeOffline,
		FixtureVersion: set.Version,
		GeneratedAt:    "deterministic-offline",
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
