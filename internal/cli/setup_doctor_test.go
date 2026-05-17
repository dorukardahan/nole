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
	if err := writeMCPJSONConfig(path, "/usr/local/bin/nole"); err != nil {
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
	if err := writeMCPJSONConfig(path, "/usr/local/bin/nole"); err != nil {
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
	if err := writeMCPJSONConfig(path, "/usr/local/bin/nole"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	assertFileMode(t, path, 0600)
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("new config should not create backup, stat err=%v", err)
	}
}

func TestWriteOpenCodeConfigMergesExistingMCPAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	existing := `{"mcp":{"other":{"command":"other","args":["serve"]}},"theme":"dark"}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfigPath(path, "/usr/local/bin/nole"); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	var cfg struct {
		MCP   map[string]mcpServerEntry `json:"mcp"`
		Theme string                    `json:"theme"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(b))
	}
	if cfg.Theme != "dark" {
		t.Fatalf("existing non-MCP fields not preserved: %#v", cfg)
	}
	if _, ok := cfg.MCP["other"]; !ok {
		t.Fatalf("existing opencode MCP server was not preserved: %#v", cfg.MCP)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteOpenCodeConfigPreservesUnknownMCPFieldsAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	existing := `{"mcp":{"other":{"url":"https://example.com/sse","transport":"sse","headers":{"X-Token":"SECRET"},"cwd":"/workspace","disabled":false},"nole":{"command":"old","args":["old"]}},"theme":"dark","metadata":{"keep":true}}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfigPath(path, "/usr/local/bin/nole"); err != nil {
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
	for _, want := range []string{"url", "transport", "headers", "cwd", "disabled"} {
		if _, ok := other[want]; !ok {
			t.Fatalf("unknown opencode mcp field %q not preserved: %s", want, string(servers["other"]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole command = %q", got)
	}
}

func TestWriteCodexConfigMergesNoleSectionAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := "model = \"gpt-5\"\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfigPath(path, "/usr/local/bin/nole"); err != nil {
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
	if err := writeCodexConfigPath(path, "/usr/local/bin/nole"); err != nil {
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
