//go:build !unix

package scrapling

import (
	"context"
	"strings"
	"testing"
)

func TestUnsupportedBrowserPlatformReportsUnavailable(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_MODE", "stealthy")
	p := New(WithPython("unused-python"))
	for _, deep := range []bool{false, true} {
		status := p.status(context.Background(), deep)
		if status.Available || !strings.Contains(status.Reason, "unsupported on this platform") {
			t.Fatal("unsupported browser reported available")
		}
	}
}
