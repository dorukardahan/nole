package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestConfiguredRouteMatrixWithoutTinyFishKeyEqualsDefaultExactly(t *testing.T) {
	got := configuredRouteMatrix("")
	want := core.DefaultRouteMatrix()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-key route matrix changed:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestConfiguredRouteMatrixWithTinyFishKeyPreservesEveryPrefixAndAppendsTail(t *testing.T) {
	base := core.DefaultRouteMatrix()
	got := configuredRouteMatrix("configured")
	if reflect.DeepEqual(got, base) {
		t.Fatal("configured matrix did not append tinyfish")
	}
	for task, wantPrefix := range base {
		route := got[task]
		if len(route) != len(wantPrefix)+1 || route[len(route)-1] != "tinyfish" || !reflect.DeepEqual(route[:len(wantPrefix)], wantPrefix) {
			t.Fatalf("task %s route=%v want prefix=%v + tinyfish", task, route, wantPrefix)
		}
	}
	got[core.TaskGeneral][0] = "mutated"
	if reflect.DeepEqual(got, core.DefaultRouteMatrix()) {
		t.Fatal("test setup failed to mutate configured copy")
	}
	if core.DefaultRouteMatrix()[core.TaskGeneral][0] == "mutated" {
		t.Fatal("configured route matrix aliases DefaultRouteMatrix")
	}
}

func TestRoutePlanCommandUsesConfiguredTinyFishTail(t *testing.T) {
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("TINYFISH_API_KEY", "unit-test-key")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"route-plan", "general web lookup", "--task", "general", "--single-intent", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("route-plan failed: %v", err)
	}
	var plan core.RoutePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("decode route-plan: %v", err)
	}
	if len(plan.Routes) != 1 || len(plan.Routes[0].Route) == 0 || plan.Routes[0].Route[len(plan.Routes[0].Route)-1] != "tinyfish" {
		t.Fatalf("configured route-plan did not append TinyFish: %#v", plan.Routes)
	}
}

func TestTinyFishQuotaEntryIsKeyedFreeWithoutPaidMode(t *testing.T) {
	t.Setenv("NOLE_TINYFISH_PAID", "1")
	present := providerQuotaEntry("tinyfish", true)
	if present.CostClass != core.CostClassKeyedFree || present.FreeRemaining != 0 || present.FreeQuota != 0 || present.RefreshWindow != core.RefreshNone || present.EstimatedCostCents != 0 || present.SpentCents != 0 || present.MeteringModel != "request-rate" {
		t.Fatalf("present entry = %#v", present)
	}
	missing := providerQuotaEntry("tinyfish", false)
	if missing.CostClass != core.CostClassDisabledNoKey || missing.KeylessFree {
		t.Fatalf("missing entry = %#v", missing)
	}
}

func TestDefaultServiceRegistersTinyFishByKeyPresenceOnly(t *testing.T) {
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("TINYFISH_API_KEY", "")
	without := providerStatusByName(defaultService().ProviderStatus(context.Background()))["tinyfish"]
	if without.Available || without.CostClass != core.CostClassDisabledNoKey {
		t.Fatalf("missing-key status = %#v", without)
	}

	t.Setenv("TINYFISH_API_KEY", "placeholder-value-never-print")
	with := providerStatusByName(defaultService().ProviderStatus(context.Background()))["tinyfish"]
	if !with.Available || with.CostClass != core.CostClassKeyedFree || !with.AllowedByPolicy || with.FreeRemaining != 0 {
		t.Fatalf("configured status = %#v", with)
	}
	if !core.HasCapability(with.Capabilities, core.CapabilitySearch) || !core.HasCapability(with.Capabilities, core.CapabilityExtract) {
		t.Fatalf("configured capabilities = %v", with.Capabilities)
	}
}

func providerStatusByName(resp core.ProviderStatusResponse) map[string]core.ProviderStatus {
	out := make(map[string]core.ProviderStatus, len(resp.Providers))
	for _, status := range resp.Providers {
		out[status.Name] = status
	}
	return out
}

func TestTinyFishConfigDumpIsPresenceOnlyAndHasNoQuotaFloor(t *testing.T) {
	const marker = "placeholder-tinyfish-value-never-print"
	t.Setenv("TINYFISH_API_KEY", marker)
	out := runConfigDump(t, "--json")
	if !strings.Contains(out, "TINYFISH_API_KEY") || strings.Contains(out, marker) {
		t.Fatalf("config dump key handling is not presence-only: %s", out)
	}
	dump := buildConfigDump(context.Background())
	for _, floor := range dump.QuotaFloors {
		if floor.Provider == "tinyfish" {
			t.Fatalf("keyed-free provider must not expose a quota floor: %#v", floor)
		}
	}
}

func TestTinyFishHumanStatusMarksMonthlyQuotaNotApplicable(t *testing.T) {
	setTinyFishOnlyStatusEnv(t)

	doctor := runTinyFishStatusCommand(t, "doctor")
	doctorBudgetLine := lineContainingAll(t, doctor, "tinyfish", "metering=request-rate")
	assertQuotaNotApplicableLine(t, "doctor budget", doctorBudgetLine)
	if strings.Contains(doctor, "resets monthly") {
		t.Fatalf("doctor must not apply monthly-reset wording when no quota-tracked provider is configured:\n%s", doctor)
	}

	config := runTinyFishStatusCommand(t, "config", "dump")
	configLine := lineContainingAll(t, config, "tinyfish", "keyed-free")
	assertQuotaNotApplicableLine(t, "config dump", configLine)

	providers := runTinyFishStatusCommand(t, "providers")
	providerLine := lineContainingAll(t, providers, "tinyfish", "keyed-free")
	assertQuotaNotApplicableLine(t, "providers", providerLine)
}

func TestTinyFishMachineStatusKeepsCompatibleZeroWithoutMonthlyQuota(t *testing.T) {
	setTinyFishOnlyStatusEnv(t)

	doctorJSON := runTinyFishStatusCommand(t, "doctor", "--json")
	var report doctorReport
	if err := json.Unmarshal([]byte(doctorJSON), &report); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, doctorJSON)
	}
	var budgetEntry core.QuotaEntry
	for _, entry := range report.Budget.Entries {
		if entry.Provider == "tinyfish" {
			budgetEntry = entry
			break
		}
	}
	if budgetEntry.Provider == "" || budgetEntry.CostClass != core.CostClassKeyedFree || budgetEntry.FreeRemaining != 0 || budgetEntry.FreeQuota != 0 || budgetEntry.RefreshWindow != core.RefreshNone || budgetEntry.MeteringModel != "request-rate" {
		t.Fatalf("TinyFish budget_status compatibility drift: %#v", budgetEntry)
	}

	providers := decodeProvidersJSON(t)
	status, ok := providers["tinyfish"]
	if !ok || status.CostClass != core.CostClassKeyedFree || status.FreeRemaining != 0 || status.PolicyReason != "keyed_free" {
		t.Fatalf("TinyFish provider_status compatibility drift: %#v", status)
	}
}

func setTinyFishOnlyStatusEnv(t *testing.T) {
	t.Helper()
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("TINYFISH_API_KEY", "placeholder-value-never-print")
}

func runTinyFishStatusCommand(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("nole %s failed: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	if strings.Contains(out.String(), "placeholder-value-never-print") {
		t.Fatalf("nole %s leaked the placeholder key:\n%s", strings.Join(args, " "), out.String())
	}
	return out.String()
}

func lineContainingAll(t *testing.T, text string, needles ...string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		matched := true
		for _, needle := range needles {
			if !strings.Contains(line, needle) {
				matched = false
				break
			}
		}
		if matched {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", needles, text)
	return ""
}

func assertQuotaNotApplicableLine(t *testing.T, surface, line string) {
	t.Helper()
	if !strings.Contains(line, "quota=not-applicable") {
		t.Fatalf("%s must make TinyFish quota non-applicability explicit: %q", surface, line)
	}
	if strings.Contains(line, "free_remaining") {
		t.Fatalf("%s must not present TinyFish as a remaining-balance counter: %q", surface, line)
	}
}

func TestRoutePlannerAcceptsTinyFishOnlyAsExplicitSearchOverride(t *testing.T) {
	if !validPlannerProvider("tinyfish") {
		t.Fatal("tinyfish explicit search override rejected")
	}
	opts, err := planOptionsFromFlags("general", "tinyfish", false)
	if err != nil || !reflect.DeepEqual(opts.ProviderOverride, []string{"tinyfish"}) {
		t.Fatalf("opts=%#v err=%v", opts, err)
	}
}

func TestComprehensiveBenchIncludesTinyFishDualCapability(t *testing.T) {
	providers := comprehensiveBenchProviders()
	p, ok := providers["tinyfish"]
	if !ok {
		t.Fatalf("providers = %v", sortedKeys(providers))
	}
	for _, capability := range []core.Capability{core.CapabilitySearch, core.CapabilityExtract} {
		if !core.HasCapability(p.Capabilities(), capability) {
			t.Fatalf("tinyfish comprehensive capabilities = %v", p.Capabilities())
		}
	}
}
