package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupAntigravityFlagWritesGlobalMCPConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--antigravity"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --antigravity: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "antigravity: configured") {
		t.Fatalf("expected antigravity configured log, got:\n%s", out.String())
	}
	path := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("antigravity setup did not create %s: %v", path, err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got == "" {
		t.Fatalf("nole.command empty in antigravity config: %s", string(servers["nole"]))
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil {
		t.Fatalf("nole.args not a string array: %v\n%s", err, string(nole["args"]))
	}
	if len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("nole.args = %#v, want [mcp]", args)
	}
}

func TestWriteAntigravityConfigFreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write antigravity config: %v", err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %q, want /usr/local/bin/nole", got)
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("nole.args = %s err=%v", string(nole["args"]), err)
	}
	if _, hasEnv := nole["env"]; hasEnv {
		t.Fatalf("antigravity setup must not embed credentials/env by default: %s", string(servers["nole"]))
	}
	assertFileMode(t, path, 0600)
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("new antigravity config should not create backup, stat err=%v", err)
	}
}

func TestWriteAntigravityConfigPreservesMergeShapeAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	existing := `{"mcpServers":{"remote":{"serverUrl":"https://mcp.example.test/sse","headers":{"X-Example":"redacted"},"timeout":30},"nole":{"command":"old","args":["old"],"env":{"OLD_VALUE":"keep-out"}}},"unknownRoot":{"keep":true}}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write antigravity config: %v", err)
	}
	assertFileMode(t, path, 0640)
	assertFileMode(t, path+".bak", 0640)
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != existing {
		t.Fatalf("backup must preserve exact existing bytes, got %s", string(backup))
	}

	root := readJSONRoot(t, path)
	if !strings.Contains(string(root["unknownRoot"]), `"keep"`) {
		t.Fatalf("unknown root field not preserved: %s", string(root["unknownRoot"]))
	}
	servers := readRawObject(t, root["mcpServers"])
	remote := readRawObject(t, servers["remote"])
	for _, want := range []string{"serverUrl", "headers", "timeout"} {
		if _, ok := remote[want]; !ok {
			t.Fatalf("unknown antigravity server field %q lost: %s", want, string(servers["remote"]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %q, want /usr/local/bin/nole", got)
	}
	if _, hasEnv := nole["env"]; hasEnv {
		t.Fatalf("old nole env should be replaced, not preserved into launch entry: %s", string(servers["nole"]))
	}
}

func TestWriteAntigravityConfigIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	spec := launchSpec{Binary: "/usr/local/bin/nole"}
	if err := writeAntigravityConfigPath(path, spec); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAntigravityConfigPath(path, spec); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("antigravity setup not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestWriteAntigravityConfigWrapperMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_config.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write antigravity config: %v", err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != wrapper {
		t.Fatalf("nole.command = %q, want wrapper %q", got, wrapper)
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil {
		t.Fatalf("nole.args not array: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("wrapper-mode antigravity entry should have empty args, got %#v", args)
	}
}

func TestSetupAllIncludesAntigravity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --all: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "antigravity: configured") {
		t.Fatalf("--all did not configure antigravity:\n%s", out.String())
	}
	path := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("--all did not create antigravity config %s: %v", path, err)
	}
}

func TestWriteAntigravityConfigMalformedJSONErrorsWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":`), 0600); err != nil {
		t.Fatal(err)
	}
	err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
	if err == nil {
		t.Fatalf("expected malformed antigravity config to error")
	}
	if !strings.Contains(err.Error(), "parse existing json config") {
		t.Fatalf("unexpected malformed error: %v", err)
	}
	if _, statErr := os.Stat(path + ".bak"); !os.IsNotExist(statErr) {
		t.Fatalf("malformed config should not be backed up/written, stat err=%v", statErr)
	}
}
