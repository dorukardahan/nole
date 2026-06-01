package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"doctor"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestDoctorJSONFlagEmitsMachineReadable(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key-value")
	out, err := runDoctor(t, "--json")
	if err != nil {
		t.Fatalf("doctor --json failed: %v\n%s", err, out)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v\n%s", err, out)
	}
	if len(report.Providers) == 0 {
		t.Fatal("expected providers[] in doctor report")
	}
	if report.Budget.Policy != "free-first" {
		t.Fatalf("budget.policy = %q, want free-first", report.Budget.Policy)
	}
	if len(report.ProviderKeys) == 0 {
		t.Fatal("expected secrets[] in doctor report")
	}
	// The set TAVILY key must show set:true but never its value.
	if strings.Contains(out, "placeholder-test-key-value") {
		t.Fatalf("doctor --json LEAKED a secret value:\n%s", out)
	}
	var sawTavilySet bool
	for _, s := range report.ProviderKeys {
		if s.Env == "TAVILY_API_KEY" && s.Set {
			sawTavilySet = true
		}
	}
	if !sawTavilySet {
		t.Fatalf("expected TAVILY_API_KEY set:true in secrets[]: %+v", report.ProviderKeys)
	}
}

func TestDoctorJSONDoesNotBreakHumanMode(t *testing.T) {
	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("doctor (human) failed: %v\n%s", err, out)
	}
	// Re-assert the existing human-path markers so the JSON branch didn't
	// regress the default output the other tests depend on.
	for _, want := range []string{"nole doctor", "- binary: ok", "policy=", "- secrets: not printed", "free_remaining="} {
		if !strings.Contains(out, want) {
			t.Fatalf("human doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorJSONMCPSmokeFailureStillErrors(t *testing.T) {
	// Point the protocol smoke at a binary that does not exist so the smoke
	// fails; --json must still emit the report AND return a non-zero error.
	t.Setenv("NOLE_MCP_SMOKE_BINARY", "/nonexistent/nole-binary-xyz")
	out, err := runDoctor(t, "--json", "--mcp")
	if err == nil {
		t.Fatalf("expected doctor --json --mcp to error on smoke failure, got nil\n%s", out)
	}
	var report doctorReport
	if jerr := json.Unmarshal([]byte(out), &report); jerr != nil {
		t.Fatalf("report should still be valid JSON on smoke failure: %v\n%s", jerr, out)
	}
	if report.MCP == nil || report.MCP.OK {
		t.Fatalf("expected mcp.ok=false on smoke failure, got %+v", report.MCP)
	}
}
