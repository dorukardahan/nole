package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Grok CLI (superagent-ai/grok-cli, @vibe-kit/grok-cli) reads
// ~/.grok/user-settings.json with a top-level "mcp" object whose "servers" is
// an ARRAY of server objects keyed by an "id" field — unlike every other
// writer's object-keyed schema. See docs/RESEARCH-FINDINGS.md §1 for the
// primary-source citations (src/utils/settings.ts:92-106,185-186,651).

// grokServers decodes mcp.servers from a written Grok config for assertions.
func grokServers(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	root := readJSONRoot(t, path)
	mcp := readRawObject(t, root["mcp"])
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(mcp["servers"], &arr); err != nil {
		t.Fatalf("mcp.servers not an array of objects: %v\n%s", err, string(mcp["servers"]))
	}
	return arr
}

func findGrokServer(servers []map[string]json.RawMessage, id string) map[string]json.RawMessage {
	for _, s := range servers {
		if strings.Trim(string(s["id"]), `"`) == id {
			return s
		}
	}
	return nil
}

func TestSetupGrokFlagWritesUserScopeFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--grok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --grok: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "grok: configured") {
		t.Fatalf("expected grok configured log, got:\n%s", out.String())
	}
	path := filepath.Join(home, ".grok", "user-settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("grok setup did not create %s: %v", path, err)
	}
	nole := findGrokServer(grokServers(t, path), "nole")
	if nole == nil {
		t.Fatalf("grok setup did not register a nole server")
	}
}

func TestWriteGrokConfigBareEmitsStdioEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-settings.json")
	if err := writeGrokConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write grok config: %v", err)
	}
	nole := findGrokServer(grokServers(t, path), "nole")
	if nole == nil {
		t.Fatal("nole entry missing")
	}
	if got := strings.Trim(string(nole["label"]), `"`); got != "nole" {
		t.Fatalf("label = %q, want nole", got)
	}
	if string(nole["enabled"]) != "true" {
		t.Fatalf("enabled = %s, want true", string(nole["enabled"]))
	}
	if got := strings.Trim(string(nole["transport"]), `"`); got != "stdio" {
		t.Fatalf("transport = %q, want stdio", got)
	}
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("command = %q, want /usr/local/bin/nole", got)
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("args = %s err=%v, want [mcp]", string(nole["args"]), err)
	}
	assertFileMode(t, path, 0600)
}

func TestWriteGrokConfigWrapperUsesWrapperPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user-settings.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeGrokConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write grok config: %v", err)
	}
	nole := findGrokServer(grokServers(t, path), "nole")
	if got := strings.Trim(string(nole["command"]), `"`); got != wrapper {
		t.Fatalf("command = %q, want wrapper %q", got, wrapper)
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil {
		t.Fatalf("args not array: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("wrapper-mode grok entry should have empty args, got %#v", args)
	}
}

func TestWriteGrokConfigUpsertsInPlacePreservingOthersAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-settings.json")
	existing := `{
  "apiKey": "GROK_KEY_PLACEHOLDER",
  "model": "grok-3-fast",
  "mcp": {
    "servers": [
      {"id":"linear","label":"linear","enabled":true,"transport":"stdio","command":"npx","args":["@linear/mcp-server"]},
      {"id":"nole","label":"My Custom Nole","enabled":false,"transport":"stdio","command":"/old/nole","args":["old"],"note":"keep-me"}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write grok config: %v", err)
	}
	assertFileMode(t, path, 0640)

	root := readJSONRoot(t, path)
	for _, k := range []string{"apiKey", "model"} {
		if _, ok := root[k]; !ok {
			t.Fatalf("unknown root field %q not preserved: %v", k, root)
		}
	}
	servers := grokServers(t, path)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers (linear + nole upserted in place), got %d: %v", len(servers), servers)
	}
	if findGrokServer(servers, "linear") == nil {
		t.Fatalf("existing linear server lost")
	}
	nole := findGrokServer(servers, "nole")
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command not updated: %q", got)
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("nole.args not updated to [mcp]: %s", string(nole["args"]))
	}
	// Launch-critical transport overwritten to stdio; user identity/policy and
	// unknown fields preserved.
	if got := strings.Trim(string(nole["transport"]), `"`); got != "stdio" {
		t.Fatalf("nole.transport = %q, want stdio", got)
	}
	if got := strings.Trim(string(nole["label"]), `"`); got != "My Custom Nole" {
		t.Fatalf("existing label not preserved: %q", got)
	}
	if string(nole["enabled"]) != "false" {
		t.Fatalf("existing enabled flag not preserved: %s", string(nole["enabled"]))
	}
	if got := strings.Trim(string(nole["note"]), `"`); got != "keep-me" {
		t.Fatalf("unknown per-entry field not preserved: %s", string(nole["note"]))
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteGrokConfigAppendsWhenNoleAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-settings.json")
	existing := `{"mcp":{"servers":[{"id":"linear","label":"linear","enabled":true,"transport":"stdio","command":"npx","args":["@linear/mcp-server"]}]}}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write grok config: %v", err)
	}
	servers := grokServers(t, path)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers after append, got %d", len(servers))
	}
	if findGrokServer(servers, "linear") == nil || findGrokServer(servers, "nole") == nil {
		t.Fatalf("append did not preserve linear and add nole: %v", servers)
	}
}

func TestWriteGrokConfigIdempotent(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec launchSpec
	}{
		{"bare", launchSpec{Binary: "/usr/local/bin/nole"}},
		{"wrapper", launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "user-settings.json")
			if err := writeGrokConfigPath(path, tc.spec); err != nil {
				t.Fatalf("first write: %v", err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeGrokConfigPath(path, tc.spec); err != nil {
				t.Fatalf("second write: %v", err)
			}
			second, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(first) != string(second) {
				t.Fatalf("grok setup not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

func TestWriteGrokConfigRejectsNonArrayServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-settings.json")
	existing := `{"mcp":{"servers":"disabled"}}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	err := writeGrokConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
	if err == nil {
		t.Fatal("expected error when existing mcp.servers is not an array")
	}
	if !strings.Contains(err.Error(), "servers") {
		t.Fatalf("expected mcp.servers error, got %q", err.Error())
	}
}
