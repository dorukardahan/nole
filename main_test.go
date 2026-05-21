package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainPrintsCommandErrorsToStderr(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "nole")
	build := exec.Command("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build test binary: %v\n%s", err, out)
	}

	cmd := exec.Command(exe, "definitely-not-a-command")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected invalid command to exit non-zero")
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Fatalf("expected stdout to stay empty, got %q", got)
	}
	if got := strings.TrimSpace(stderr.String()); got == "" {
		t.Fatal("expected invalid command to print a diagnostic to stderr")
	}
}
