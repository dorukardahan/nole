package scrapling

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

func TestScraplingBoundsWaitForInheritedStdout(t *testing.T) {
	const timeout = time.Second
	for _, operation := range []string{"extract", "status"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_PROXY", "")
			p := syntheticScrapling(t, nil)
			p.timeout = timeout
			marker := os.Getenv("NOLE_TEST_FETCH_COUNT")
			t.Cleanup(func() { _ = os.WriteFile(marker+".done", nil, 0600) })
			module := `import os, subprocess, sys
from types import SimpleNamespace

marker = os.environ['NOLE_TEST_FETCH_COUNT']
subprocess.Popen([sys.executable, '-c', """
import os, sys, time
deadline = time.monotonic() + 5
while time.monotonic() < deadline and not os.path.exists(sys.argv[1]):
    time.sleep(0.05)
""", marker + '.done'], stdin=subprocess.DEVNULL, stdout=sys.stdout, stderr=subprocess.DEVNULL)
with open(marker, 'w'):
    pass

class Fetcher:
    @staticmethod
    def get(url, **kwargs):
        return SimpleNamespace(status=200, text='Useful fixture page.', url=url)
`
			if err := os.WriteFile(filepath.Join(os.Getenv("PYTHONPATH"), "scrapling", "fetchers.py"), []byte(module), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			start := time.Now()
			if operation == "extract" {
				if _, err := p.Extract(ctx, core.ExtractRequest{URL: pubA}); err == nil {
					t.Error("extract accepted a subprocess with undrained stdout")
				}
			} else if p.Status(ctx).Available {
				t.Error("status accepted a subprocess with undrained stdout")
			}
			if elapsed := time.Since(start); elapsed > timeout+2*time.Second+500*time.Millisecond {
				t.Errorf("inherited stdout held command for %v", elapsed)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("runtime did not start the stdout holder")
			}
		})
	}
}
