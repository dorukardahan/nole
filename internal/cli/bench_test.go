package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRootCommandIncludesBenchCommand(t *testing.T) {
	cmd := NewRootCommand()
	found := false
	for _, child := range cmd.Commands() {
		if child.Name() == "bench" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected root command to include bench")
	}
}

func TestBenchCommandRunsOfflineJSONByDefault(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bench", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bench command failed: %v", err)
	}
	var report struct {
		Mode           string `json:"mode"`
		FixtureVersion string `json:"fixture_version"`
		Summary        struct {
			TotalCases int `json:"total_cases"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("bench output is not JSON: %v\n%s", err, out.String())
	}
	if report.Mode != "offline" {
		t.Fatalf("mode = %q, want offline", report.Mode)
	}
	if report.FixtureVersion == "" || report.Summary.TotalCases == 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestBenchCommandLiveModeRequiresExplicitFlagAndLowLimit(t *testing.T) {
	cmd := newBenchCommand()
	if flag := cmd.Flags().Lookup("live"); flag == nil {
		t.Fatal("bench command missing --live flag")
	}
	if flag := cmd.Flags().Lookup("max-live-cases"); flag == nil {
		t.Fatal("bench command missing --max-live-cases flag")
	} else if flag.DefValue != "3" {
		t.Fatalf("max live cases default = %q, want 3", flag.DefValue)
	}
}

func TestBenchCommandTextOutputMentionsOfflineAndFixtureVersion(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bench"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bench command failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "offline") || !strings.Contains(text, "fixture") {
		t.Fatalf("text output should mention offline fixture run, got: %s", text)
	}
}
