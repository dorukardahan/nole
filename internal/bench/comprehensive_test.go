package bench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

// fakeProvider is a controllable provider for comprehensive-mode tests. It
// reports a fixed capability set and returns either canned results or a canned
// error so each branch of the runner is exercisable without network.
type fakeProvider struct {
	name       string
	caps       []core.Capability
	searchErr  error
	extractErr error
	results    int
	latency    time.Duration
	emptyExtr  bool
}

func (p fakeProvider) Name() string                    { return p.name }
func (p fakeProvider) Capabilities() []core.Capability { return p.caps }
func (p fakeProvider) Status(ctx context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: p.name, Available: true, Capabilities: p.caps}
}
func (p fakeProvider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if p.latency > 0 {
		time.Sleep(p.latency)
	}
	if p.searchErr != nil {
		return core.SearchResponse{}, p.searchErr
	}
	results := make([]core.SearchResult, 0, p.results)
	for i := 0; i < p.results; i++ {
		results = append(results, core.SearchResult{Provider: p.name})
	}
	return core.SearchResponse{Provider: p.name, Results: results}, nil
}
func (p fakeProvider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	if p.latency > 0 {
		time.Sleep(p.latency)
	}
	if p.extractErr != nil {
		return core.ExtractResponse{}, p.extractErr
	}
	if p.emptyExtr {
		return core.ExtractResponse{Provider: p.name, Content: ""}, nil
	}
	return core.ExtractResponse{Provider: p.name, Content: "ok"}, nil
}

func TestRunComprehensiveLive_BypassesRouterAndFiltersByCapability(t *testing.T) {
	// search-only provider must skip the extract fixture; extract-only provider
	// must skip search fixtures. Both rules together prove the runner consults
	// declared capabilities rather than calling every provider blindly.
	providers := map[string]core.Provider{
		"search-only":  fakeProvider{name: "search-only", caps: []core.Capability{core.CapabilitySearch}, results: 3},
		"extract-only": fakeProvider{name: "extract-only", caps: []core.Capability{core.CapabilityExtract}},
	}
	set := FixtureSet{
		Version: "test.v1",
		Fixtures: []Fixture{
			{ID: "s1", Task: core.TaskGeneral, Kind: KindSearch, Query: "x"},
			{ID: "e1", Task: core.TaskExtract, Kind: KindExtract, TargetURL: "https://example.com"},
		},
	}
	rep := RunComprehensiveLive(context.Background(), set, providers, ComprehensiveOptions{InterCallSpacing: 1 * time.Millisecond})
	if rep.Mode != ModeComprehensiveLive {
		t.Fatalf("mode = %q, want comprehensive_live", rep.Mode)
	}
	if len(rep.Measurements) != 2 {
		t.Fatalf("got %d measurements, want 2", len(rep.Measurements))
	}
	for _, m := range rep.Measurements {
		switch {
		case m.Provider == "search-only" && m.Kind != KindSearch:
			t.Errorf("search-only ran %s fixture", m.Kind)
		case m.Provider == "extract-only" && m.Kind != KindExtract:
			t.Errorf("extract-only ran %s fixture", m.Kind)
		}
	}
}

func TestRunComprehensiveLive_RecordsSuccessAndError(t *testing.T) {
	good := fakeProvider{name: "good", caps: []core.Capability{core.CapabilitySearch}, results: 5}
	bad := fakeProvider{name: "bad", caps: []core.Capability{core.CapabilitySearch}, searchErr: errors.New("rate limited (status 202)")}
	empty := fakeProvider{name: "empty", caps: []core.Capability{core.CapabilitySearch}, results: 0}
	providers := map[string]core.Provider{"good": good, "bad": bad, "empty": empty}
	set := FixtureSet{Version: "test.v1", Fixtures: []Fixture{{ID: "s1", Task: core.TaskGeneral, Kind: KindSearch, Query: "x"}}}
	rep := RunComprehensiveLive(context.Background(), set, providers, ComprehensiveOptions{InterCallSpacing: 1 * time.Millisecond})
	if rep.Summary.PassedCases != 1 || rep.Summary.FailedCases != 2 {
		t.Fatalf("summary passed/failed = %d/%d, want 1/2", rep.Summary.PassedCases, rep.Summary.FailedCases)
	}
	byProv := map[string]Measurement{}
	for _, m := range rep.Measurements {
		byProv[m.Provider] = m
	}
	if !byProv["good"].Success || byProv["good"].ResultCount != 5 {
		t.Errorf("good measurement = %#v", byProv["good"])
	}
	if byProv["bad"].Success || byProv["bad"].ErrorClass != "rate_limited" {
		t.Errorf("bad should classify as rate_limited, got %#v", byProv["bad"])
	}
	if byProv["empty"].Success || byProv["empty"].ErrorClass != "empty_results" {
		t.Errorf("empty should classify as empty_results, got %#v", byProv["empty"])
	}
}

func TestRunComprehensiveLive_ExpectedNotFoundScoresContractFidelity(t *testing.T) {
	providers := map[string]core.Provider{
		"correct": fakeProvider{
			name:       "correct",
			caps:       []core.Capability{core.CapabilityExtract},
			extractErr: providerhttp.NewHTTPStatusError("correct", "extract", 404, nil),
		},
		"swallows-error": fakeProvider{
			name: "swallows-error",
			caps: []core.Capability{core.CapabilityExtract},
		},
	}
	set := FixtureSet{Version: "test.v1", Fixtures: []Fixture{{
		ID:                 "expected-404",
		Task:               core.TaskExtract,
		Kind:               KindExtract,
		TargetURL:          "https://example.com/missing",
		ExpectedErrorClass: "not_found",
	}}}
	rep := RunComprehensiveLive(context.Background(), set, providers, ComprehensiveOptions{InterCallSpacing: time.Millisecond})
	byProvider := make(map[string]Measurement, len(rep.Measurements))
	for _, measurement := range rep.Measurements {
		byProvider[measurement.Provider] = measurement
	}
	correct := byProvider["correct"]
	if !correct.Success || correct.ExpectedErrorClass != "not_found" || correct.ErrorClass != "not_found" {
		t.Fatalf("expected 404 contract match = %#v", correct)
	}
	swallowed := byProvider["swallows-error"]
	if swallowed.Success || swallowed.ExpectedErrorClass != "not_found" || swallowed.ErrorClass != "unexpected_success" {
		t.Fatalf("swallowed 404 must fail contract probe: %#v", swallowed)
	}
	if rep.ProviderSummary["correct"].Successes != 1 || rep.ProviderSummary["correct"].Failures != 0 {
		t.Fatalf("correct aggregate = %#v", rep.ProviderSummary["correct"])
	}
	if rep.ProviderSummary["swallows-error"].Successes != 0 || rep.ProviderSummary["swallows-error"].Failures != 1 {
		t.Fatalf("swallowed aggregate = %#v", rep.ProviderSummary["swallows-error"])
	}
}

func TestRunComprehensiveLive_ExpectedNotFoundNormalizesProviderNativeClasses(t *testing.T) {
	for _, code := range []string{"page_not_found", "target_http_error"} {
		t.Run(code, func(t *testing.T) {
			provider := fakeProvider{
				name:       "tinyfish",
				caps:       []core.Capability{core.CapabilityExtract},
				extractErr: fmt.Errorf("tinyfish: fetch failed (%s)", code),
			}
			fixture := Fixture{
				ID:                 "expected-404",
				Task:               core.TaskExtract,
				Kind:               KindExtract,
				TargetURL:          "https://example.com/missing",
				ExpectedErrorClass: "not_found",
			}
			measurement := runComprehensiveOne(context.Background(), provider.name, provider, fixture, time.Second)
			if !measurement.Success || measurement.ErrorClass != "not_found" {
				t.Fatalf("provider-native %s measurement = %#v", code, measurement)
			}
		})
	}
}

func TestSanitizationOfMeasurementFields(t *testing.T) {
	// Even though the provider returns SearchResults containing URLs and
	// snippets, the Measurement must surface only counts/latency/classification
	// — no URL, title or snippet may leak through.
	p := fakeProvider{name: "p", caps: []core.Capability{core.CapabilitySearch}, results: 1}
	set := FixtureSet{Version: "test.v1", Fixtures: []Fixture{{ID: "s1", Task: core.TaskGeneral, Kind: KindSearch, Query: "secret-query-value"}}}
	rep := RunComprehensiveLive(context.Background(), set, map[string]core.Provider{"p": p}, ComprehensiveOptions{})
	if len(rep.Measurements) != 1 {
		t.Fatalf("want 1 measurement, got %d", len(rep.Measurements))
	}
	for _, m := range rep.Measurements {
		// Reflect-ish field check: render the value as text and scan for forbidden
		// substrings. If any future field accidentally captures payload we want
		// the test to fail loudly.
		text := m.Provider + ":" + string(m.Task) + ":" + m.FixtureID + ":" + m.Language + ":" + string(m.Kind) + ":" + m.ErrorClass
		for _, bad := range []string{"http://", "https://", "secret-query-value", "example.com", "snippet"} {
			if strings.Contains(strings.ToLower(text), strings.ToLower(bad)) {
				t.Errorf("measurement leaks %q: %#v", bad, m)
			}
		}
	}
}

func TestClassifyComprehensiveError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{context.DeadlineExceeded, "timeout"},
		{errors.New("rate limited (status 202): blocked"), "rate_limited"},
		{errors.New("ddgs: rate limited (status 202)"), "rate_limited"},
		{errors.New("upstream 429 Too Many Requests"), "rate_limited"},
		{errors.New("brave: BRAVE_API_KEY not set"), "auth_missing_key"},
		{errors.New("status 401 unauthorized"), "auth_unauthorized"},
		{errors.New("status 403 forbidden"), "auth_forbidden"},
		{errors.New("dial tcp: connection refused"), "network"},
		{errors.New("status 500 internal server error"), "provider_5xx"},
		{errors.New("something weird went wrong"), "provider_error"},
		// Real provider HTTP errors come from providerhttp.NewHTTPStatusError,
		// which emits "returned HTTP <code>" — the original "status 5" check
		// missed this shape, mislabeling real 5xx as provider_error. Structured
		// detection via errors.As must take precedence and pick the right class
		// straight from the status code regardless of message wording.
		{providerhttp.NewHTTPStatusError("brave", "search", 500, nil), "provider_5xx"},
		{providerhttp.NewHTTPStatusError("brave", "search", 503, nil), "provider_5xx"},
		{providerhttp.NewHTTPStatusError("brave", "search", 401, nil), "auth_unauthorized"},
		{providerhttp.NewHTTPStatusError("brave", "search", 403, nil), "auth_forbidden"},
		{providerhttp.NewHTTPStatusError("brave", "search", 429, nil), "rate_limited"},
		{providerhttp.NewHTTPStatusError("brave", "search", 202, nil), "rate_limited"},
		// The string form providerhttp.HTTPStatusError.Error() produces must
		// still classify correctly when it reaches the fallback path (e.g.
		// after passing through safeerr.Message into a fresh fmt.Errorf).
		{errors.New("brave: search returned HTTP 502 (transient; response body redacted, 0 bytes)"), "provider_5xx"},
	}
	for _, tc := range cases {
		got := classifyComprehensiveError(tc.err)
		if got != tc.want {
			t.Errorf("classify(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestSummarizeMeasurementsLatencyOnSuccessesOnly(t *testing.T) {
	// Failures often complete in single-digit ms (DDG returns 202 immediately).
	// If they were included in latency stats the p50 would collapse toward the
	// failure floor and obscure the real success-path latency. The summary
	// must use successful calls only.
	ms := []Measurement{
		{Provider: "p", Success: true, LatencyMS: 100, Task: core.TaskGeneral},
		{Provider: "p", Success: true, LatencyMS: 200, Task: core.TaskGeneral},
		{Provider: "p", Success: true, LatencyMS: 300, Task: core.TaskGeneral},
		{Provider: "p", Success: false, LatencyMS: 5, ErrorClass: "rate_limited", Task: core.TaskGeneral},
		{Provider: "p", Success: false, LatencyMS: 5, ErrorClass: "rate_limited", Task: core.TaskNews},
	}
	stats := summarizeMeasurements(ms)
	got := stats["p"]
	if got.Calls != 5 || got.Successes != 3 || got.Failures != 2 {
		t.Fatalf("counts wrong: %#v", got)
	}
	if got.P50LatencyMS == 5 {
		t.Fatalf("p50 = 5 indicates failures contaminated latency; got %d", got.P50LatencyMS)
	}
	if got.P50LatencyMS < 100 || got.P50LatencyMS > 300 {
		t.Fatalf("p50 = %d, expected in [100, 300]", got.P50LatencyMS)
	}
	if got.ErrorClasses["rate_limited"] != 2 {
		t.Fatalf("error classes = %v", got.ErrorClasses)
	}
}

func TestSummarizeMeasurementsP95UsesNearestRank(t *testing.T) {
	// The original (N*95)/100 formula selected index 19 for N=20 — the max,
	// not the 95th-percentile rank. Nearest-rank percentile lands on index 18
	// (the 19th sorted sample). The boundary is what makes cross-provider
	// latency comparisons meaningful, so pin the exact index here.
	ms := make([]Measurement, 0, 20)
	for i := 1; i <= 20; i++ {
		ms = append(ms, Measurement{Provider: "p", Success: true, LatencyMS: int64(i * 10), Task: core.TaskGeneral})
	}
	stats := summarizeMeasurements(ms)
	got := stats["p"]
	if got.P95LatencyMS != 190 {
		t.Errorf("p95 (N=20) = %d, want 190 (index 18 nearest-rank), not %d (index 19, the max)", got.P95LatencyMS, got.P95LatencyMS)
	}
	if got.P95LatencyMS == 200 {
		t.Errorf("p95 collapsed to max (200) — off-by-one regression in (N*95)/100")
	}
	// Small-N sanity: with N=1, p95 must still be defined and equal the only sample.
	one := summarizeMeasurements([]Measurement{{Provider: "p", Success: true, LatencyMS: 42, Task: core.TaskGeneral}})
	if one["p"].P95LatencyMS != 42 {
		t.Errorf("p95 (N=1) = %d, want 42", one["p"].P95LatencyMS)
	}
}

func TestMarkdownComprehensiveSummaryIsSanitized(t *testing.T) {
	rep := Report{
		Mode:           ModeComprehensiveLive,
		FixtureVersion: "test.v1",
		GeneratedAt:    "2026-05-23T12:00:00Z",
		Evidence:       ComprehensiveLiveEvidenceMetadata("free-first", 0),
		NetworkContext: "home_network_2026-05-23",
		Measurements: []Measurement{
			{Provider: "p", Task: core.TaskGeneral, FixtureID: "s1", Kind: KindSearch, Success: true, ResultCount: 5, LatencyMS: 100},
			{Provider: "p", Task: core.TaskGeneral, FixtureID: "s2", Kind: KindSearch, Success: false, ErrorClass: "rate_limited"},
		},
		ProviderSummary: map[string]ProviderStat{
			"p": {Calls: 2, Successes: 1, Failures: 1, P50LatencyMS: 100, P95LatencyMS: 100, AvgResults: 5, ErrorClasses: map[string]int{"rate_limited": 1}, PerTaskCalls: map[string]int{"general": 2}},
		},
	}
	out := MarkdownComprehensiveSummary(rep)
	for _, bad := range []string{"http://", "https://", "Bearer", "Authorization", "api_key"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
			t.Errorf("markdown leaks %q", bad)
		}
	}
	for _, want := range []string{"Comprehensive route evidence", "general", "rate_limited"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestRunComprehensiveLive_MaxFixturesCap(t *testing.T) {
	p := fakeProvider{name: "p", caps: []core.Capability{core.CapabilitySearch}, results: 1}
	set := FixtureSet{
		Version: "test.v1",
		Fixtures: []Fixture{
			{ID: "s1", Task: core.TaskGeneral, Kind: KindSearch, Query: "a"},
			{ID: "s2", Task: core.TaskGeneral, Kind: KindSearch, Query: "b"},
			{ID: "s3", Task: core.TaskGeneral, Kind: KindSearch, Query: "c"},
		},
	}
	rep := RunComprehensiveLive(context.Background(), set, map[string]core.Provider{"p": p}, ComprehensiveOptions{MaxFixtures: 2, InterCallSpacing: 1 * time.Millisecond})
	if len(rep.Measurements) != 2 {
		t.Fatalf("max-fixtures cap not applied: got %d, want 2", len(rep.Measurements))
	}
}

func TestComprehensiveEvidenceMetadataMentionsCorrectCapFlag(t *testing.T) {
	// The CostCaveat originally pointed users at "--max-cases", which the CLI
	// does not define for this mode — following the caveat would fail with an
	// unknown-flag error. Pin the real flag name so a rename in the CLI must
	// also touch this metadata.
	md := ComprehensiveLiveEvidenceMetadata("free-first", 0)
	if !strings.Contains(md.CostCaveat, "--max-comprehensive-cases") {
		t.Errorf("cost caveat should reference --max-comprehensive-cases, got: %q", md.CostCaveat)
	}
	if strings.Contains(md.CostCaveat, "--max-cases ") || strings.HasSuffix(md.CostCaveat, "--max-cases.") {
		t.Errorf("cost caveat must not reference non-existent --max-cases flag, got: %q", md.CostCaveat)
	}
}

func TestComprehensiveReproductionCommandsCarryCap(t *testing.T) {
	// When maxCases > 0 the methodology promises a bounded run, but the
	// original reproduction commands dropped the cap, so a rerun would
	// silently execute every fixture and inflate provider-account cost.
	// The cap must round-trip into the commands; the unbounded path must
	// not emit the flag at all.
	bounded := ComprehensiveLiveEvidenceMetadata("free-first", 5)
	for _, cmd := range bounded.Reproduction {
		if !strings.Contains(cmd, "--max-comprehensive-cases 5") {
			t.Errorf("bounded reproduction command should carry the cap, got: %q", cmd)
		}
	}
	full := ComprehensiveLiveEvidenceMetadata("free-first", 0)
	for _, cmd := range full.Reproduction {
		if strings.Contains(cmd, "--max-comprehensive-cases") {
			t.Errorf("unbounded reproduction command should not include the cap flag, got: %q", cmd)
		}
	}
}
