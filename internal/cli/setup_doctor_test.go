package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteMCPJSONConfigMergesExistingServersAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	existing := `{"mcpServers":{"other":{"command":"other","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg mcpConfig
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(b))
	}
	if _, ok := cfg.McpServers["other"]; !ok {
		t.Fatalf("existing server was not preserved: %#v", cfg.McpServers)
	}
	if got := cfg.McpServers["nole"].Command; got != "/usr/local/bin/nole" {
		t.Fatalf("nole command = %q", got)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if string(backup) != existing {
		t.Fatalf("backup content changed: %s", string(backup))
	}
}

func TestWriteMCPJSONConfigPreservesUnknownFieldsAndSecretPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	existing := `{"mcpServers":{"other":{"url":"https://example.com/mcp","type":"sse","headers":{"Authorization":"Bearer SECRET"},"timeout":30,"disabled":true},"nole":{"command":"old","args":["old"],"env":{"OLD_SECRET":"keep-out"}}},"clientFeature":{"keep":true}}`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write config: %v", err)
	}
	assertFileMode(t, path, 0600)
	assertFileMode(t, path+".bak", 0600)
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != existing {
		t.Fatalf("backup must preserve exact existing bytes, got %s", string(backup))
	}

	root := readJSONRoot(t, path)
	if !strings.Contains(string(root["clientFeature"]), `"keep": true`) && !strings.Contains(string(root["clientFeature"]), `"keep":true`) {
		t.Fatalf("unknown root field was not preserved: %s", string(root["clientFeature"]))
	}
	servers := readRawObject(t, root["mcpServers"])
	other := readRawObject(t, servers["other"])
	for _, want := range []string{"url", "type", "headers", "timeout", "disabled"} {
		if _, ok := other[want]; !ok {
			t.Fatalf("unknown field %q was not preserved in other server: %s", want, string(servers["other"]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole command = %q", got)
	}
	if _, ok := nole["env"]; ok {
		t.Fatalf("existing nole entry should be replaced, not merged with old env secrets: %s", string(servers["nole"]))
	}
}

func TestWriteMCPJSONConfigCreatesNewFile0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "mcp.json")
	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write config: %v", err)
	}
	assertFileMode(t, path, 0600)
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("new config should not create backup, stat err=%v", err)
	}
}

func TestWriteOpenCodeConfigMergesExistingMCPAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	existing := `{"mcp":{"other":{"type":"local","command":["other","serve"],"enabled":true,"environment":{}}},"theme":"dark"}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	root := readJSONRoot(t, path)
	if !strings.Contains(string(root["theme"]), `"dark"`) {
		t.Fatalf("existing non-MCP fields not preserved: %s", string(root["theme"]))
	}
	servers := readRawObject(t, root["mcp"])
	if _, ok := servers["other"]; !ok {
		t.Fatalf("existing opencode MCP server was not preserved: %#v", servers)
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["type"]), `"`); got != "local" {
		t.Fatalf("nole.type = %q, want %q", got, "local")
	}
	var command []string
	if err := json.Unmarshal(nole["command"], &command); err != nil {
		t.Fatalf("nole.command not a string array: %v\n%s", err, string(nole["command"]))
	}
	if len(command) != 2 || command[0] != "/usr/local/bin/nole" || command[1] != "mcp" {
		t.Fatalf("nole.command = %#v, want [/usr/local/bin/nole mcp]", command)
	}
	if !strings.Contains(string(nole["enabled"]), "true") {
		t.Fatalf("nole.enabled = %s, want true", string(nole["enabled"]))
	}
	if _, ok := nole["environment"]; !ok {
		t.Fatalf("nole entry missing environment field: %s", string(servers["nole"]))
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteOpenCodeConfigPreservesUnknownMCPFieldsAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	existing := `{"mcp":{"other":{"type":"remote","url":"https://example.com/sse","transport":"sse","headers":{"X-Token":"SECRET"},"cwd":"/workspace","disabled":false},"nole":{"type":"local","command":["old"],"enabled":false,"environment":{"OLD_SECRET":"keep-out"}}},"theme":"dark","metadata":{"keep":true}}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	assertFileMode(t, path, 0640)
	assertFileMode(t, path+".bak", 0640)
	root := readJSONRoot(t, path)
	if !strings.Contains(string(root["metadata"]), `"keep": true`) && !strings.Contains(string(root["metadata"]), `"keep":true`) {
		t.Fatalf("unknown root metadata not preserved: %s", string(root["metadata"]))
	}
	servers := readRawObject(t, root["mcp"])
	other := readRawObject(t, servers["other"])
	for _, want := range []string{"type", "url", "transport", "headers", "cwd", "disabled"} {
		if _, ok := other[want]; !ok {
			t.Fatalf("unknown opencode mcp field %q not preserved: %s", want, string(servers["other"]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	var command []string
	if err := json.Unmarshal(nole["command"], &command); err != nil {
		t.Fatalf("nole.command not array after upsert: %v\n%s", err, string(nole["command"]))
	}
	if len(command) != 2 || command[0] != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %#v, want [/usr/local/bin/nole mcp]", command)
	}
	if _, ok := nole["environment"]; !ok {
		t.Fatalf("nole entry must keep an environment field: %s", string(servers["nole"]))
	}
	// The old environment value must be replaced (not merged) so stale secrets
	// can't linger after an upsert.
	if strings.Contains(string(nole["environment"]), "OLD_SECRET") {
		t.Fatalf("nole entry should not inherit stale environment secrets: %s", string(nole["environment"]))
	}
}

func TestWriteCodexConfigMergesNoleSectionAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := "model = \"gpt-5\"\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"model = \"gpt-5\"", "[mcp_servers.other]", "[mcp_servers.nole]", "command = \"/bin/sh\"", ".config/nole/.env", "/usr/local/bin/nole"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged codex config missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteCodexConfigPreservesExistingModeAndBackupMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := "model = \"gpt-5\"\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	assertFileMode(t, path, 0600)
	assertFileMode(t, path+".bak", 0600)
}

func TestDoctorCommandMCPFlagReportsStdioSmoke(t *testing.T) {
	bin := buildNoleBinary(t)
	t.Setenv("NOLE_MCP_SMOKE_BINARY", bin)
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor", "--mcp"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor --mcp failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "mcp:") || !strings.Contains(text, "initialize") || !strings.Contains(text, "tools/list") {
		t.Fatalf("doctor --mcp should report MCP protocol health, got:\n%s", text)
	}
}

func TestDoctorCommandReportsCostPolicyWithoutSecrets(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	// Opt into paid mode so doctor surfaces the premium-capable formatting
	// path. Default behavior post-Phase-B is free-tier-BYOK; the premium path
	// is what this test verifies (the default-BYOK path is exercised by
	// TestDoctorCommandFreeTierBYOKByDefault below).
	t.Setenv("NOLE_TAVILY_PAID", "1")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"policy=free-first", "no_hidden_paid_spend=true", "premium-capable", "premium_blocked_free_first"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "placeholder-test-key") {
		t.Fatalf("doctor output leaked provider key:\n%s", text)
	}
}

func TestDoctorCommandFreeTierBYOKByDefault(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"cost=free-tier-BYOK", "reason=free_tier_available", "free_remaining=1000"} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q (BYOK default behavior):\n%s", want, text)
		}
	}
}

func TestDoctorCommandSurfacesBraveCCWarning(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "placeholder-test-key")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "brave_note:") || !strings.Contains(text, "CC on file") {
		t.Fatalf("doctor should surface Brave CC warning when key is set:\n%s", text)
	}
	if !strings.Contains(text, "caps usage at the monthly free quota") {
		t.Fatalf("free-mode brave_note should mention the monthly free-quota cap:\n%s", text)
	}
}

func TestDoctorCommandBraveCCWarningInPaidModeDropsMonthlyCapClaim(t *testing.T) {
	// Regression for codex P2 (PR #21 round 3): the doctor warning used to
	// say "nole caps usage at the monthly free quota" regardless of mode,
	// which is false when NOLE_BRAVE_PAID=1 is set. Verify the paid-mode
	// wording acknowledges the cap is gone and points at the cost policy.
	t.Setenv("BRAVE_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_BRAVE_PAID", "1")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "brave_note:") || !strings.Contains(text, "CC on file") {
		t.Fatalf("paid-mode brave_note should still surface the CC caution:\n%s", text)
	}
	if strings.Contains(text, "caps usage at the monthly free quota") {
		t.Fatalf("paid-mode brave_note must NOT claim a monthly free-quota cap is active:\n%s", text)
	}
	if !strings.Contains(text, "NOLE_BRAVE_PAID=1") || !strings.Contains(text, "cost policy") {
		t.Fatalf("paid-mode brave_note should name NOLE_BRAVE_PAID=1 and cost policy as the actual guard:\n%s", text)
	}
}

func TestDoctorCommandSurfacesPaidModeWarning(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "placeholder-test-key")
	t.Setenv("NOLE_TAVILY_PAID", "1")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "paid_mode_active:") || !strings.Contains(text, "tavily") {
		t.Fatalf("doctor should list paid-mode providers:\n%s", text)
	}
}

func TestDoctorCommandMCPFlagReturnsErrorOnSmokeFailure(t *testing.T) {
	t.Setenv("NOLE_MCP_SMOKE_BINARY", filepath.Join(t.TempDir(), "missing-nole"))
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor", "--mcp"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("doctor --mcp should return an error when subprocess smoke fails; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "mcp: failed") {
		t.Fatalf("doctor --mcp should report failed MCP status, got:\n%s", out.String())
	}
}

func TestMCPStdioSmokeDoesNotWriteStartupNoiseToStdout(t *testing.T) {
	result := checkMCPStdioSmoke(context.Background())
	if !result.OK {
		t.Fatalf("expected mcp stdio smoke ok, got: %#v", result)
	}
	if result.StdoutBytes != 0 {
		t.Fatalf("expected no startup stdout bytes, got %d", result.StdoutBytes)
	}
}

func TestMCPProtocolSmokeRunsSubprocessInitializeAndToolsList(t *testing.T) {
	bin := buildNoleBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result := checkMCPProtocolSmoke(ctx, bin)
	if !result.OK {
		t.Fatalf("expected mcp protocol smoke ok, got: %#v", result)
	}
	for _, want := range []string{"budget_status", "extract", "provider_status", "search"} {
		if !containsString(result.Tools, want) {
			t.Fatalf("expected tool %q in %#v", want, result.Tools)
		}
	}
	if result.StdoutBytes == 0 {
		t.Fatalf("expected JSON-RPC stdout bytes from subprocess smoke")
	}
	if result.NonJSONStdoutLines != 0 {
		t.Fatalf("expected JSON-only stdout, got %d non-json lines", result.NonJSONStdoutLines)
	}
}

func buildNoleBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "nole-test")
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build nole test binary: %v\n%s", err, string(out))
	}
	return bin
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %#o, want %#o", path, got, want)
	}
}

func readJSONRoot(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return readRawObject(t, b)
}

func readRawObject(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal raw object: %v\n%s", err, string(b))
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
