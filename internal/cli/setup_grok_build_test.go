package cli

import (
	"bytes"
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

// TestWriteGrokBuildConfigPreservesSiblingsAndRoot guards the table upsert: an
// existing config.toml with a root key and a sibling [mcp_servers.other] table must
// survive; only the [mcp_servers.nole] table is (re)written (its stale launch keys
// replaced). (A customized nole entry is refused instead — see the refuse test.)
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
	if strings.Contains(got, "/old/nole") {
		t.Errorf("stale nole command not replaced:\n%s", got)
	}
	if !strings.Contains(got, `command = "/usr/local/bin/nole"`) {
		t.Errorf("new nole command missing:\n%s", got)
	}
}

// TestWriteGrokBuildConfigRefusesToClobber pins the AGENTS.md preserve-config
// contract: a nole entry with user-owned content the writer does not manage (a
// [mcp_servers.nole.*] sub-table, or an extra direct key) must NOT be silently
// dropped. The writer refuses with an error and leaves the file byte-for-byte
// untouched (no write, no backup).
func TestWriteGrokBuildConfigRefusesToClobber(t *testing.T) {
	cases := map[string]string{
		"sub-table": `[mcp_servers.nole]
command = "/old/nole"
args = ["mcp"]
enabled = false

[mcp_servers.nole.env]
NOLE_SECRET_OVERRIDE = "keep me"
`,
		"extra-direct-key": `[mcp_servers.nole]
command = "/old/nole"
args = ["mcp"]
timeout = 30
`,
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
				t.Fatal(err)
			}
			err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
			if err == nil {
				t.Fatal("expected a refusal error when the nole entry has unmanaged content")
			}
			if !strings.Contains(err.Error(), "refusing to overwrite") {
				t.Errorf("unexpected error text: %v", err)
			}
			if got := readFileString(t, path); got != existing {
				t.Fatalf("file must be left untouched on refusal:\n%s", got)
			}
			if _, statErr := os.Stat(path + ".bak"); statErr == nil {
				t.Fatal("no backup should be written on refusal")
			}
		})
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

// TestWriteGrokBuildConfigHandlesCommentedHeader pins the Codex P2 fix: a TOML
// table header with an inline comment ([mcp_servers.nole] # ...) must still be
// recognized so (1) a user-set enabled=false is preserved and (2) the table is
// REPLACED in place rather than left behind while a duplicate [mcp_servers.nole]
// is appended (which would be invalid TOML).
func TestWriteGrokBuildConfigHandlesCommentedHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := `[mcp_servers.nole]  # my pinned server
command = "/old/nole"
args = ["mcp"]
enabled = false # disabled for now
`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeGrokBuildConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFileString(t, path)
	if strings.Count(got, "[mcp_servers.nole]") != 1 {
		t.Fatalf("annotated header must be replaced in place, not duplicated (invalid TOML):\n%s", got)
	}
	if !strings.Contains(got, "enabled = false") {
		t.Errorf("enabled=false (set on an annotated header's table) must be preserved:\n%s", got)
	}
	if strings.Contains(got, "/old/nole") {
		t.Errorf("launch-critical command should still be updated:\n%s", got)
	}
}

// TestTomlTableHeader unit-pins the header parser: standard/array tables, inline
// comments (incl. a ']' inside the comment), and non-headers.
func TestTomlTableHeader(t *testing.T) {
	cases := []struct {
		in   string
		name string
		ok   bool
	}{
		{"[mcp_servers.nole]", "mcp_servers.nole", true},
		{"[mcp_servers.nole] # note", "mcp_servers.nole", true},
		{"[mcp_servers.nole]   # see ]bracket in comment", "mcp_servers.nole", true},
		{"[[mcp_servers]]", "mcp_servers", true},
		{"[[mcp_servers]] # c", "mcp_servers", true},
		{`model = "x"`, "", false},
		{"enabled = false", "", false},
		{"# just a comment", "", false},
		{"[unterminated", "", false},
	}
	for _, c := range cases {
		gotName, gotOK := tomlTableHeader(c.in)
		if gotOK != c.ok || (gotOK && gotName != c.name) {
			t.Errorf("tomlTableHeader(%q) = (%q,%v), want (%q,%v)", c.in, gotName, gotOK, c.name, c.ok)
		}
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

// TestSetupGrokBuildCommandFailsWhenRefused pins that `nole setup --grok-build`
// exits non-zero (RunE returns an error) when the only requested agent is refused,
// rather than misreporting success with exit 0.
func TestSetupGrokBuildCommandFailsWhenRefused(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	grokDir := filepath.Join(dir, ".grok")
	if err := os.MkdirAll(grokDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := "[mcp_servers.nole]\ncommand = \"/old\"\nargs = [\"mcp\"]\n\n[mcp_servers.nole.env]\nKEY = \"v\"\n"
	if err := os.WriteFile(filepath.Join(grokDir, "config.toml"), []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--grok-build"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("setup --grok-build must exit non-zero when the only requested agent is refused:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "could not be configured") {
		t.Errorf("unexpected error: %v", err)
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
