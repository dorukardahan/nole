package core

import (
	"strings"
	"testing"
)

func TestBYOKProvidersInvariants(t *testing.T) {
	if len(BYOKProviders) == 0 {
		t.Fatal("BYOKProviders is empty")
	}
	names := map[string]bool{}
	envVars := map[string]bool{}
	for _, p := range BYOKProviders {
		if p.Name == "" {
			t.Errorf("entry has empty Name: %#v", p)
		}
		if names[p.Name] {
			t.Errorf("duplicate provider name: %q", p.Name)
		}
		names[p.Name] = true
		if len(p.EnvVars) == 0 {
			t.Errorf("%s has no env vars", p.Name)
		}
		for _, ev := range p.EnvVars {
			if envVars[ev] {
				t.Errorf("env var %q is claimed by multiple providers", ev)
			}
			envVars[ev] = true
			if !strings.HasSuffix(ev, "_API_KEY") && !strings.HasSuffix(ev, "_SEARCH_API_KEY") {
				t.Errorf("%s env var %q does not follow the *_API_KEY convention", p.Name, ev)
			}
		}
		if p.FreeQuota <= 0 {
			t.Errorf("%s has non-positive FreeQuota %d", p.Name, p.FreeQuota)
		}
		if p.RefreshWindow == "" {
			t.Errorf("%s has empty RefreshWindow", p.Name)
		}
		if p.SignupURL == "" {
			t.Errorf("%s has empty SignupURL", p.Name)
		}
		if p.EnvExample == "" {
			t.Errorf("%s has empty EnvExample", p.Name)
		}
		if !p.SupportsSearch && !p.SupportsExtract {
			t.Errorf("%s supports neither search nor extract — what is it for?", p.Name)
		}
	}
	for _, want := range []string{"brave", "tavily", "firecrawl"} {
		if !names[want] {
			t.Errorf("missing required provider %q", want)
		}
	}
	if names["jina"] {
		t.Error("jina entry leaked back in — it was removed in PR #21")
	}
}
