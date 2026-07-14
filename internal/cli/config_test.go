package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestSecretEnvStatusesReportsProviderKeyPresence(t *testing.T) {
	unsetProviderKeyEnvForTest(t)

	tests := []struct {
		env      string
		provider string
	}{
		{env: "FIRECRAWL_API_KEY", provider: "firecrawl"},
		{env: "BRAVE_API_KEY", provider: "brave"},
		{env: "BRAVE_SEARCH_API_KEY", provider: "brave (alt)"},
		{env: "TAVILY_API_KEY", provider: "tavily"},
	}
	expectedProviders := make(map[string]string, len(tests))
	for _, tt := range tests {
		if _, exists := expectedProviders[tt.env]; exists {
			t.Fatalf("duplicate environment key in test table: %s", tt.env)
		}
		expectedProviders[tt.env] = tt.provider
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv(tt.env, "test-only-placeholder")

			statuses := secretEnvStatuses()
			if len(statuses) != len(tests) {
				t.Errorf("got %d provider key statuses, want %d", len(statuses), len(tests))
			}

			seen := make(map[string]int, len(statuses))
			for _, status := range statuses {
				seen[status.Env]++
				wantProvider, ok := expectedProviders[status.Env]
				if !ok {
					t.Errorf("unexpected provider key status for %s", status.Env)
					continue
				}
				if status.Provider != wantProvider {
					t.Errorf("%s provider = %q, want %q", status.Env, status.Provider, wantProvider)
				}
				wantSet := status.Env == tt.env
				if status.Set != wantSet {
					t.Errorf("%s set = %t, want %t", status.Env, status.Set, wantSet)
				}
			}
			for env := range expectedProviders {
				if seen[env] != 1 {
					t.Errorf("%s status count = %d, want 1", env, seen[env])
				}
			}
		})
	}
}

func TestBuildConfigDumpLoadsEnvFileAndMergesProcessEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOLE_DISABLE_ENV_FILE", "")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	t.Setenv("NOLE_COST_POLICY", "free-first")
	t.Setenv("NOLE_BRAVE_PAID", "")
	t.Setenv("NOLE_TAVILY_PAID", "")
	unsetProviderKeyEnvForTest(t)
	unsetEnvForTest(t, "NOLE_LOG")

	const processProviderValue = "example-process-brave-key"
	t.Setenv("BRAVE_API_KEY", processProviderValue)

	envDir := filepath.Join(home, ".config", "nole")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	envFile := []byte(strings.Join([]string{
		"NOLE_LOG=json",
		"BRAVE_API_KEY=" + "example-file-brave-key",
		"TAVILY_API_KEY=" + "example-file-tavily-key",
		"",
	}, "\n"))
	if err := os.WriteFile(filepath.Join(envDir, ".env"), envFile, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	dump := buildConfigDump(context.Background())
	if !dump.EnvFileLoaded {
		t.Fatal("expected config dump to report the env file as loaded")
	}
	if dump.LogMode != "json" {
		t.Fatalf("log mode = %q, want json from env file", dump.LogMode)
	}
	if os.Getenv("BRAVE_API_KEY") != processProviderValue {
		t.Fatal("env-file merge overrode an existing process environment value")
	}

	keysByEnv := make(map[string]secretStatus, len(dump.ProviderKeys))
	for _, status := range dump.ProviderKeys {
		keysByEnv[status.Env] = status
	}
	for _, env := range []string{"BRAVE_API_KEY", "TAVILY_API_KEY"} {
		if !keysByEnv[env].Set {
			t.Errorf("expected %s to be reported as set after config loading", env)
		}
	}
	for _, env := range []string{"BRAVE_SEARCH_API_KEY", "FIRECRAWL_API_KEY"} {
		if keysByEnv[env].Set {
			t.Errorf("expected %s to remain unset", env)
		}
	}

	providersByName := make(map[string]providerCostClassStatus, len(dump.Providers))
	for _, status := range dump.Providers {
		providersByName[status.Name] = status
	}
	for _, name := range []string{"brave", "tavily"} {
		status, ok := providersByName[name]
		if !ok {
			t.Errorf("expected provider status for %s", name)
			continue
		}
		if status.CostClass != string(core.CostClassFreeTierBYOK) || !status.AllowedByPolicy {
			t.Errorf("unexpected effective status for %s: cost_class=%q allowed_by_policy=%t", name, status.CostClass, status.AllowedByPolicy)
		}
	}
}
