//go:build unix

package scrapling

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserProcessGroupCleanup(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "parent_exit", true: "deadline"}[timeout], func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "orphan-ran")
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			// A descendant surviving the parent would create the marker. Positional
			// arguments keep the temporary path out of shell source.
			script := `(sleep 0.5; touch "$1") &`
			if timeout {
				script += "\nwait"
			}
			cmd := exec.CommandContext(ctx, "sh", "-c", script, "fixture", marker)
			err := runBrowserCommand(cmd)
			if timeout && err == nil || !timeout && err != nil {
				t.Fatalf("unexpected process result: %v", err)
			}
			time.Sleep(600 * time.Millisecond)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("browser descendant survived cleanup: %v", err)
			}
		})
	}
}
