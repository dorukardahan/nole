package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSetupClaudeFlagPrintsInstructionsAndWritesNoFile asserts that
// `nole setup --claude` no longer writes a misleading ~/.claude/mcp.json. It
// must print the official `claude mcp add` invocation instead.
func TestSetupClaudeFlagPrintsInstructionsAndWritesNoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --claude: %v\n%s", err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "claude: instructions") {
		t.Fatalf("expected instruction header, got:\n%s", text)
	}
	if !strings.Contains(text, "claude mcp add nole -s user --") {
		t.Fatalf("expected claude mcp add command, got:\n%s", text)
	}
	// The previous writer would create ~/.claude/mcp.json; that path must stay
	// untouched now.
	stalePath := filepath.Join(home, ".claude", "mcp.json")
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("claude writer must not create %s, got err=%v", stalePath, err)
	}
	staleDir := filepath.Join(home, ".claude")
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Fatalf("claude writer must not create %s, got err=%v", staleDir, err)
	}
}

// The post-setup message must state up front that Nólë works with ZERO keys
// (keyless DDGS + optional local Scrapling), so onboarding doesn't imply keys
// are required. Locks the v0.9.0 keyless-aware onboarding line.
func TestSetupMessageStatesKeylessOperation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --claude: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"ZERO keys", "DDGS", "keyless", "OPTIONAL"} {
		if !strings.Contains(text, want) {
			t.Fatalf("keyless onboarding message missing %q:\n%s", want, text)
		}
	}
}

// TestSetupClaudeFlagWithWrapperUsesWrapperPath asserts that the printed
// instruction targets the wrapper path when --mcp-wrapper is given.
func TestSetupClaudeFlagWithWrapperUsesWrapperPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wrapper := filepath.Join(home, "wrap", "nole-mcp")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--claude", "--mcp-wrapper", wrapper})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --claude --mcp-wrapper: %v\n%s", err, out.String())
	}
	text := out.String()
	expected := "claude mcp add nole -s user -- " + wrapper + "\n"
	if !strings.Contains(text, expected) {
		t.Fatalf("expected wrapper invocation %q, got:\n%s", strings.TrimSpace(expected), text)
	}
}

func TestSetupRejectsRelativeWrapperPath(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--claude", "--mcp-wrapper", "relative/nole-mcp"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for relative --mcp-wrapper, got nil; output:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute-path error, got %q", err.Error())
	}
}

// TestResolveHomeDirReturnsErrorWhenUnset asserts the helper that backs the
// writers fails loudly when neither HOME nor an OS-level fallback yields a
// directory, rather than silently falling back to "" (which would have the
// writers create config under the current working directory).
func TestResolveHomeDirReturnsErrorWhenUnset(t *testing.T) {
	// Clearing HOME on darwin/linux is enough for os.UserHomeDir() to fail
	// inside Go's test sandbox; if a future Go release reads /etc/passwd as
	// a fallback even with HOME unset, this test will not fail closed and
	// will simply be a no-op — that's acceptable because the helper would
	// then have returned a real path.
	t.Setenv("HOME", "")
	home, err := resolveHomeDir()
	if err == nil && home != "" {
		// os.UserHomeDir() fell back to /etc/passwd; that's still a non-empty
		// path, which is what callers depend on.
		return
	}
	if err == nil {
		t.Fatalf("resolveHomeDir returned empty string without error")
	}
	if !strings.Contains(err.Error(), "home dir") {
		t.Fatalf("expected home-dir error message, got %q", err.Error())
	}
}

func TestSetupRequiresAtLeastOneClient(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when no client flag is supplied")
	}
}

func TestWriteKimiConfigDefaultEmitsCommandAndArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := writeKimiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write kimi config: %v", err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %q, want %q", got, "/usr/local/bin/nole")
	}
	var args []string
	if err := json.Unmarshal(nole["args"], &args); err != nil {
		t.Fatalf("nole.args not a string array: %v\n%s", err, string(nole["args"]))
	}
	if len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("nole.args = %#v, want [mcp]", args)
	}
	assertFileMode(t, path, 0600)
}

func TestWriteKimiConfigWrapperEmitsSingleCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeKimiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write kimi config: %v", err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != wrapper {
		t.Fatalf("nole.command = %q, want %q", got, wrapper)
	}
	if _, hasArgs := nole["args"]; hasArgs {
		t.Fatalf("wrapper mode kimi entry should not include args, got: %s", string(servers["nole"]))
	}
}

func TestWriteKimiConfigPreservesUnknownFieldsAndOtherServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	existing := `{"mcpServers":{"other":{"command":"/usr/bin/other","args":["serve"],"env":{"X":"Y"}},"nole":{"command":"old","args":["old"]}},"metadata":{"profile":"work"}}`
	if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeKimiConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write kimi config: %v", err)
	}
	assertFileMode(t, path, 0640)
	root := readJSONRoot(t, path)
	if !strings.Contains(string(root["metadata"]), `"profile"`) {
		t.Fatalf("unknown root field not preserved: %s", string(root["metadata"]))
	}
	servers := readRawObject(t, root["mcpServers"])
	other := readRawObject(t, servers["other"])
	for _, want := range []string{"command", "args", "env"} {
		if _, ok := other[want]; !ok {
			t.Fatalf("unknown kimi mcp field %q lost: %s", want, string(servers["other"]))
		}
	}
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != "/usr/local/bin/nole" {
		t.Fatalf("nole.command = %q, want /usr/local/bin/nole", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteOpenCodeConfigWrapperUsesWrapperPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeOpenCodeConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcp"])
	nole := readRawObject(t, servers["nole"])
	var command []string
	if err := json.Unmarshal(nole["command"], &command); err != nil {
		t.Fatalf("nole.command not a string array: %v\n%s", err, string(nole["command"]))
	}
	if len(command) != 1 || command[0] != wrapper {
		t.Fatalf("wrapper-mode opencode command = %#v, want [%s]", command, wrapper)
	}
	if got := strings.Trim(string(nole["type"]), `"`); got != "local" {
		t.Fatalf("nole.type = %q, want local", got)
	}
}

func TestWriteCursorConfigWrapperUsesWrapperPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write cursor-style config: %v", err)
	}
	var cfg mcpConfig
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(b))
	}
	if got := cfg.McpServers["nole"].Command; got != wrapper {
		t.Fatalf("nole.command = %q, want %q", got, wrapper)
	}
	if len(cfg.McpServers["nole"].Args) != 0 {
		t.Fatalf("wrapper-mode entry should have no args, got %#v", cfg.McpServers["nole"].Args)
	}
}

func TestWriteCodexConfigWrapperBypassesShellLaunch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	if err := writeCodexConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write codex wrapper config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "[mcp_servers.nole]") {
		t.Fatalf("codex wrapper output missing nole section:\n%s", text)
	}
	if !strings.Contains(text, "command = \""+wrapper+"\"") {
		t.Fatalf("codex wrapper output missing wrapper command:\n%s", text)
	}
	if !strings.Contains(text, "args = []") {
		t.Fatalf("codex wrapper output missing empty args:\n%s", text)
	}
	if strings.Contains(text, "/bin/sh") {
		t.Fatalf("codex wrapper output should not embed /bin/sh launch line:\n%s", text)
	}
	if strings.Contains(text, ".config/nole/.env") {
		t.Fatalf("codex wrapper output should rely on wrapper for env, not embed env-sourcing:\n%s", text)
	}
}

func TestSetupWritersIdempotent(t *testing.T) {
	cases := []struct {
		name string
		spec launchSpec
		ext  string
		fn   func(string, launchSpec) error
	}{
		{name: "kimi-bare", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeKimiConfigPath},
		{name: "kimi-wrapper", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeKimiConfigPath},
		{name: "opencode-bare", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeOpenCodeConfigPath},
		{name: "opencode-wrapper", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeOpenCodeConfigPath},
		{name: "cursor-bare", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeMCPJSONConfig},
		{name: "cursor-wrapper", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeMCPJSONConfig},
		{name: "antigravity-bare", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeAntigravityConfigPath},
		{name: "antigravity-wrapper", ext: "json", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeAntigravityConfigPath},
		{name: "codex-bare", ext: "toml", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeCodexConfigPath},
		{name: "codex-wrapper", ext: "toml", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeCodexConfigPath},
		{name: "grok-build-bare", ext: "toml", spec: launchSpec{Binary: "/usr/local/bin/nole"}, fn: writeGrokBuildConfigPath},
		{name: "grok-build-wrapper", ext: "toml", spec: launchSpec{Binary: "/usr/local/bin/nole", Wrapper: "/usr/local/bin/nole-mcp"}, fn: writeGrokBuildConfigPath},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config."+tc.ext)
			if err := tc.fn(path, tc.spec); err != nil {
				t.Fatalf("first write: %v", err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.fn(path, tc.spec); err != nil {
				t.Fatalf("second write: %v", err)
			}
			second, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(first) != string(second) {
				t.Fatalf("%s setup not idempotent:\nfirst:\n%s\nsecond:\n%s", tc.name, string(first), string(second))
			}
		})
	}
}

// TestSetupWrapperPathWithSpace exercises the printed instruction and the
// generated config when the wrapper path contains a space. Wrapper paths are
// user-supplied; the writers must round-trip them without splitting on
// whitespace.
func TestSetupWrapperPathWithSpace(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "User Apps", "nole-mcp")

	// Claude: printed instruction must keep the path as a single token.
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	t.Setenv("HOME", dir)
	cmd.SetArgs([]string{"setup", "--claude", "--mcp-wrapper", wrapper})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --claude --mcp-wrapper: %v\n%s", err, out.String())
	}
	want := "claude mcp add nole -s user -- " + wrapper + "\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("expected exact wrapper invocation %q, got:\n%s", strings.TrimSpace(want), out.String())
	}

	// Kimi: JSON encoding preserves the space verbatim.
	kimiPath := filepath.Join(dir, "mcp.json")
	if err := writeKimiConfigPath(kimiPath, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("kimi write: %v", err)
	}
	root := readJSONRoot(t, kimiPath)
	servers := readRawObject(t, root["mcpServers"])
	nole := readRawObject(t, servers["nole"])
	if got := strings.Trim(string(nole["command"]), `"`); got != wrapper {
		t.Fatalf("kimi command = %q, want %q", got, wrapper)
	}

	// Codex: TOML output uses Go's %q which escapes the space inside the
	// double-quoted string; the literal path bytes must still appear.
	codexPath := filepath.Join(dir, "config.toml")
	if err := writeCodexConfigPath(codexPath, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("codex write: %v", err)
	}
	codex, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codex), "User Apps") {
		t.Fatalf("codex output did not preserve wrapper path with space:\n%s", string(codex))
	}

	// Grok Build TUI: TOML %q on the command must round-trip a space-containing
	// wrapper path verbatim (same %q-into-TOML invariant as Codex, without the
	// inline shell-launch line).
	grokBuildPath := filepath.Join(dir, "grok-config.toml")
	if err := writeGrokBuildConfigPath(grokBuildPath, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("grok-build write: %v", err)
	}
	grokBuild, err := os.ReadFile(grokBuildPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(grokBuild), "User Apps") {
		t.Fatalf("grok-build output did not preserve wrapper path with space:\n%s", string(grokBuild))
	}
	if !strings.Contains(string(grokBuild), `command = "`+wrapper+`"`) {
		t.Fatalf("grok-build command should be the verbatim wrapper path:\n%s", string(grokBuild))
	}
}

func TestSetupKimiFlagWritesUserScopeFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--kimi"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --kimi: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "kimi: configured") {
		t.Fatalf("expected kimi configured log, got:\n%s", out.String())
	}
	path := filepath.Join(home, ".kimi", "mcp.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("kimi setup did not create %s: %v", path, err)
	}
	root := readJSONRoot(t, path)
	servers := readRawObject(t, root["mcpServers"])
	if _, ok := servers["nole"]; !ok {
		t.Fatalf("kimi setup did not register nole entry: %#v", servers)
	}
}

func TestSetupOpenCodeFlagWritesNativeConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--opencode"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --opencode: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "opencode: configured") {
		t.Fatalf("expected opencode configured log, got:\n%s", out.String())
	}
	nativePath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if _, err := os.Stat(nativePath); err != nil {
		t.Fatalf("opencode setup did not create %s: %v", nativePath, err)
	}
	// The previous writer wrote to ~/opencode.json. That stale path must stay
	// untouched.
	stalePath := filepath.Join(home, "opencode.json")
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("opencode setup must not write %s, got err=%v", stalePath, err)
	}
}

func TestWriteHermesConfigPreservesExistingYAMLAndRegistersNole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte("model:\n  provider: openrouter\n  default: test-model\nmcp_servers:\n  other:\n    command: /usr/bin/other\n    args:\n      - serve\n")
	if err := os.WriteFile(path, existing, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	wrapper := "/usr/local/bin/nole-mcp"
	if err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole", Wrapper: wrapper}); err != nil {
		t.Fatalf("write hermes config: %v", err)
	}
	assertFileMode(t, path, 0640)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
	}
	model, ok := root["model"].(map[string]any)
	if !ok || model["provider"] != "openrouter" || model["default"] != "test-model" {
		t.Fatalf("existing model config not preserved: %#v", root["model"])
	}
	servers, ok := root["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers missing or wrong type: %#v", root["mcp_servers"])
	}
	if _, ok := servers["other"]; !ok {
		t.Fatalf("existing mcp server lost: %#v", servers)
	}
	nole, ok := servers["nole"].(map[string]any)
	if !ok {
		t.Fatalf("nole mcp server missing: %#v", servers)
	}
	if got := nole["command"]; got != wrapper {
		t.Fatalf("nole command = %#v, want %q", got, wrapper)
	}
	if args, ok := nole["args"].([]any); !ok || len(args) != 0 {
		t.Fatalf("wrapper mode should write empty args, got %#v", nole["args"])
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteHermesConfigPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte("# user profile\nmodel:\n  # keep provider note\n  provider: openrouter\n  default: test-model\n\n# servers managed by hermes\nmcp_servers:\n  # existing tool server\n  other:\n    command: /usr/bin/other\n    args:\n      - serve\n")
	if err := os.WriteFile(path, existing, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write hermes config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		"# user profile",
		"# keep provider note",
		"# servers managed by hermes",
		"# existing tool server",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Hermes config writer should preserve comment %q, got:\n%s", want, text)
		}
	}
}

func TestWriteHermesConfigRejectsNonMappingMCPServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte("model:\n  provider: openrouter\nmcp_servers: disabled\n")
	if err := os.WriteFile(path, existing, 0640); err != nil {
		t.Fatal(err)
	}
	err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"})
	if err == nil {
		t.Fatal("expected error when existing mcp_servers is not a mapping")
	}
	if !strings.Contains(err.Error(), "mcp_servers") {
		t.Fatalf("expected mcp_servers error, got %q", err.Error())
	}
}

func TestWriteHermesConfigTreatsNullRootAsEmptyMapping(t *testing.T) {
	for _, existing := range []string{"null\n", "~\n"} {
		t.Run(strings.TrimSpace(existing), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(existing), 0640); err != nil {
				t.Fatal(err)
			}
			if err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
				t.Fatalf("write hermes config: %v", err)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			root := map[string]any{}
			if err := yaml.Unmarshal(b, &root); err != nil {
				t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
			}
			servers, ok := root["mcp_servers"].(map[string]any)
			if !ok {
				t.Fatalf("mcp_servers missing or wrong type: %#v", root["mcp_servers"])
			}
			if _, ok := servers["nole"].(map[string]any); !ok {
				t.Fatalf("nole server missing after null-root setup: %#v", servers)
			}
		})
	}
}

func TestWriteHermesConfigPreservesExistingNolePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte("mcp_servers:\n  nole:\n    command: /old/nole\n    args:\n      - mcp\n    timeout: 240\n    connect_timeout: 15\n    tools:\n      resources: false\n      prompts: false\n    supports_parallel_tool_calls: true\n")
	if err := os.WriteFile(path, existing, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write hermes config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
	}
	servers := root["mcp_servers"].(map[string]any)
	nole := servers["nole"].(map[string]any)
	if nole["supports_parallel_tool_calls"] != true {
		t.Fatalf("existing supports_parallel_tool_calls lost: %#v", nole)
	}
	tools, ok := nole["tools"].(map[string]any)
	if !ok || tools["resources"] != false || tools["prompts"] != false {
		t.Fatalf("existing tools policy lost: %#v", nole["tools"])
	}
	if nole["command"] != "/usr/local/bin/nole" {
		t.Fatalf("nole command = %#v, want updated binary", nole["command"])
	}
	if nole["timeout"] != 240 || nole["connect_timeout"] != 15 {
		t.Fatalf("existing timeout settings should be preserved, got timeout=%#v connect_timeout=%#v", nole["timeout"], nole["connect_timeout"])
	}
}

func TestWriteHermesConfigAddsDefaultTimeoutsAndToolPolicyForNewNoleServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte("mcp_servers:\n  other:\n    command: /usr/bin/other\n")
	if err := os.WriteFile(path, existing, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeHermesConfigPath(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write hermes config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
	}
	servers := root["mcp_servers"].(map[string]any)
	nole := servers["nole"].(map[string]any)
	if nole["timeout"] != 120 || nole["connect_timeout"] != 60 {
		t.Fatalf("new nole server should get default timeouts, got timeout=%#v connect_timeout=%#v", nole["timeout"], nole["connect_timeout"])
	}
	tools, ok := nole["tools"].(map[string]any)
	if !ok {
		t.Fatalf("new nole server should get explicit Hermes MCP tool policy, got %#v", nole["tools"])
	}
	if tools["resources"] != false || tools["prompts"] != false {
		t.Fatalf("new nole server should disable MCP resource/prompt utility tools, got %#v", tools)
	}
}

func TestSetupHermesFlagWritesUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wrapper := filepath.Join(home, "bin", "nole-mcp")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--hermes", "--mcp-wrapper", wrapper})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --hermes: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "hermes: configured") {
		t.Fatalf("expected hermes configured log, got:\n%s", out.String())
	}
	path := filepath.Join(home, ".hermes", "config.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hermes setup did not create %s: %v", path, err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
	}
	servers := root["mcp_servers"].(map[string]any)
	nole := servers["nole"].(map[string]any)
	if nole["command"] != wrapper {
		t.Fatalf("nole command = %#v, want %q", nole["command"], wrapper)
	}
}
