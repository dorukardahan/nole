package cli

import (
	"bytes"
	"os"
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

func TestDefaultQuotaLedgerIsFileBackedByDefault(t *testing.T) {
	// Regression for codex P1 (PR #21 round 4): the brave_note line claims
	// nole caps usage at the monthly free quota. That claim is only true if
	// the ledger survives process restarts. Verify the default ledger is
	// file-backed when no env override is set.
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "")
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	ledger := defaultQuotaLedger(core.DefaultQuotaPolicy(), []core.QuotaEntry{
		{Provider: "tavily", CostClass: core.CostClassFreeTierBYOK, FreeRemaining: 1000, FreeQuota: 1000, RefreshWindow: core.RefreshMonthly, PeriodStart: core.CurrentMonthISO()},
	})
	status := ledger.BudgetStatus()
	if status.LedgerState != core.LedgerStateFileOK {
		t.Fatalf("default ledger should be file-backed, got LedgerState=%s", status.LedgerState)
	}

	// Confirm the file actually landed under the XDG-resolved path.
	expected := filepath.Join(tmp, "nole", "quota-ledger.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected ledger at %s, got stat err: %v", expected, err)
	}
}

func TestDefaultQuotaLedgerHonoursXDGStateHome(t *testing.T) {
	// XDG_STATE_HOME wins over ~/.local/state. Verify the resolved path uses
	// the user's XDG value when it's set.
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "")
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	ledger := defaultQuotaLedger(core.DefaultQuotaPolicy(), nil)
	if status := ledger.BudgetStatus(); status.LedgerState != core.LedgerStateFileOK {
		t.Fatalf("expected file-backed ledger under XDG_STATE_HOME, got %#v", status)
	}
	expected := filepath.Join(xdg, "nole", "quota-ledger.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("ledger should land at XDG path %s, got stat err: %v", expected, err)
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
