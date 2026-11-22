package scrapling

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// These tests execute the Python helper without installing Scrapling or a
// browser. Real Chromium interception is covered separately by local fixtures.
func TestBrowserRuntimeContracts(t *testing.T) {
	var python string
	for _, candidate := range []string{"python3.13", "python3.12", "python3.11", "python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			python = path
			break
		}
	}
	if python == "" {
		t.Skip("optional Python runtime is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-B", "browser_runtime_test.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser helper contracts: %v\n%s", err, output)
	}
}
