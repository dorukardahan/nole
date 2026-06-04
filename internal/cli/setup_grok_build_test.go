package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteGrokBuildConfigBareShape pins the TOML block xAI's Grok Build TUI reads
// (verified 2026-06-04 via `grok mcp doctor`): a flat [mcp_servers.nole] table with
// command, args=["mcp"] (bare), and enabled=true.
func TestWriteGrokBuildConfigBareShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFileString(t, path)
	for _, want := range []string{
		"[mcp_servers.nole]",
		`command = "/usr/local/bin/nole"`,
		`args = ["mcp"]`,
		"enabled = true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml missing %q:\n%s", want, got)
		}
	}
}

// TestWriteGrokBuildConfigWrapperShape pins the wrapper form: command points at the
// wrapper and args is the empty TOML array (the wrapper exec's `nole mcp` itself).
func TestWriteGrokBuildConfigWrapperShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFileString(t, path)
	if !strings.Contains(got, `command = "/usr/local/bin/nole-mcp"`) {
		t.Errorf("wrapper command missing:\n%s", got)
	}
	if !strings.Contains(got, "args = []") {
		t.Errorf("wrapper args should be the empty TOML array:\n%s", got)
	}
}

// TestWriteGrokBuildConfigPreservesSiblingsAndRoot guards the wholesale-table
// upsert: an existing config.toml with a root key and a sibling [mcp_servers.other]
// table must survive; only the [mcp_servers.nole] table is (re)written, and its old
// contents (including a stale [mcp_servers.nole.env] sub-table) are dropped.
func TestWriteGrokBuildConfigPreservesSiblingsAndRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := `model = "grok-4"

[mcp_servers.other]
command = "/usr/bin/other"
args = ["run"]
enabled = true

[mcp_servers.nole]
command = "/old/nole"
args = ["mcp"]
enabled = true

[mcp_servers.nole.env]
STALE = "value"
`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFileString(t, path)
	if !strings.Contains(got, `model = "grok-4"`) {
		t.Errorf("root key not preserved:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.other]") || !strings.Contains(got, `/usr/bin/other`) {
		t.Errorf("sibling MCP server not preserved:\n%s", got)
	}
	if strings.Contains(got, "/old/nole") || strings.Contains(got, "[mcp_servers.nole.env]") || strings.Contains(got, "STALE") {
		t.Errorf("stale nole table/sub-table not replaced:\n%s", got)
	}
	if !strings.Contains(got, `command = "/usr/local/bin/nole"`) {
		t.Errorf("new nole command missing:\n%s", got)
	}
}

// TestWriteGrokBuildConfigPreservesEnabledFalse pins user agency: a user who set
// enabled=false on the nole entry keeps that across a re-run (we do not force it
// back to true), matching the superagent Grok writer's preserve-enabled intent.
func TestWriteGrokBuildConfigPreservesEnabledFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := `[mcp_servers.nole]
command = "/old/nole"
args = ["mcp"]
enabled = false
`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFileString(t, path)
	if !strings.Contains(got, "enabled = false") {
		t.Errorf("a user-set enabled=false must be preserved across re-run:\n%s", got)
	}
	if strings.Contains(got, "/old/nole") {
		t.Errorf("launch-critical command should still be updated:\n%s", got)
	}
	// First write (no existing) defaults enabled to true.
	fresh := filepath.Join(t.TempDir(), "config.toml")
	if err := writeGrokBuildConfigPath(fresh, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("fresh write: %v", err)
	}
	if !strings.Contains(readFileString(t, fresh), "enabled = true") {
		t.Errorf("first write should default enabled=true")
	}
}

// TestExistingGrokBuildEnabled unit-pins the enabled reader: default true when
// absent, the value when present, and that a sibling table's enabled is never read
// as nole's.
func TestExistingGrokBuildEnabled(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", true},
		{"no-nole-table", "[mcp_servers.other]\nenabled = false\n", true},
		{"nole-true", "[mcp_servers.nole]\nenabled = true\n", true},
		{"nole-false", "[mcp_servers.nole]\ncommand = \"x\"\nenabled = false\n", false},
		{"nole-false-inline-comment", "[mcp_servers.nole]\nenabled = false # disabled for now\n", false},
		{"nole-true-inline-comment", "[mcp_servers.nole]\nenabled = true  # on\n", true},
		{"nole-missing-key", "[mcp_servers.nole]\ncommand = \"x\"\n", true},
		{"sibling-false-nole-default", "[mcp_servers.other]\nenabled = false\n\n[mcp_servers.nole]\ncommand = \"x\"\n", true},
		{"subtable-does-not-bleed", "[mcp_servers.nole]\ncommand = \"x\"\n\n[mcp_servers.nole.env]\nenabled = false\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := existingGrokBuildEnabled(tc.in); got != tc.want {
				t.Fatalf("existingGrokBuildEnabled(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestWriteGrokBuildConfigBacksUpExisting confirms a pre-existing config is backed
// up before the in-place rewrite.
func TestWriteGrokBuildConfigBacksUpExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "[mcp_servers.nole]\ncommand = \"/old/nole\"\nargs = [\"mcp\"]\nenabled = true\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	bak := readFileString(t, path+".bak")
	if bak != original {
		t.Fatalf("backup should match the original byte-for-byte:\ngot:\n%s\nwant:\n%s", bak, original)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
