package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gemini CLI reads ~/.gemini/settings.json with a top-level "mcpServers"
// object keyed by server name (object-keyed-by-name, shallow-merged) — the
// same shape Cursor uses. See docs/RESEARCH-FINDINGS.md §1 for the
// primary-source citations (google-gemini/gemini-cli settingsSchema.ts:161,
// commands/mcp/add.ts, storage.ts/paths.ts).

func TestSetupGeminiFlagWritesUserScopeFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--gemini"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --gemini: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "gemini: configured") {
		t.Fatalf("expected gemini configured log, got:\n%s", out.String())
	}
	path := filepath.Join(home, ".gemini", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("gemini setup did not create %s: %v", path, err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got == "" {
		t.Fatalf("nole.command empty in gemini config: %s", string(servers["nole"]))
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil {
		t.Fatalf("nole.args not a string array: %v\n%s", err, string(nole["args"]))
	}
	if len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("nole.args = %#v, want [mcp]", args)
	}
}

func TestWriteGeminiConfigBareEmitsCommandAndArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeGeminiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write gemini config: %v", err)
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
	assertFileMode(t, path, 0600)
}

func TestWriteGeminiConfigWrapperUsesWrapperPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeGeminiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write gemini config: %v", err)
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
		t.Fatalf("wrapper-mode gemini entry should have empty args, got %#v", args)
	}
}

func TestWriteGeminiConfigPreservesUnknownRootAndOtherServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"mcpServers":{"other":{"command":"/usr/bin/other","args":["serve"],"env":{"X":"Y"}}},"theme":"GitHub","contextFileName":"GEMINI.md"}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeGeminiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write gemini config: %v", err)
	}
	assertFileMode(t, path, 0640)
	root := readJSONRoot(t, path)
	for _, k := range []string{"theme", "contextFileName"} {
		if _, ok := root[k]; !ok {
			t.Fatalf("unknown root field %q not preserved: %v", k, root)
		}
	}
	servers := readRawObject(t, root["mcpServers"])
	other := readRawObject(t, servers["other"])
	for _, want := range []string{"command", "args", "env"} {
		if _, ok := other[want]; !ok {
			t.Fatalf("unknown gemini server field %q lost: %s", want, string(servers["other"]))
		}
	}
	if _, ok := servers["nole"]; !ok {
		t.Fatalf("nole entry not added: %#v", servers)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}
