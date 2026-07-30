package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

// ComprehensiveOptions tune the comprehensive run. Defaults aim at a polite,
// low-burst probe: a generous per-call timeout, a short inter-call spacing per
// provider, and no fixture cap.
type ComprehensiveOptions struct {
	MaxFixtures              int                      // 0 = all
	PerCallTimeout           time.Duration            // default 30s
	InterCallSpacing         time.Duration            // default 250ms per provider serial
	ProviderInterCallSpacing map[string]time.Duration // optional provider-specific floor
	NetworkContext           string                   // free-text annotation for the artifact
	CostPolicy               string                   // metadata-only; comprehensive mode bypasses policy
}

func (o ComprehensiveOptions) normalize() ComprehensiveOptions {
	if o.PerCallTimeout <= 0 {
		o.PerCallTimeout = 30 * time.Second
	}
	if o.InterCallSpacing < 0 {
		o.InterCallSpacing = 0
	}
	if o.InterCallSpacing == 0 {
		o.InterCallSpacing = 250 * time.Millisecond
	}
	return o
}

func (o ComprehensiveOptions) spacingFor(provider string) time.Duration {
	spacing := o.InterCallSpacing
	if floor := o.ProviderInterCallSpacing[provider]; floor > spacing {
		spacing = floor
	}
	return spacing
}

// RunComprehensiveLive exercises every (provider, fixture) pair the providers'
// declared capabilities permit. Each provider runs its fixtures serially (so
// the inter-call delay is meaningful for rate-limit-prone backends like DDGS),
// but providers run concurrently against each other.
//
// The router, cost policy and quota ledger are intentionally NOT consulted;
// the goal is to measure providers in isolation, not to evaluate routing.
func RunComprehensiveLive(ctx context.Context, set FixtureSet, providers map[string]core.Provider, opts ComprehensiveOptions) Report {
	opts = opts.normalize()

	fixtures := set.Fixtures
	if opts.MaxFixtures > 0 && opts.MaxFixtures < len(fixtures) {
		fixtures = fixtures[:opts.MaxFixtures]
	}

	type provBatch struct {
		name string
		out  []Measurement
	}
	batches := make(chan provBatch, len(providers))

	var wg sync.WaitGroup
	for name, p := range providers {
		name, p := name, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]Measurement, 0, len(fixtures))
			for _, fx := range fixtures {
				want := core.CapabilitySearch
				if fx.Kind == KindExtract {
					want = core.CapabilityExtract
				}
				if !core.HasCapability(p.Capabilities(), want) {
					continue
				}
				m := runComprehensiveOne(ctx, name, p, fx, opts.PerCallTimeout)
				local = append(local, m)
				spacing := opts.spacingFor(name)
				if spacing > 0 {
					select {
					case <-ctx.Done():
					case <-time.After(spacing):
					}
				}
				if ctx.Err() != nil {
					break
				}
			}
			batches <- provBatch{name: name, out: local}
		}()
	}
	wg.Wait()
	close(batches)

	// Flatten in deterministic order (alphabetical by provider) so JSON output
	// diffs cleanly across runs.
	byProv := make(map[string][]Measurement, len(providers))
	for b := range batches {
		byProv[b.name] = b.out
	}
	names := make([]string, 0, len(byProv))
	for n := range byProv {
		names = append(names, n)
	}
	sort.Strings(names)
	all := make([]Measurement, 0, len(fixtures)*len(providers))
	for _, n := range names {
		all = append(all, byProv[n]...)
	}

	report := Report{
		SchemaVersion:   "2",
		Mode:            ModeComprehensiveLive,
		FixtureVersion:  set.Version,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Evidence:        ComprehensiveLiveEvidenceMetadata(opts.CostPolicy, opts.MaxFixtures),
		Measurements:    all,
		ProviderSummary: summarizeMeasurements(all),
		NetworkContext:  opts.NetworkContext,
		RouteMatrix:     map[string][]string{},
	}
	report.Summary = comprehensiveSummary(all)
	return report
}

func runComprehensiveOne(parent context.Context, name string, p core.Provider, fx Fixture, timeout time.Duration) Measurement {
	m := Measurement{
		Provider:           name,
		Task:               fx.Task,
		FixtureID:          fx.ID,
		Language:           fx.Language,
		Kind:               fx.Kind,
		ExpectedErrorClass: fx.ExpectedErrorClass,
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	start := time.Now()

	if fx.Kind == KindExtract {
		resp, err := p.Extract(ctx, core.ExtractRequest{URL: fx.TargetURL, Format: "markdown"})
		m.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			m.ErrorClass = classifyComprehensiveFixtureError(err, m.ExpectedErrorClass)
			m.Success = m.ExpectedErrorClass != "" && m.ErrorClass == m.ExpectedErrorClass
			return m
		}
		if m.ExpectedErrorClass != "" {
			m.ErrorClass = "unexpected_success"
			return m
		}
		if strings.TrimSpace(resp.Content) == "" {
			m.ErrorClass = "empty_content"
			return m
		}
		m.Success = true
		m.ResultCount = 1
		return m
	}

	resp, err := p.Search(ctx, core.SearchRequest{
		Query: fx.Query,
		Task:  fx.Task,
		Limit: 5,
		Options: core.SearchOptions{
			Country:    fx.Country,
			SearchLang: fx.SearchLang,
			Freshness:  fx.Freshness,
		},
	})
	m.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		m.ErrorClass = classifyComprehensiveFixtureError(err, m.ExpectedErrorClass)
		m.Success = m.ExpectedErrorClass != "" && m.ErrorClass == m.ExpectedErrorClass
		return m
	}
	if m.ExpectedErrorClass != "" {
		m.ErrorClass = "unexpected_success"
		return m
	}
	m.ResultCount = len(resp.Results)
	if m.ResultCount == 0 {
		m.ErrorClass = "empty_results"
		return m
	}
	m.Success = true
	return m
}

// classifyComprehensiveError reduces provider error messages to a small,
// sanitized vocabulary suitable for evidence artifacts. It deliberately does
// not preserve the original message — that may contain URLs, payload
// fragments or key material in pathological cases.
func classifyComprehensiveError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, io.EOF) {
		return "eof"
	}
	// Structured detection first: providerhttp.HTTPStatusError carries the real
	// status code, so we don't depend on the message format remaining stable.
	// String matching falls through to it for plain fmt.Errorf paths.
	var statusErr *providerhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		switch {
		case statusErr.StatusCode == 401:
			return "auth_unauthorized"
		case statusErr.StatusCode == 403:
			return "auth_forbidden"
		case statusErr.StatusCode == 404:
			return "not_found"
		case statusErr.StatusCode == 429 || statusErr.StatusCode == 202:
			return "rate_limited"
		case statusErr.StatusCode >= 500:
			return "provider_5xx"
		}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limited") || strings.Contains(msg, "too many requests") || hasHTTPStatusMarker(msg, "429") || hasHTTPStatusMarker(msg, "202"):
		return "rate_limited"
	case strings.Contains(msg, "api key") || strings.Contains(msg, "apikey") || strings.Contains(msg, "not set"):
		return "auth_missing_key"
	case strings.Contains(msg, "unauthorized") || hasHTTPStatusMarker(msg, "401"):
		return "auth_unauthorized"
	case strings.Contains(msg, "forbidden") || hasHTTPStatusMarker(msg, "403"):
		return "auth_forbidden"
	case strings.Contains(msg, "not found") || hasHTTPStatusMarker(msg, "404"):
		return "not_found"
	// providerhttp emits "returned HTTP 5xx (...)" rather than "status 5xx";
	// keep both spellings so plain fmt.Errorf strings keep classifying too.
	case strings.Contains(msg, "returned http 5") || strings.Contains(msg, "status 5"):
		return "provider_5xx"
	case strings.Contains(msg, "context") && strings.Contains(msg, "canceled"):
		return "canceled"
	case strings.Contains(msg, "dial") || strings.Contains(msg, "no route") || strings.Contains(msg, "network"):
		return "network"
	default:
		return "provider_error"
	}
}

func hasHTTPStatusMarker(msg, code string) bool {
	return strings.Contains(msg, "returned http "+code) ||
		strings.Contains(msg, "http status "+code) ||
		strings.Contains(msg, "status "+code)
}

func classifyComprehensiveFixtureError(err error, expected string) string {
	class := classifyComprehensiveError(err)
	if expected != "not_found" || class != "provider_error" {
		return class
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "(page_not_found)") {
		return "not_found"
	}
	return class
}

func summarizeMeasurements(ms []Measurement) map[string]ProviderStat {
	byProv := map[string][]Measurement{}
	for _, m := range ms {
		byProv[m.Provider] = append(byProv[m.Provider], m)
	}
	out := make(map[string]ProviderStat, len(byProv))
	for prov, list := range byProv {
		stat := ProviderStat{
			ErrorClasses: map[string]int{},
			PerTaskCalls: map[string]int{},
		}
		var succLat []int64
		var totalResults int
		for _, m := range list {
			stat.Calls++
			stat.PerTaskCalls[string(m.Task)]++
			if m.Success {
				stat.Successes++
				totalResults += m.ResultCount
				if m.LatencyMS > 0 {
					succLat = append(succLat, m.LatencyMS)
				}
			} else {
				stat.Failures++
				if m.ErrorClass != "" {
					stat.ErrorClasses[m.ErrorClass]++
				}
			}
		}
		if len(succLat) > 0 {
			sort.Slice(succLat, func(i, j int) bool { return succLat[i] < succLat[j] })
			var sum int64
			for _, v := range succLat {
				sum += v
			}
			stat.AvgLatencyMS = sum / int64(len(succLat))
			stat.P50LatencyMS = succLat[len(succLat)/2]
			// Nearest-rank percentile: index = ceil(p/100 * N) - 1, clamped.
			// The earlier (N*95)/100 formula was off-by-one for zero-based
			// indexing — e.g. with N=20 it picked succLat[19] (the max)
			// instead of the 95th-percentile rank at index 18, inflating
			// reported P95. The +99 forces ceil to land on the right rank.
			p95idx := (len(succLat)*95+99)/100 - 1
			if p95idx < 0 {
				p95idx = 0
			}
			if p95idx >= len(succLat) {
				p95idx = len(succLat) - 1
			}
			stat.P95LatencyMS = succLat[p95idx]
		}
		if stat.Successes > 0 {
			stat.AvgResults = float64(totalResults) / float64(stat.Successes)
		}
		out[prov] = stat
	}
	return out
}

func comprehensiveSummary(ms []Measurement) Summary {
	s := Summary{TotalCases: len(ms)}
	for _, m := range ms {
		if m.Success {
			s.PassedCases++
		} else {
			s.FailedCases++
		}
	}
	return s
}

// MarkdownComprehensiveSummary renders a sanitized public-safe Markdown
// summary of a comprehensive run, mirroring the format of the legacy
// MarkdownEvidenceSummary but emitting one row per (provider, task) bucket.
func MarkdownComprehensiveSummary(report Report) string {
	generatedAt := report.GeneratedAt
	if generatedAt == "" {
		generatedAt = DeterministicTimestamp()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Comprehensive route evidence summary %s\n\n", sanitizeMarkdownCell(generatedAt))
	fmt.Fprintf(&b, "Fixture version: %s\n", sanitizeMarkdownCell(report.FixtureVersion))
	fmt.Fprintf(&b, "Mode: %s\n", sanitizeMarkdownCell(string(report.Mode)))
	fmt.Fprintf(&b, "Artifact kind: %s\n", sanitizeMarkdownCell(report.Evidence.ArtifactKind))
	fmt.Fprintf(&b, "Private data: none included\n")
	fmt.Fprintf(&b, "Keys: presence/status only, no values\n")
	fmt.Fprintf(&b, "Network required: %t\n", report.Evidence.NetworkRequired)
	fmt.Fprintf(&b, "Secrets required: %t\n", report.Evidence.SecretsRequired)
	if report.NetworkContext != "" {
		fmt.Fprintf(&b, "Network context: %s\n", sanitizeMarkdownCell(report.NetworkContext))
	}
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

	fmt.Fprintf(&b, "## Provider aggregate\n\n")
	fmt.Fprintf(&b, "| provider | calls | success | failures | p50 (ok) | p95 (ok) | avg results | top errors |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
	provs := make([]string, 0, len(report.ProviderSummary))
	for n := range report.ProviderSummary {
		provs = append(provs, n)
	}
	sort.Strings(provs)
	for _, n := range provs {
		s := report.ProviderSummary[n]
		errs := topErrorClasses(s.ErrorClasses, 3)
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %.1f | %s |\n",
			sanitizeMarkdownCell(n), s.Calls, s.Successes, s.Failures,
			s.P50LatencyMS, s.P95LatencyMS, s.AvgResults, sanitizeMarkdownCell(errs))
	}

	fmt.Fprintf(&b, "\n## Per-(provider, task) bucket\n\n")
	fmt.Fprintf(&b, "| provider | task | calls | success | latency bucket (ok) | notes |\n")
	fmt.Fprintf(&b, "| --- | --- | ---: | ---: | --- | --- |\n")
	for _, row := range comprehensiveRows(report) {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s |\n",
			sanitizeMarkdownCell(row.Provider), sanitizeMarkdownCell(row.Task),
			row.Calls, row.Successes, sanitizeMarkdownCell(row.LatencyBucket),
			sanitizeMarkdownCell(row.Notes))
	}
	return b.String()
}

type comprehensiveRow struct {
	Provider      string
	Task          string
	Calls         int
	Successes     int
	LatencyBucket string
	Notes         string
}

func comprehensiveRows(report Report) []comprehensiveRow {
	type key struct {
		Provider string
		Task     string
	}
	groups := map[key][]Measurement{}
	for _, m := range report.Measurements {
		groups[key{m.Provider, string(m.Task)}] = append(groups[key{m.Provider, string(m.Task)}], m)
	}
	rows := make([]comprehensiveRow, 0, len(groups))
	for k, list := range groups {
		row := comprehensiveRow{Provider: k.Provider, Task: k.Task, Calls: len(list)}
		var succLat int64
		errs := map[string]int{}
		for _, m := range list {
			if m.Success {
				row.Successes++
				if m.LatencyMS > succLat {
					succLat = m.LatencyMS
				}
			} else if m.ErrorClass != "" {
				errs[m.ErrorClass]++
			}
		}
		row.LatencyBucket = latencyBucket(succLat)
		if row.Successes == 0 {
			row.LatencyBucket = "n/a"
		}
		row.Notes = comprehensiveNotes(row.Calls, row.Successes, errs)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}
		return rows[i].Task < rows[j].Task
	})
	return rows
}

func comprehensiveNotes(calls, succ int, errs map[string]int) string {
	if calls == succ {
		return "all success"
	}
	if succ == 0 && len(errs) > 0 {
		return "all failed: " + topErrorClasses(errs, 2)
	}
	if len(errs) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d success; top: %s", succ, calls, topErrorClasses(errs, 2))
}

func topErrorClasses(m map[string]int, limit int) string {
	if len(m) == 0 {
		return ""
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s×%d", p.k, p.v))
	}
	return strings.Join(parts, ", ")
}
