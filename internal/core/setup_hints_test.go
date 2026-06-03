package core

import (
	"reflect"
	"sort"
	"testing"
)

// configuredSet is a small helper so each test case reads as "these providers
// have keys configured." The string keys match the provider names in
// BYOKProviders (lowercase).
func configuredSet(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func TestBuildSetupSuggestionsExhaustive(t *testing.T) {
	cases := []struct {
		label                string
		configured           map[string]bool
		extractAvailable     bool     // keyless httpfetch backstop (always true in the default service)
		wantMissing          []string // missing provider names, sorted
		wantImpactByProvider map[string]string
	}{
		// extractAvailable=true is the PRODUCTION reality: the keyless httpfetch
		// backstop is always registered, so extract works out of the box and a
		// missing keyed extract provider is a fidelity upgrade (medium), not a
		// disabled-feature unlock (high).
		{
			label:                "no keys, keyless extract baseline — keyed extract = fidelity upgrade",
			configured:           configuredSet(),
			extractAvailable:     true,
			wantMissing:          []string{"brave", "firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium", "firecrawl": "medium"},
		},
		{
			label:                "only brave, keyless extract baseline — keyed extract still MEDIUM upgrade",
			configured:           configuredSet("brave"),
			extractAvailable:     true,
			wantMissing:          []string{"firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium", "firecrawl": "medium"},
		},
		{
			label:                "only tavily — extract keyed via tavily, others MEDIUM/LOW",
			configured:           configuredSet("tavily"),
			extractAvailable:     true,
			wantMissing:          []string{"brave", "firecrawl"},
			wantImpactByProvider: map[string]string{"brave": "medium", "firecrawl": "low"},
		},
		{
			label:                "only firecrawl — extract keyed via firecrawl",
			configured:           configuredSet("firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium"},
		},
		{
			label:                "brave + tavily — only firecrawl missing, pure redundancy",
			configured:           configuredSet("brave", "tavily"),
			extractAvailable:     true,
			wantMissing:          []string{"firecrawl"},
			wantImpactByProvider: map[string]string{"firecrawl": "low"},
		},
		{
			label:                "brave + firecrawl — tavily missing, semantic quality MEDIUM",
			configured:           configuredSet("brave", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium"},
		},
		{
			label:                "tavily + firecrawl — brave missing, search slower",
			configured:           configuredSet("tavily", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{"brave"},
			wantImpactByProvider: map[string]string{"brave": "medium"},
		},
		{
			label:                "all three configured — no suggestions",
			configured:           configuredSet("brave", "tavily", "firecrawl"),
			extractAvailable:     true,
			wantMissing:          []string{},
			wantImpactByProvider: map[string]string{},
		},
		// extractAvailable=false: a build with NO extract provider at all (no
		// httpfetch, no keyed extract, no Scrapling). Extract is genuinely
		// unavailable, so the keyed extract providers are HIGH-impact unlocks. This
		// is not the default service, but the function must stay correct for it.
		{
			label:                "no keys AND no keyless extract — extract truly disabled, HIGH",
			configured:           configuredSet(),
			extractAvailable:     false,
			wantMissing:          []string{"brave", "firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "high", "firecrawl": "high"},
		},
		{
			label:                "only brave, no keyless extract — both extract providers HIGH",
			configured:           configuredSet("brave"),
			extractAvailable:     false,
			wantMissing:          []string{"firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"tavily": "high", "firecrawl": "high"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := BuildSetupSuggestions(tc.configured, tc.extractAvailable)
			gotKeys := []string{}
			gotImpact := map[string]string{}
			for _, s := range got {
				// Map MissingKey (env var) back to provider name for assertion.
				for _, p := range BYOKProviders() {
					for _, ev := range p.EnvVars {
						if ev == s.MissingKey {
							gotKeys = append(gotKeys, p.Name)
							gotImpact[p.Name] = s.Impact
						}
					}
				}
			}
			sort.Strings(gotKeys)
			if !reflect.DeepEqual(gotKeys, tc.wantMissing) {
				t.Errorf("missing providers: got %v, want %v", gotKeys, tc.wantMissing)
			}
			if !reflect.DeepEqual(gotImpact, tc.wantImpactByProvider) {
				t.Errorf("impact map: got %v, want %v", gotImpact, tc.wantImpactByProvider)
			}
		})
	}
}

func TestBuildSetupSuggestionsFieldsArePopulated(t *testing.T) {
	// Sanity check: when a suggestion is produced, all string fields are
	// non-empty so the AI tool has enough to articulate.
	got := BuildSetupSuggestions(configuredSet(), true) // worst case: 3 suggestions (keyless extract baseline)
	if len(got) == 0 {
		t.Fatal("expected suggestions for zero-key case")
	}
	for _, s := range got {
		if s.MissingKey == "" || s.Impact == "" || len(s.Unlocks) == 0 ||
			s.CurrentWorkaround == "" || s.FreeTier == "" || s.SignupURL == "" || s.EnvExample == "" {
			t.Errorf("suggestion has empty field: %+v", s)
		}
	}
}

func TestBuildSetupTipPresenceMatchesSuggestions(t *testing.T) {
	// With no suggestions the tip must be nil. With suggestions present
	// the tip Summary must mention at least one of the missing keys.
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily", "firecrawl"), true)); tip != nil {
		t.Errorf("expected nil tip when nothing missing, got %+v", tip)
	}
	// LOW-only: brave + tavily configured, firecrawl missing at LOW — tip must be nil.
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily"), true)); tip != nil {
		t.Errorf("expected nil tip for LOW-only suggestions, got %+v", tip)
	}
	// Only brave: with the keyless extract baseline, tavily+firecrawl are MEDIUM
	// fidelity upgrades, so the tip is present (a quality nudge, not "disabled").
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave"), true))
	if tip == nil {
		t.Fatal("expected non-nil tip when MEDIUM fidelity upgrades are available")
	}
	if tip.Summary == "" {
		t.Error("tip.Summary is empty")
	}
}
