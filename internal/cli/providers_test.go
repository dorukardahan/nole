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

	var statuses []core.ProviderStatus
	if err := json.Unmarshal(out.Bytes(), &statuses); err != nil {
		t.Fatalf("providers output is not JSON: %v\n%s", err, out.String())
	}
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

func TestProvidersCommandFreeFirstDoesNotAllowPremiumJustBecauseKeyExists(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")

	statuses := decodeProvidersJSON(t)
	got := statuses["tavily"]
	if !got.Available {
		t.Fatalf("test setup expected tavily adapter to be available when key is present: %#v", got)
	}
	if got.CostPolicy != core.CostPolicyFreeFirst || got.CostClass != core.CostClassPremiumCapable || got.AllowedByPolicy || got.PolicyReason != "premium_blocked_free_first" {
		t.Fatalf("free-first must block premium-capable provider even with a key, got %#v", got)
	}
}

func TestProvidersCommandCostCappedRequiresExplicitEstimate(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_COST_POLICY", string(core.CostPolicyCostCapped))
	t.Setenv("NOLE_HARD_CAP_CENTS", "5")

	withoutEstimate := decodeProvidersJSON(t)["tavily"]
	if withoutEstimate.AllowedByPolicy || withoutEstimate.PolicyReason != "unknown_cost_blocked" {
		t.Fatalf("cost-capped should fail closed without an explicit local estimate, got %#v", withoutEstimate)
	}

	t.Setenv("NOLE_TAVILY_ESTIMATED_COST_CENTS", "2")
	withEstimate := decodeProvidersJSON(t)["tavily"]
	if !withEstimate.AllowedByPolicy || withEstimate.PolicyReason != "within_cost_cap" || withEstimate.EstimatedCostCents != 2 {
		t.Fatalf("cost-capped should allow premium only with an explicit estimate inside cap, got %#v", withEstimate)
	}
}

func TestProvidersCommandQualityFirstExplicitlyAllowsPremiumCapable(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_COST_POLICY", string(core.CostPolicyQualityFirst))

	got := decodeProvidersJSON(t)["tavily"]
	if got.CostPolicy != core.CostPolicyQualityFirst || got.CostClass != core.CostClassPremiumCapable || !got.AllowedByPolicy || got.PolicyReason != "quality_first_allows_premium" {
		t.Fatalf("quality-first should explicitly allow premium-capable provider, got %#v", got)
	}
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
	var statuses []core.ProviderStatus
	if err := json.Unmarshal(out.Bytes(), &statuses); err != nil {
		t.Fatalf("providers output is not JSON: %v\n%s", err, out.String())
	}
	byName := map[string]core.ProviderStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	return byName
}

func clearProviderPolicyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY", "TAVILY_API_KEY", "FIRECRAWL_API_KEY",
		"NOLE_COST_POLICY", "NOLE_HARD_CAP_CENTS", "NOLE_BRAVE_ESTIMATED_COST_CENTS", "NOLE_TAVILY_ESTIMATED_COST_CENTS",
		"NOLE_FIRECRAWL_ESTIMATED_COST_CENTS",
		"NOLE_QUOTA_LEDGER_PATH", "NOLE_CACHE_TTL", "NOLE_CACHE_TTL_SECONDS",
	} {
		t.Setenv(key, "")
	}
}
