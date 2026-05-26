package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUpsertShellEnvAssignmentUpdatesScraplingPath(t *testing.T) {
	existing := "# keep user comments\nFOO=bar\nexport NOLE_SCRAPLING_PYTHON=/old/python\nOTHER=value\n"
	got := upsertShellEnvAssignment(existing, "NOLE_SCRAPLING_PYTHON", "/new path/py'thon")

	if strings.Count(got, "NOLE_SCRAPLING_PYTHON=") != 1 {
		t.Fatalf("expected one Scrapling assignment, got:\n%s", got)
	}
	for _, want := range []string{
		"# keep user comments",
		"FOO=bar",
		"OTHER=value",
		"NOLE_SCRAPLING_PYTHON='/new path/py'\"'\"'thon'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in env file, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/old/python") {
		t.Fatalf("old Scrapling assignment was not replaced:\n%s", got)
	}
}

func TestWriteMCPWrapperSourcesNoleEnvAndUsesBinaryFallback(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "bin", "nole-mcp")
	binary := filepath.Join(dir, "User Apps", "nole")

	if err := writeMCPWrapper(wrapper, binary); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	b, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		`"$HOME/.config/nole/.env"`,
		"exec \"$NOLE_BIN\" mcp",
		"NOLE_BIN_DEFAULT='" + binary + "'",
		"exec nole mcp",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected wrapper to contain %q, got:\n%s", want, text)
		}
	}
	assertFileMode(t, wrapper, 0700)
}

func TestSetupLocalExtractFlagWritesEnvAndWrapperWithoutAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python shell script is Unix-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	fakePython := writeFakeSetupPython(t, home)
	venv := filepath.Join(home, "runtime", "scrapling")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--local-extract", "--python", fakePython, "--local-extract-venv", venv})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --local-extract: %v\n%s", err, out.String())
	}

	envPath := filepath.Join(home, ".config", "nole", ".env")
	env, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected env file: %v", err)
	}
	venvPython := filepath.Join(venv, "bin", "python")
	if !strings.Contains(string(env), "NOLE_SCRAPLING_PYTHON='"+venvPython+"'") {
		t.Fatalf("env file missing Scrapling python path:\n%s", string(env))
	}
	assertFileMode(t, envPath, 0600)

	wrapper := filepath.Join(home, ".local", "bin", "nole-mcp")
	if _, err := os.Stat(wrapper); err != nil {
		t.Fatalf("expected MCP wrapper: %v", err)
	}
	assertFileMode(t, wrapper, 0700)

	text := out.String()
	for _, want := range []string{
		"local-extract: configured Scrapling runtime",
		"local-extract: wrote env-sourcing MCP wrapper",
		"0 agent(s) configured",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestSetupHermesWithLocalExtractRegistersGeneratedWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python shell script is Unix-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	fakePython := writeFakeSetupPython(t, home)
	venv := filepath.Join(home, "runtime", "scrapling")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"setup", "--hermes", "--local-extract", "--python", fakePython, "--local-extract-venv", venv})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --hermes --local-extract: %v\n%s", err, out.String())
	}

	path := filepath.Join(home, ".hermes", "config.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected Hermes config: %v", err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatalf("unmarshal hermes yaml: %v\n%s", err, string(b))
	}
	servers := root["mcp_servers"].(map[string]any)
	nole := servers["nole"].(map[string]any)
	wrapper := filepath.Join(home, ".local", "bin", "nole-mcp")
	if nole["command"] != wrapper {
		t.Fatalf("Hermes command = %#v, want generated wrapper %q", nole["command"], wrapper)
	}
	if args, ok := nole["args"].([]any); !ok || len(args) != 0 {
		t.Fatalf("Hermes wrapper mode should write empty args, got %#v", nole["args"])
	}
	if _, err := os.Stat(wrapper); err != nil {
		t.Fatalf("expected generated wrapper: %v", err)
	}
}

func writeFakeSetupPython(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-python")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "-c" ]; then
  exit 0
fi
if [ "${1:-}" = "-m" ] && [ "${2:-}" = "venv" ]; then
  target="$3"
  mkdir -p "$target/bin"
  cat > "$target/bin/python" <<'PY'
#!/bin/sh
set -eu
if [ "${1:-}" = "-c" ]; then
  exit 0
fi
if [ "${1:-}" = "-m" ] && [ "${2:-}" = "pip" ]; then
  exit 0
fi
echo "unexpected venv python args: $*" >&2
exit 2
PY
  chmod +x "$target/bin/python"
  exit 0
fi
echo "unexpected python args: $*" >&2
exit 2
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
