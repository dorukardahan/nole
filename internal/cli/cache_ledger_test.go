package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

func TestDefaultQuotaLedgerPersistsWhenPathConfigured(t *testing.T) {
	clearProviderPolicyEnv(t)
	ledgerPath := filepath.Join(t.TempDir(), "quota-ledger.json")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", ledgerPath)
	policy := core.QuotaPolicy{Policy: core.CostPolicyCostCapped, HardCapCents: 5}
	entries := []core.QuotaEntry{{Provider: "tavily", CostClass: core.CostClassPremiumCapable, EstimatedCostCents: 2}}

	ledger := defaultQuotaLedger(policy, entries)
	if status := ledger.BudgetStatus(); status.LedgerState != core.LedgerStateFileOK || status.LedgerWarning != "" {
		t.Fatalf("expected file-backed ledger state without warnings, got %#v", status)
	}
	if err := ledger.Record("tavily"); err != nil {
		t.Fatalf("record premium attempt: %v", err)
	}

	reloaded := defaultQuotaLedger(policy, entries)
	if got := reloaded.BudgetStatus().SpentCents; got != 2 {
		t.Fatalf("reloaded spent cents = %d, want 2", got)
	}
}

func TestDoctorCommandReportsLedgerStateWithoutPrivatePath(t *testing.T) {
	clearProviderPolicyEnv(t)
	ledgerPath := filepath.Join(t.TempDir(), "quota-ledger.json")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", ledgerPath)
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "ledger=file-ok") {
		t.Fatalf("doctor output should include ledger state, got:\n%s", text)
	}
	if strings.Contains(text, ledgerPath) {
		t.Fatalf("doctor output leaked private ledger path:\n%s", text)
	}
}

func TestDoctorCommandMCPFailureDoesNotLeakConfiguredBinaryPath(t *testing.T) {
	clearProviderPolicyEnv(t)
	privateBinary := filepath.Join(t.TempDir(), "private", "internal", "nole")
	t.Setenv("NOLE_MCP_SMOKE_BINARY", privateBinary)
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor", "--mcp"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor --mcp should fail when configured smoke binary is missing")
	}
	text := out.String()
	if !strings.Contains(text, "protocol_reason:") {
		t.Fatalf("doctor --mcp should report pathless protocol reason, got:\n%s", text)
	}
	if strings.Contains(text, privateBinary) || strings.Contains(text, filepath.Dir(privateBinary)) {
		t.Fatalf("doctor --mcp leaked private smoke binary path:\n%s", text)
	}
}

func TestDefaultQuotaLedgerCanStayMemoryOnly(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	ledger := defaultQuotaLedger(core.DefaultQuotaPolicy(), []core.QuotaEntry{{Provider: "ddgs", CostClass: core.CostClassKeylessFree}})
	if status := ledger.BudgetStatus(); status.LedgerState != core.LedgerStateMemory {
		t.Fatalf("expected memory ledger, got %#v", status)
	}
}

func TestDefaultResponseCacheFromEnvUsesConfiguredTTL(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_CACHE_TTL_SECONDS", "60")
	cache := defaultResponseCacheFromEnv()
	if cache == nil {
		t.Fatal("expected cache when NOLE_CACHE_TTL_SECONDS is positive")
	}
	cache.SetSearch(core.SearchRequest{Query: " Cache Test ", Task: core.TaskDocs, Limit: 2}, core.SearchResponse{Query: "Cache Test", Task: core.TaskDocs, Provider: "ddgs"})
	if _, ok := cache.GetSearch(core.SearchRequest{Query: "cache test", Task: core.TaskDocs, Limit: 2}); !ok {
		t.Fatal("expected configured cache to normalize and return search response")
	}
}

func TestDefaultResponseCacheFromEnvDisablesNonPositiveTTL(t *testing.T) {
	clearProviderPolicyEnv(t)
	for _, raw := range []string{"", "0", "-1", "not-a-duration"} {
		t.Run(raw, func(t *testing.T) {
			clearProviderPolicyEnv(t)
			t.Setenv("NOLE_CACHE_TTL_SECONDS", raw)
			if cache := defaultResponseCacheFromEnv(); cache != nil {
				t.Fatalf("TTL %q should disable cache, got %#v", raw, cache)
			}
		})
	}
}

func TestDefaultCacheTTLAllowsDurationSyntax(t *testing.T) {
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_CACHE_TTL", "2m")
	if got := defaultCacheTTL(); got != 2*time.Minute {
		t.Fatalf("defaultCacheTTL() = %s, want 2m", got)
	}
}
