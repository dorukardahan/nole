package bench

import (
	"context"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

func TestComprehensiveProviderSpacingUsesProviderFloor(t *testing.T) {
	opts := ComprehensiveOptions{
		InterCallSpacing: 250 * time.Millisecond,
		ProviderInterCallSpacing: map[string]time.Duration{
			"tinyfish": 2 * time.Second,
			"fast":     10 * time.Millisecond,
		},
	}.normalize()
	if got := opts.spacingFor("tinyfish"); got != 2*time.Second {
		t.Fatalf("tinyfish spacing = %v", got)
	}
	if got := opts.spacingFor("fast"); got != 250*time.Millisecond {
		t.Fatalf("provider floor must not lower global spacing: %v", got)
	}
	if got := opts.spacingFor("other"); got != 250*time.Millisecond {
		t.Fatalf("unrelated provider spacing = %v", got)
	}
}

func TestRunComprehensiveLiveProviderSpacingWaitIsCancellable(t *testing.T) {
	p := fakeProvider{name: "tinyfish", caps: []core.Capability{core.CapabilitySearch}, results: 1}
	set := FixtureSet{Version: "test", Fixtures: []Fixture{
		{ID: "one", Task: core.TaskGeneral, Kind: KindSearch, Query: "one"},
		{ID: "two", Task: core.TaskGeneral, Kind: KindSearch, Query: "two"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	report := RunComprehensiveLive(ctx, set, map[string]core.Provider{"tinyfish": p}, ComprehensiveOptions{
		InterCallSpacing:         1 * time.Millisecond,
		ProviderInterCallSpacing: map[string]time.Duration{"tinyfish": time.Second},
	})
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("cancellation did not interrupt provider spacing: %v", elapsed)
	}
	if len(report.Measurements) == 0 {
		t.Fatal("expected first measurement before cancellation")
	}
}

func TestComprehensiveFixtureSetAddsLiveOnlyContractCoverage(t *testing.T) {
	offline := DefaultFixtureSet()
	live := ComprehensiveFixtureSet()
	if live.Version == offline.Version || len(live.Fixtures) <= len(offline.Fixtures) {
		t.Fatalf("comprehensive fixture set should extend, not mutate, offline set: offline=%s/%d live=%s/%d", offline.Version, len(offline.Fixtures), live.Version, len(live.Fixtures))
	}
	wantCategories := map[string]bool{
		"localized search options":      false,
		"freshness search options":      false,
		"JavaScript-heavy extraction":   false,
		"redirect extraction":           false,
		"error-path extraction":         false,
		"structured-content extraction": false,
	}
	for _, fixture := range live.Fixtures {
		if _, ok := wantCategories[fixture.Category]; ok {
			wantCategories[fixture.Category] = true
		}
	}
	for category, found := range wantCategories {
		if !found {
			t.Errorf("missing comprehensive category %q", category)
		}
	}
	if got := DefaultFixtureSet(); got.Version != offline.Version || len(got.Fixtures) != len(offline.Fixtures) {
		t.Fatal("ComprehensiveFixtureSet mutated deterministic offline fixtures")
	}
}

func TestRunComprehensiveOnePassesFixtureSearchOptions(t *testing.T) {
	p := &captureOptionsProvider{name: "capture"}
	fixture := Fixture{ID: "localized", Task: core.TaskNews, Kind: KindSearch, Query: "haber", Country: "tr", SearchLang: "tr", Freshness: "pd"}
	measurement := runComprehensiveOne(context.Background(), p.name, p, fixture, time.Second)
	if !measurement.Success {
		t.Fatalf("measurement = %#v", measurement)
	}
	want := (core.SearchOptions{Country: "tr", SearchLang: "tr", Freshness: "pd"})
	if p.options != want {
		t.Fatalf("options = %#v, want %#v", p.options, want)
	}
}

type captureOptionsProvider struct {
	name    string
	options core.SearchOptions
}

func (p *captureOptionsProvider) Name() string { return p.name }
func (p *captureOptionsProvider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityStatus}
}
func (p *captureOptionsProvider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	p.options = req.Options
	return core.SearchResponse{Provider: p.name, Results: []core.SearchResult{{Provider: p.name}}}, nil
}
func (p *captureOptionsProvider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{}, nil
}
func (p *captureOptionsProvider) Status(ctx context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: p.name, Available: true, Capabilities: p.Capabilities()}
}
