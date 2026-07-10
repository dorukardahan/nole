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
	if !strings.Contains(out.String(), "1 agent(s) configured") {
		t.Fatalf("expected antigravity in setup summary, got:\n%s", out.String())
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

func TestSetupAntigravityAppearsInHelpAndSelectionValidation(t *testing.T) {
	helpCmd := NewRootCommand()
	var help bytes.Buffer
	helpCmd.SetOut(&help)
	helpCmd.SetErr(&help)
	helpCmd.SetArgs([]string{"setup", "--help"})
	if err := helpCmd.Execute(); err != nil {
		t.Fatalf("setup --help: %v", err)
	}
	for _, want := range []string{"--antigravity", "~/.gemini/config/mcp_config.json", "--gemini"} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("setup help missing %q:\n%s", want, help.String())
		}
	}

	validationCmd := NewRootCommand()
	validationCmd.SetArgs([]string{"setup"})
	err := validationCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--antigravity") {
		t.Fatalf("setup selection error should list --antigravity, got %v", err)
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
	existing := `{"mcpServers":{"remote":{"serverUrl":"https://mcp.example.test/sse","headers":{"X-Example":"redacted"},"timeout":30},"migrated":{"url":"https://mcp.example.test/mcp"},"legacy":{"httpUrl":"https://mcp.example.test/legacy"},"nole":{"command":"old","args":["old"],"env":{"EXISTING_VALUE":"preserve"},"cwd":"/existing/workspace","disabled":true,"disabledTools":["search"],"timeout":45,"documentationUrl":"https://docs.example.test/nole"}},"unknownRoot":{"keep":true}}`
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
	for serverName, remoteKey := range map[string]string{"migrated": "url", "legacy": "httpUrl"} {
		sibling := readRawObject(t, servers[serverName])
		if _, ok := sibling[remoteKey]; !ok {
			t.Fatalf("remote sibling %q field %q lost: %s", serverName, remoteKey, string(servers[serverName]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %q, want /usr/local/bin/nole", got)
	}
	for _, want := range []string{"env", "cwd", "disabled", "disabledTools", "timeout", "documentationUrl"} {
		if _, ok := nole[want]; !ok {
			t.Fatalf("existing antigravity nole field %q lost: %s", want, string(servers["nole"]))
		}
	}
}

func TestWriteAntigravityConfigRejectsExistingRemoteNoleWithoutBackup(t *testing.T) {
	for _, remoteKey := range []string{"serverUrl", "serverURL", "url", "httpUrl"} {
		t.Run(remoteKey, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp_config.json")
			existing := `{"mcpServers":{"nole":{"` + remoteKey + `":"https://mcp.example.test/transport","headers":{"Authorization":"preserve"},"disabled":true}}}`
			if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}

			err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
			if err == nil || !strings.Contains(err.Error(), remoteKey) || !strings.Contains(err.Error(), "local stdio") {
				t.Fatalf("expected %s remote Nólë entry conflict, got %v", remoteKey, err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != existing {
				t.Fatalf("remote Nólë config was modified: %s", got)
			}
			assertFileMode(t, path, 0640)
			if _, statErr := os.Stat(path + ".bak"); !os.IsNotExist(statErr) {
				t.Fatalf("remote conflict should not be backed up/written, stat err=%v", statErr)
			}
		})
	}
}

func TestWriteAntigravityConfigRejectsNonObjectNoleWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	existing := `{"mcpServers":{"nole":false}}`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
	if err == nil || !strings.Contains(err.Error(), "mcpServers.nole") {
		t.Fatalf("expected non-object nole error, got %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != existing {
		t.Fatalf("invalid config was modified: %s", got)
	}
	if _, statErr := os.Stat(path + ".bak"); !os.IsNotExist(statErr) {
		t.Fatalf("invalid config should not be backed up/written, stat err=%v", statErr)
	}
}

func TestWriteAntigravityConfigRejectsNullObjectsWithoutBackup(t *testing.T) {
	for name, existing := range map[string]string{
		"root":    `null`,
		"servers": `{"mcpServers":null}`,
		"nole":    `{"mcpServers":{"nole":null}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp_config.json")
			if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
				t.Fatal(err)
			}
			err := writeAntigravityConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
			if err == nil || !strings.Contains(err.Error(), "must be an object") {
				t.Fatalf("expected null object error, got %v", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != existing {
				t.Fatalf("null config was modified: %s", got)
			}
			assertFileMode(t, path, 0640)
			if _, statErr := os.Stat(path + ".bak"); !os.IsNotExist(statErr) {
				t.Fatalf("invalid config should not be backed up/written, stat err=%v", statErr)
			}
		})
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
