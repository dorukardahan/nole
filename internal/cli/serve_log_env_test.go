package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dorukardahan/nole/internal/nolelog"
)

// Regression (Codex PR #41): when NOLE_LOG is set ONLY in the local env file
// (~/.config/nole/.env), the serve handler's diagnostic logger must still honor
// it. serve.go builds defaultService() (which loads the env file) BEFORE
// constructing the logger, so nolelog.FromEnv sees the env-file value rather than
// the process-env default. (The non-loopback security warning is intentionally
// NOT governed by NOLE_LOG — it is a raw, unconditional stderr notice.)
// This test mirrors serve's order and asserts the resolved mode.
func TestServeLoggerHonorsEnvFileNoleLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory") // avoid creating a ledger file
	t.Setenv("NOLE_DISABLE_ENV_FILE", "")        // re-enable env-file loading (TestMain disables it)
	unsetEnvForTest(t, "NOLE_LOG")               // NOLE_LOG must come only from the file

	envDir := filepath.Join(home, ".config", "nole")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, ".env"), []byte("NOLE_LOG=off\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Same order as serve.go's RunE: service first (loads the env file), then the
	// logger reads NOLE_LOG.
	_ = defaultService()
	if mode := nolelog.ParseMode(os.Getenv("NOLE_LOG")); mode != nolelog.ModeOff {
		t.Fatalf("serve logger would not honor env-file NOLE_LOG=off; resolved mode=%q (env file not loaded before logger?)", mode)
	}
}
