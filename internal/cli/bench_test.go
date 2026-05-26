package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
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
		Evidence       struct {
			ArtifactKind    string   `json:"artifact_kind"`
			DataSource      string   `json:"data_source"`
			DoesNotMeasure  []string `json:"does_not_measure"`
			NetworkRequired bool     `json:"network_required"`
			SecretsRequired bool     `json:"secrets_required"`
			Sanitized       bool     `json:"sanitized"`
		} `json:"evidence"`
		Summary struct {
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
	if report.Evidence.ArtifactKind != "deterministic_fixture_eval" || report.Evidence.NetworkRequired || report.Evidence.SecretsRequired || !report.Evidence.Sanitized {
		t.Fatalf("offline JSON must include honest evidence metadata, got %#v", report.Evidence)
	}
	if !strings.Contains(strings.Join(report.Evidence.DoesNotMeasure, "\n"), "live web result quality") {
		t.Fatalf("offline JSON must say it does not measure live web quality, got %#v", report.Evidence.DoesNotMeasure)
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
	if flag := cmd.Flags().Lookup("evidence-md"); flag == nil {
		t.Fatal("bench command missing --evidence-md flag")
	}
}

func TestComprehensiveBenchProviderSetIncludesLocalScrapling(t *testing.T) {
	providers := comprehensiveBenchProviders()
	p, ok := providers["scrapling"]
	if !ok {
		t.Fatalf("comprehensive provider set must include scrapling for local extract benchmarks; got %v", sortedKeys(providers))
	}
	if !core.HasCapability(p.Capabilities(), core.CapabilityExtract) {
		t.Fatalf("scrapling comprehensive provider must advertise extract capability, got %v", p.Capabilities())
	}
}

func TestRunComprehensiveBenchLoadsDefaultEnvFileBeforeConstructingProviders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOLE_DISABLE_ENV_FILE", "")
	unsetEnvForTest(t, "BRAVE_API_KEY")
	envDir := filepath.Join(home, ".config", "nole")
	if err := os.MkdirAll(envDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, ".env"), []byte("BRAVE_API_KEY=from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loadDefaultNoleEnvFile()
	providers := comprehensiveBenchProviders()
	if st := providers["brave"].Status(context.Background()); !st.Available {
		t.Fatalf("brave should be available after loading default env file, got %#v", st)
	}
}

func TestBenchCommandEvidenceMarkdownOutputIsPublicSafe(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bench", "--evidence-md"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bench evidence markdown failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"# Route evidence summary", "Mode: offline", "Private data: none included", "does not measure live web result quality"} {
		if !strings.Contains(text, want) {
			t.Fatalf("evidence markdown missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"Authorization", "Bearer", "SECRET", "private.example", "/home/"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("evidence markdown leaked %q:\n%s", forbidden, text)
		}
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

func TestSanitizedBenchErrorUsesStructuredSafeHTTPStatus(t *testing.T) {
	err := providerhttp.NewHTTPStatusError("tavily", "search", 401, []byte(`token=SECRET Authorization: Bearer SECRET https://private.example`))
	msg := sanitizedBenchError(err)
	for _, forbidden := range []string{"SECRET", "Authorization", "private.example", "Bearer"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("bench error leaked %q in %q", forbidden, msg)
		}
	}
	for _, want := range []string{"tavily", "search", "401"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("bench error missing %q in %q", want, msg)
		}
	}
}
