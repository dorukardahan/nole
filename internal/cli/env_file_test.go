package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNoleEnvFileParsesShellAssignmentsWithoutOverriding(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	home := t.TempDir()
	t.Setenv("HOME", home)
	content := "# comment\n" +
		"NOLE_SCRAPLING_PYTHON='/tmp/py'\"'\"'thon'\n" +
		"export TAVILY_API_KEY=\"from-file\"\n" +
		"BRAVE_API_KEY=from-file # trailing comment\n" +
		"NOLE_QUOTA_LEDGER_PATH=\"$HOME/.local/state/nole/quota-ledger.json\"\n" +
		"NOLE_CACHE_TTL=\"\\$HOME-literal\"\n" +
		"INVALID-KEY=ignored\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	unsetEnvForTest(t, "NOLE_SCRAPLING_PYTHON")
	unsetEnvForTest(t, "BRAVE_API_KEY")
	unsetEnvForTest(t, "NOLE_QUOTA_LEDGER_PATH")
	unsetEnvForTest(t, "NOLE_CACHE_TTL")
	t.Setenv("TAVILY_API_KEY", "from-shell")

	if err := loadNoleEnvFile(path); err != nil {
		t.Fatalf("load env file: %v", err)
	}
	if got := os.Getenv("NOLE_SCRAPLING_PYTHON"); got != "/tmp/py'thon" {
		t.Fatalf("NOLE_SCRAPLING_PYTHON = %q", got)
	}
	if got := os.Getenv("TAVILY_API_KEY"); got != "from-shell" {
		t.Fatalf("TAVILY_API_KEY should not be overridden, got %q", got)
	}
	if got := os.Getenv("BRAVE_API_KEY"); got != "from-file" {
		t.Fatalf("BRAVE_API_KEY = %q", got)
	}
	if got, want := os.Getenv("NOLE_QUOTA_LEDGER_PATH"), filepath.Join(home, ".local", "state", "nole", "quota-ledger.json"); got != want {
		t.Fatalf("NOLE_QUOTA_LEDGER_PATH = %q, want %q", got, want)
	}
	if got := os.Getenv("NOLE_CACHE_TTL"); got != "$HOME-literal" {
		t.Fatalf("escaped dollar should not expand, got %q", got)
	}
	if got := os.Getenv("INVALID-KEY"); got != "" {
		t.Fatalf("invalid key should not be loaded, got %q", got)
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
