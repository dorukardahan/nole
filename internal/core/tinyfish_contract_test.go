package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyedFreeAllowedByEveryPolicyWithoutQuota(t *testing.T) {
	for _, policy := range []CostPolicy{CostPolicyFreeFirst, CostPolicyCostCapped, CostPolicyQualityFirst} {
		t.Run(string(policy), func(t *testing.T) {
			ledger := NewMemoryQuotaLedgerWithPolicy(QuotaPolicy{Policy: policy})
			ledger.Set(QuotaEntry{Provider: "tinyfish", CostClass: CostClassKeyedFree, MeteringModel: "request-rate"})
			decision := ledger.Decide("tinyfish")
			if !decision.Allowed || decision.Reason != "keyed_free" || decision.FreeRemaining != 0 || decision.EstimatedCostCents != 0 || decision.SpentCents != 0 {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestKeyedFreeRecordDoesNotDecrementOrSpend(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	seed := QuotaEntry{Provider: "tinyfish", CostClass: CostClassKeyedFree, MeteringModel: "request-rate"}
	ledger.Set(seed)
	for i := 0; i < 3; i++ {
		if err := ledger.Record("tinyfish"); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	got, ok := ledger.Get("tinyfish")
	if !ok || got.CostClass != CostClassKeyedFree || got.FreeRemaining != 0 || got.FreeQuota != 0 || got.SpentCents != 0 || got.EstimatedCostCents != 0 || got.RefreshWindow != RefreshNone {
		t.Fatalf("entry changed after keyed-free records: %#v", got)
	}
}

func TestKeyedFreeAllowedWhenLedgerFailsClosed(t *testing.T) {
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tinyfish", CostClass: CostClassKeyedFree})
	ledger.failClosedReason = "ledger_corrupt_fail_closed"
	decision := ledger.Decide("tinyfish")
	if !decision.Allowed || decision.Reason != "keyed_free" {
		t.Fatalf("keyed-free must remain usable in fail-closed ledger mode: %#v", decision)
	}
}

func TestFileLedgerRoundTripsKeyedFreeWithoutSchemaLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota-ledger.json")
	seed := []QuotaEntry{{Provider: "tinyfish", CostClass: CostClassKeyedFree, MeteringModel: "request-rate"}}
	ledger, err := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Record("tinyfish"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewFileQuotaLedgerWithPolicy(path, DefaultQuotaPolicy(), seed)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reloaded.Get("tinyfish")
	if !ok || entry.CostClass != CostClassKeyedFree || entry.FreeQuota != 0 || entry.RefreshWindow != RefreshNone || entry.SpentCents != 0 {
		t.Fatalf("round-tripped entry = %#v", entry)
	}
}

func TestTinyFishBYOKMetadataIsKeyRequiredCreditFreeRateOnly(t *testing.T) {
	meta, ok := LookupBYOK("tinyfish")
	if !ok {
		t.Fatal("tinyfish BYOK metadata missing")
	}
	if meta.DefaultCostClass != CostClassKeyedFree || meta.FreeQuota != 0 || meta.RefreshWindow != RefreshNone || meta.MeteringModel != "request-rate" || !meta.SupportsSearch || !meta.SupportsExtract {
		t.Fatalf("metadata = %#v", meta)
	}
	if len(meta.EnvVars) != 1 || meta.EnvVars[0] != "TINYFISH_API_KEY" {
		t.Fatalf("env vars = %v", meta.EnvVars)
	}
	lower := strings.ToLower(meta.FreeTierNote + " " + meta.RateLimitNote)
	for _, want := range []string{"search", "fetch", "no credits", "rate"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("metadata should mention %q without inventing quota: %#v", want, meta)
		}
	}
	for _, forbidden := range []string{"monthly quota", "remaining credits", "500 credits/month"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("metadata invents %q: %#v", forbidden, meta)
		}
	}
}

func TestTinyFishRemoteUsageStrategyDoesNotQueryOrInventBalance(t *testing.T) {
	provider := &remoteUsageFakeProvider{name: "tinyfish", caps: []Capability{CapabilitySearch, CapabilityExtract, CapabilityStatus}}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "tinyfish", CostClass: CostClassKeyedFree, MeteringModel: "request-rate"})
	svc := NewService(registry, ledger, RouteMatrix{TaskGeneral: {"tinyfish"}, TaskExtract: {"tinyfish"}})
	resp := svc.ProviderStatusWithOptions(context.Background(), ProviderStatusOptions{LiveUsage: true, SyncLedger: true})
	if len(resp.Providers) != 1 {
		t.Fatalf("providers = %#v", resp.Providers)
	}
	status := resp.Providers[0]
	if provider.usageCalls != 0 || status.RemoteUsage != nil || status.RemoteUsageError != "" || status.RemoteUsageStrategy != RemoteUsageStrategyUnsupported {
		t.Fatalf("status = %#v usageCalls=%d", status, provider.usageCalls)
	}
	lower := strings.ToLower(status.RemoteUsageReason)
	if !strings.Contains(lower, "request-rate") || !strings.Contains(lower, "credit balance") {
		t.Fatalf("remote usage reason = %q", status.RemoteUsageReason)
	}
}

func TestTinyFishMissingKeySuggestionIsLowAndNeverBecomesSetupTip(t *testing.T) {
	suggestions := BuildSetupSuggestions(configuredSet("brave", "tavily", "firecrawl"), true)
	if len(suggestions) != 1 || suggestions[0].MissingKey != "TINYFISH_API_KEY" || suggestions[0].Impact != "low" {
		t.Fatalf("suggestions = %#v", suggestions)
	}
	if tip := BuildSetupTip(suggestions); tip != nil {
		t.Fatalf("low experimental TinyFish suggestion must not nag: %#v", tip)
	}
}
