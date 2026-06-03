package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression (Codex PR #62): when NOLE_SERVE_TOKEN is set ONLY in the local env
// file (~/.config/nole/.env), serve must honor it — the security preflight and the
// auth middleware must see it. serve.go loads the env file BEFORE reading the
// token, so an env-file-only token is visible. Reading the token before the load
// (the original bug) would wrongly refuse a non-loopback bind and leave auth off.
func TestServeTokenHonorsEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory") // avoid creating a ledger file
	t.Setenv("NOLE_DISABLE_ENV_FILE", "")        // re-enable env-file loading (TestMain disables it)
	unsetEnvForTest(t, "NOLE_SERVE_TOKEN")       // the token must come only from the file

	envDir := filepath.Join(home, ".config", "nole")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Short, obviously-fake value (the real token is user-supplied at runtime).
	if err := os.WriteFile(filepath.Join(envDir, ".env"), []byte("NOLE_SERVE_TOKEN=filetok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Same order as serve.go's RunE: load the env file, THEN read the token.
	loadDefaultNoleEnvFile()
	token := strings.TrimSpace(os.Getenv("NOLE_SERVE_TOKEN"))
	if token == "" {
		t.Fatal("NOLE_SERVE_TOKEN from the env file was not honored (token read before the env file loaded?)")
	}
	// With the env-file token present, a non-loopback bind must pass the preflight
	// instead of being refused.
	if err := serveSecurityPreflight("0.0.0.0:8765", token); err != nil {
		t.Fatalf("env-file token should satisfy the non-loopback preflight, got refuse-to-start: %v", err)
	}
}
