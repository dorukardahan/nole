package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// runConfigDump executes `config dump [args...]` against an isolated env and
// returns combined stdout. It points the ledger at memory so the test never
// touches the user's real quota file.
func runConfigDump(t *testing.T, args ...string) string {
	t.Helper()
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1") // don't merge the developer's ~/.config/nole/.env
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"config", "dump"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("config dump failed: %v\noutput:\n%s", err, out.String())
	}
	return out.String()
}

func TestConfigDumpShowsSecretAsSetNotValue(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key-value")
	out := runConfigDump(t)
	if !strings.Contains(out, "TAVILY_API_KEY") {
		t.Fatalf("expected TAVILY_API_KEY in output:\n%s", out)
	}
	if !strings.Contains(out, "set") {
		t.Fatalf("expected a set/unset marker:\n%s", out)
	}
	if strings.Contains(out, "placeholder-test-key-value") {
		t.Fatalf("config dump LEAKED a secret value:\n%s", out)
	}
}

func TestConfigDumpJSONShape(t *testing.T) {
	out := runConfigDump(t, "--json")
	var d configDump
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("config dump --json is not valid JSON: %v\n%s", err, out)
	}
	if d.CostPolicy != "free-first" {
		t.Fatalf("default cost_policy = %q, want free-first", d.CostPolicy)
	}
	if d.HardCapSource != "" {
		t.Fatalf("free-first must not carry a hard_cap_source, got %q", d.HardCapSource)
	}
	// Secrets present and all unset (no key set in this test), value never serialized.
	if len(d.ProviderKeys) == 0 {
		t.Fatal("expected secrets[] in dump")
	}
	for _, s := range d.ProviderKeys {
		if s.Set {
			t.Fatalf("did not expect %s set in a clean env", s.Env)
		}
	}
	// quota_floors reflect the v0.7.1 credit-vs-call floors.
	floors := map[string]int{}
	for _, q := range d.QuotaFloors {
		floors[q.Provider] = q.FreeQuota
	}
	if floors["brave"] != 1000 || floors["tavily"] != 500 || floors["firecrawl"] != 250 {
		t.Fatalf("quota_floors drift: %+v", floors)
	}
	// The raw JSON must not contain a "value" key inside a secret object — the
	// secretStatus struct has no value field at all, so assert the field name is
	// absent next to a secret env name.
	if strings.Contains(out, `"env":"TAVILY_API_KEY","provider":"tavily","set":false,"value"`) {
		t.Fatalf("secret object unexpectedly carries a value field:\n%s", out)
	}
}

func TestConfigDumpCostCappedSurfacesHardCapSource(t *testing.T) {
	t.Setenv("NOLE_COST_POLICY", "cost-capped")
	t.Setenv("NOLE_HARD_CAP_CENTS", "") // explicitly unset
	out := runConfigDump(t, "--json")
	var d configDump
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if d.HardCapSource != "unset" {
		t.Fatalf("cost-capped with no cap should report hard_cap_source=unset, got %q", d.HardCapSource)
	}
	human := runConfigDump(t)
	if !strings.Contains(human, "cost_cap_note") || !strings.Contains(human, "NOLE_HARD_CAP_CENTS") {
		t.Fatalf("human mode should explain the blocked cost-capped setup:\n%s", human)
	}
}

func TestConfigDumpReflectsLogMode(t *testing.T) {
	t.Setenv("NOLE_LOG", "json")
	out := runConfigDump(t, "--json")
	var d configDump
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if d.LogMode != "json" {
		t.Fatalf("log_mode = %q, want json", d.LogMode)
	}
}

// config dump must not read a secret value even into the ConfigEnv allowlist:
// API-key env vars are not on the allowlist, so they never appear with a value.
func TestConfigDumpConfigEnvNeverCarriesAKey(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "placeholder-brave-secret")
	out := runConfigDump(t, "--json")
	if strings.Contains(out, "placeholder-brave-secret") {
		t.Fatalf("config_env leaked a key value:\n%s", out)
	}
}

// config dump is read-only: it must not mutate the quota ledger (no debit).
func TestConfigDumpIsReadOnly(t *testing.T) {
	first := runConfigDump(t, "--json")
	second := runConfigDump(t, "--json")
	var a, b configDump
	_ = json.Unmarshal([]byte(first), &a)
	_ = json.Unmarshal([]byte(second), &b)
	floorsA, floorsB := map[string]int{}, map[string]int{}
	for _, p := range a.Providers {
		floorsA[p.Name] = p.FreeRemaining
	}
	for _, p := range b.Providers {
		floorsB[p.Name] = p.FreeRemaining
	}
	for name, remA := range floorsA {
		if floorsB[name] != remA {
			t.Fatalf("config dump mutated free_remaining for %s: %d -> %d", name, remA, floorsB[name])
		}
	}
}

// A credential-bearing NOLE_QUOTA_LEDGER_PATH (e.g. s3://user:pass@host) must
// be redacted in the ledger_path field, not just in config_env.
func TestConfigDumpRedactsCredentialLedgerPath(t *testing.T) {
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "s3://user:FAKESECRET-leak-me@bucket/nole-ledger.json")
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"config", "dump", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("config dump failed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "FAKESECRET-leak-me") {
		t.Fatalf("config dump leaked a credential in ledger_path:\n%s", out.String())
	}
	var d configDump
	if err := json.Unmarshal([]byte(out.String()), &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(d.LedgerPath, "REDACTED") {
		t.Fatalf("ledger_path should be redacted, got %q", d.LedgerPath)
	}
}

func TestBareConfigCommandExitsZero(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"config"})
	if err := root.Execute(); err != nil {
		t.Fatalf("bare `nole config` should print help and exit 0, got error: %v", err)
	}
}
