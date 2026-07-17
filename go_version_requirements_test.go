package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoVersionFixture(t *testing.T, readmeRequirement string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                        "module example.test/nole\n\ngo 1.25.12\n",
		"README.md":                     "- " + readmeRequirement + " for building from source (matches the `go 1.25.12` directive in `go.mod`).\n\nElsewhere: Go 1.25.12+ (matches the `go 1.25.12` directive in `go.mod`).\n",
		"AGENTS.md":                     "Install Go 1.25.12+ (matches the `go 1.25.12` directive in `go.mod`).\n",
		"CONTRIBUTING.md":               "- Go 1.25.12+ (the project pins `go 1.25.12` in `go.mod`).\n",
		"docs/AGENT-INSTALL.md":         "Install Go 1.25.12+ or use a user-local Go toolchain.\n",
		".github/workflows/ci.yml":      "steps:\n  - uses: actions/setup-go@v6\n    with:\n      go-version: '1.25.12'\n",
		".github/workflows/release.yml": "steps:\n  - uses: actions/setup-go@v6\n    with:\n      go-version-file: 'go.mod'\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runGoVersionRequirements(t *testing.T, root string) (string, error) {
	t.Helper()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "check-go-version-requirements.sh"), root)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGoVersionRequirementsGuardAcceptsCompatibleRequirements(t *testing.T) {
	root := writeGoVersionFixture(t, "Go 1.25.12+")
	if out, err := runGoVersionRequirements(t, root); err != nil {
		t.Fatalf("guard rejected compatible requirements: %v\n%s", err, out)
	}
}

func TestGoVersionRequirementsGuardRejectsStaleReadmeMinimum(t *testing.T) {
	root := writeGoVersionFixture(t, "Go 1.25+")
	out, err := runGoVersionRequirements(t, root)
	if err == nil {
		t.Fatalf("guard accepted a stale README minimum:\n%s", out)
	}
	if !strings.Contains(out, "README.md must document a Go 1.25.12+ minimum") {
		t.Fatalf("guard failed for the wrong reason:\n%s", out)
	}
}

func TestGoVersionRequirementsGuardRejectsStaleAgentInstallMinimum(t *testing.T) {
	root := writeGoVersionFixture(t, "Go 1.25.12+")
	path := filepath.Join(root, "docs", "AGENT-INSTALL.md")
	if err := os.WriteFile(path, []byte("Install Go 1.25+ or use a user-local Go toolchain.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runGoVersionRequirements(t, root)
	if err == nil {
		t.Fatalf("guard accepted a stale agent install minimum:\n%s", out)
	}
	if !strings.Contains(out, "docs/AGENT-INSTALL.md must document a Go 1.25.12+ install minimum") {
		t.Fatalf("guard failed for the wrong reason:\n%s", out)
	}
}
