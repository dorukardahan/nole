package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestProvidersCommandJSONIncludesCostPolicyWithoutSecrets(t *testing.T) {
	clearProviderPolicyEnv(t)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"providers", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("providers failed: %v", err)
	}

	var envelope core.ProviderStatusResponse
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("providers output is not JSON: %v\n%s", err, out.String())
	}
	statuses := envelope.Providers
	if len(statuses) == 0 {
		t.Fatal("expected provider statuses")
	}
	byName := map[string]core.ProviderStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	if got := byName["ddgs"]; got.CostClass != core.CostClassKeylessFree || got.CostPolicy != core.CostPolicyFreeFirst || !got.AllowedByPolicy {
		t.Fatalf("unexpected ddgs policy status: %#v", got)
	}
	if got := byName["brave"]; got.CostClass != core.CostClassDisabledNoKey || got.AllowedByPolicy || got.PolicyReason != "disabled_no_key" {
		t.Fatalf("unexpected brave disabled status: %#v", got)
	}
	forbidden := []string{"SECRET", "Bearer", "Authorization", "api_key"}
	for _, token := range forbidden {
		if bytes.Contains(out.Bytes(), []byte(token)) {
			t.Fatalf("providers JSON leaked forbidden token %q: %s", token, out.String())
		}
	}
}

func TestProvidersCommandFreeFirstAllowsBYOKFreeTierByDefault(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")

	statuses := decodeProvidersJSON(t)
	got := statuses["tavily"]
	if !got.Available {
		t.Fatalf("test setup expected tavily adapter to be available when key is present: %#v", got)
	}
	if got.CostPolicy != core.CostPolicyFreeFirst || got.CostClass != core.CostClassFreeTierBYOK || !got.AllowedByPolicy || got.PolicyReason != "free_tier_available" {
		t.Fatalf("free-first should allow BYOK key as free-tier by default, got %#v", got)
	}
	if got.FreeRemaining != 1000 {
		t.Fatalf("free-tier-BYOK should seed FreeRemaining=1000 from hardcoded defaults, got %d", got.FreeRemaining)
	}
}

func TestProvidersCommandPaidModeUsesPremiumCapable(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_TAVILY_PAID", "1")

	got := decodeProvidersJSON(t)["tavily"]
	if got.CostClass != core.CostClassPremiumCapable || got.AllowedByPolicy || got.PolicyReason != "premium_blocked_free_first" {
		t.Fatalf("paid mode under free-first should mark provider premium-capable and block, got %#v", got)
	}
}

func TestProvidersCommandPaidModeCostCappedRequiresExplicitEstimate(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_TAVILY_PAID", "1")
	t.Setenv("NOLE_COST_POLICY", string(core.CostPolicyCostCapped))
	t.Setenv("NOLE_HARD_CAP_CENTS", "5")

	withoutEstimate := decodeProvidersJSON(t)["tavily"]
	if withoutEstimate.AllowedByPolicy || withoutEstimate.PolicyReason != "unknown_cost_blocked" {
		t.Fatalf("cost-capped paid should fail closed without an explicit local estimate, got %#v", withoutEstimate)
	}

	t.Setenv("NOLE_TAVILY_ESTIMATED_COST_CENTS", "2")
	withEstimate := decodeProvidersJSON(t)["tavily"]
	if !withEstimate.AllowedByPolicy || withEstimate.PolicyReason != "within_cost_cap" || withEstimate.EstimatedCostCents != 2 {
		t.Fatalf("cost-capped paid should allow premium only with an explicit estimate inside cap, got %#v", withEstimate)
	}
}

func TestProvidersCommandPaidModeQualityFirstExplicitlyAllows(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_TAVILY_PAID", "1")
	t.Setenv("NOLE_COST_POLICY", string(core.CostPolicyQualityFirst))

	got := decodeProvidersJSON(t)["tavily"]
	if got.CostPolicy != core.CostPolicyQualityFirst || got.CostClass != core.CostClassPremiumCapable || !got.AllowedByPolicy || got.PolicyReason != "quality_first_allows_premium" {
		t.Fatalf("quality-first paid mode should allow premium-capable, got %#v", got)
	}
}

// TestProvidersCommandJSONEnvelopeIncludesSetupSuggestions asserts that the
// --json output is an envelope (not a bare array) and that setup_suggestions
// is populated when all BYOK keys are absent. Three BYOK providers are
// registered (brave, tavily, firecrawl), so we expect exactly three entries.
func TestProvidersCommandJSONEnvelopeIncludesSetupSuggestions(t *testing.T) {
	clearProviderPolicyEnv(t)
	// All BYOK keys are already cleared by clearProviderPolicyEnv; be explicit.
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")

	envelope := decodeProvidersEnvelopeJSON(t)

	if len(envelope.Providers) == 0 {
		t.Fatal("expected providers in envelope")
	}
	if len(envelope.SetupSuggestions) != 3 {
		t.Fatalf("expected 3 setup_suggestions (brave/tavily/firecrawl all missing), got %d: %#v",
			len(envelope.SetupSuggestions), envelope.SetupSuggestions)
	}
	// All three BYOK providers must appear as missing keys.
	byKey := map[string]core.SetupSuggestion{}
	for _, s := range envelope.SetupSuggestions {
		byKey[s.MissingKey] = s
	}
	for _, wantKey := range []string{"BRAVE_API_KEY", "TAVILY_API_KEY", "FIRECRAWL_API_KEY"} {
		if _, ok := byKey[wantKey]; !ok {
			t.Errorf("expected setup_suggestions to contain missing key %q, got keys: %v",
				wantKey, keysOf(byKey))
		}
	}
	// Impact must be set for each suggestion.
	for _, s := range envelope.SetupSuggestions {
		if s.Impact == "" {
			t.Errorf("suggestion for %q has empty impact", s.MissingKey)
		}
		if s.SignupURL == "" {
			t.Errorf("suggestion for %q has empty signup_url", s.MissingKey)
		}
	}
}

func keysOf(m map[string]core.SetupSuggestion) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func decodeProvidersJSON(t *testing.T) map[string]core.ProviderStatus {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"providers", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("providers failed: %v", err)
	}
	var envelope core.ProviderStatusResponse
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("providers output is not JSON: %v\n%s", err, out.String())
	}
	byName := map[string]core.ProviderStatus{}
	for _, status := range envelope.Providers {
		byName[status.Name] = status
	}
	return byName
}

func decodeProvidersEnvelopeJSON(t *testing.T) core.ProviderStatusResponse {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"providers", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("providers failed: %v", err)
	}
	var envelope core.ProviderStatusResponse
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("providers output is not JSON: %v\n%s", err, out.String())
	}
	return envelope
}

func clearProviderPolicyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY", "TAVILY_API_KEY", "FIRECRAWL_API_KEY",
		"NOLE_COST_POLICY", "NOLE_HARD_CAP_CENTS", "NOLE_BRAVE_ESTIMATED_COST_CENTS", "NOLE_TAVILY_ESTIMATED_COST_CENTS",
		"NOLE_FIRECRAWL_ESTIMATED_COST_CENTS",
		"NOLE_BRAVE_PAID", "NOLE_TAVILY_PAID", "NOLE_FIRECRAWL_PAID",
		"NOLE_CACHE_TTL", "NOLE_CACHE_TTL_SECONDS",
	} {
		t.Setenv(key, "")
	}
	// Force memory-mode ledger for provider/doctor command tests so no test
	// run writes to ~/.local/state/nole/. The file-backed default path is
	// exercised by TestDefaultLedgerIsFileBacked in cache_ledger_test.go.
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
}
