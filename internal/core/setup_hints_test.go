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
		wantMissing          []string // missing provider names, sorted
		wantImpactByProvider map[string]string
	}{
		{
			label:                "no keys configured",
			configured:           configuredSet(),
			wantMissing:          []string{"brave", "firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "high", "firecrawl": "high"},
		},
		{
			label:                "only brave — extract still broken, both extract providers HIGH",
			configured:           configuredSet("brave"),
			wantMissing:          []string{"firecrawl", "tavily"},
			wantImpactByProvider: map[string]string{"tavily": "high", "firecrawl": "high"},
		},
		{
			label:                "only tavily — extract works via tavily, others MEDIUM",
			configured:           configuredSet("tavily"),
			wantMissing:          []string{"brave", "firecrawl"},
			wantImpactByProvider: map[string]string{"brave": "medium", "firecrawl": "low"},
		},
		{
			label:                "only firecrawl — extract works via firecrawl",
			configured:           configuredSet("firecrawl"),
			wantMissing:          []string{"brave", "tavily"},
			wantImpactByProvider: map[string]string{"brave": "medium", "tavily": "medium"},
		},
		{
			label:                "brave + tavily — only firecrawl missing, pure redundancy",
			configured:           configuredSet("brave", "tavily"),
			wantMissing:          []string{"firecrawl"},
			wantImpactByProvider: map[string]string{"firecrawl": "low"},
		},
		{
			label:                "brave + firecrawl — tavily missing, semantic quality MEDIUM",
			configured:           configuredSet("brave", "firecrawl"),
			wantMissing:          []string{"tavily"},
			wantImpactByProvider: map[string]string{"tavily": "medium"},
		},
		{
			label:                "tavily + firecrawl — brave missing, search slower",
			configured:           configuredSet("tavily", "firecrawl"),
			wantMissing:          []string{"brave"},
			wantImpactByProvider: map[string]string{"brave": "medium"},
		},
		{
			label:                "all three configured — no suggestions",
			configured:           configuredSet("brave", "tavily", "firecrawl"),
			wantMissing:          []string{},
			wantImpactByProvider: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := BuildSetupSuggestions(tc.configured)
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
	got := BuildSetupSuggestions(configuredSet()) // worst case: 3 suggestions
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
	if tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave", "tavily", "firecrawl"))); tip != nil {
		t.Errorf("expected nil tip when nothing missing, got %+v", tip)
	}
	tip := BuildSetupTip(BuildSetupSuggestions(configuredSet("brave")))
	if tip == nil {
		t.Fatal("expected non-nil tip when extract is missing")
	}
	if tip.Summary == "" {
		t.Error("tip.Summary is empty")
	}
}
