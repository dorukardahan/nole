package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/version"
)

// The version package's Commit and Date vars were declared but never consumed
// or build-stamped (dead). A `nole version` command makes them readable at
// runtime and gives users/agents a way to query the running binary's build.
func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	origV, origC, origD := version.Version, version.Commit, version.Date
	version.Version, version.Commit, version.Date = "9.9.9-test", "abc1234", "2026-05-30T00:00:00Z"
	defer func() { version.Version, version.Commit, version.Date = origV, origC, origD }()

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"9.9.9-test", "abc1234", "2026-05-30T00:00:00Z"} {
		if !strings.Contains(text, want) {
			t.Fatalf("version output missing %q, got:\n%s", want, text)
		}
	}
}
