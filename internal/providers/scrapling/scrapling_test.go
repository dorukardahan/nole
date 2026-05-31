package scrapling

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
)

func TestScraplingNameAndCapabilities(t *testing.T) {
	p := New()
	if p.Name() != "scrapling" {
		t.Fatalf("expected scrapling, got %q", p.Name())
	}
	if !core.HasCapability(p.Capabilities(), core.CapabilityExtract) {
		t.Fatal("expected extract capability")
	}
	if core.HasCapability(p.Capabilities(), core.CapabilitySearch) {
		t.Fatal("scrapling should not advertise search capability")
	}
}

func TestScraplingExtractViaConfiguredPython(t *testing.T) {
	fakePython := writeFakePython(t, `#!/usr/bin/env sh
case "$2" in
  *"__version__"*) printf '1.2.3\n'; exit 0 ;;
esac
printf '{"content":" extracted content ","metadata":{"title":"Example","mode":"fetcher","ai_targeted":"true"}}\n'
`)
	p := New(WithPython(fakePython), WithTimeout(2*time.Second))
	resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: "https://example.com", Format: "markdown"})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if resp.Provider != "scrapling" || resp.Content != "extracted content" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Metadata["ai_targeted"] != "true" {
		t.Fatalf("expected ai_targeted metadata, got %#v", resp.Metadata)
	}
}

func TestScraplingExtractRequiresExplicitRuntime(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_PYTHON", "")
	_, err := New().Extract(context.Background(), core.ExtractRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected extract error without explicit runtime")
	}
	if !strings.Contains(err.Error(), "NOLE_SCRAPLING_PYTHON") {
		t.Fatalf("expected explicit runtime error, got %q", err.Error())
	}
}

func TestScraplingStatusRequiresExplicitRuntime(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_PYTHON", "")
	status := New().Status(context.Background())
	if status.Available {
		t.Fatal("expected unavailable status without explicit runtime")
	}
	if !strings.Contains(status.Reason, "NOLE_SCRAPLING_PYTHON") {
		t.Fatalf("expected explicit runtime status reason, got %q", status.Reason)
	}
}

func TestScraplingStatusUnavailableWhenPythonFails(t *testing.T) {
	fakePython := writeFakePython(t, `#!/usr/bin/env sh
printf 'ModuleNotFoundError: No module named scrapling\n' >&2
exit 1
`)
	p := New(WithPython(fakePython), WithTimeout(2*time.Second))
	status := p.Status(context.Background())
	if status.Available {
		t.Fatal("expected unavailable status")
	}
	if !strings.Contains(status.Reason, "scrapling") {
		t.Fatalf("expected scrapling reason, got %q", status.Reason)
	}
	if !strings.Contains(status.Reason, `pip install "scrapling[fetchers]"`) {
		t.Fatalf("expected actionable install hint, got %q", status.Reason)
	}
}

func TestScraplingStatusRequiresFetcherExtra(t *testing.T) {
	fakePython := writeFakePython(t, `#!/usr/bin/env sh
case "$2" in
  *"scrapling.fetchers"*) printf 'ModuleNotFoundError: No module named scrapling.fetchers\n' >&2; exit 1 ;;
  *"__version__"*) printf '1.2.3\n'; exit 0 ;;
esac
exit 0
`)
	p := New(WithPython(fakePython), WithTimeout(2*time.Second))
	status := p.Status(context.Background())
	if status.Available {
		t.Fatal("expected unavailable status when fetcher extra is missing")
	}
	if !strings.Contains(status.Reason, `pip install "scrapling[fetchers]"`) {
		t.Fatalf("expected fetchers install hint, got %q", status.Reason)
	}
}

func TestScraplingSearchUnsupported(t *testing.T) {
	_, err := New().Search(context.Background(), core.SearchRequest{Query: "x"})
	if err == nil {
		t.Fatal("expected unsupported search error")
	}
}

func TestCappedBufferUnderCapPreservesBytes(t *testing.T) {
	c := newCappedBuffer(16)
	n, err := c.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error under cap: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes written, got %d", n)
	}
	if _, err := c.Write([]byte(" world")); err != nil {
		t.Fatalf("unexpected error under cap: %v", err)
	}
	if c.Truncated() {
		t.Fatal("did not expect truncation under cap")
	}
	if got := c.String(); got != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", got)
	}
	if got := string(c.Bytes()); got != "hello world" {
		t.Fatalf("Bytes mismatch: got %q", got)
	}
}

func TestCappedBufferStopsAtCapAndFlagsTruncation(t *testing.T) {
	c := newCappedBuffer(8)
	// Fill exactly to the cap; this must still succeed.
	if _, err := c.Write([]byte("12345678")); err != nil {
		t.Fatalf("unexpected error at exact cap: %v", err)
	}
	if c.Truncated() {
		t.Fatal("did not expect truncation when exactly at cap")
	}
	// The next byte overflows: it keeps what fits (nothing here) and flags.
	n, err := c.Write([]byte("9"))
	if err == nil {
		t.Fatal("expected error once cap exceeded")
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes buffered past cap, got %d", n)
	}
	if !c.Truncated() {
		t.Fatal("expected truncation flag after overflow")
	}
	if c.buf.Len() != 8 {
		t.Fatalf("buffer grew past cap: len=%d", c.buf.Len())
	}
	// Subsequent writes are rejected without buffering anything more.
	if n, err := c.Write([]byte("more")); err == nil || n != 0 {
		t.Fatalf("expected rejected write after truncation, got n=%d err=%v", n, err)
	}
	if c.buf.Len() != 8 {
		t.Fatalf("buffer grew after truncation: len=%d", c.buf.Len())
	}
}

func TestCappedBufferPartialFillThenOverflow(t *testing.T) {
	c := newCappedBuffer(8)
	// A single write larger than the cap keeps exactly the remaining capacity.
	n, err := c.Write([]byte("0123456789"))
	if err == nil {
		t.Fatal("expected error when single write exceeds cap")
	}
	if n != 8 {
		t.Fatalf("expected 8 bytes retained, got %d", n)
	}
	if !c.Truncated() {
		t.Fatal("expected truncation flag")
	}
	if got := c.String(); got != "01234567" {
		t.Fatalf("expected first 8 bytes %q, got %q", "01234567", got)
	}
}

func writeFakePython(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake is unix-only")
	}
	path := filepath.Join(t.TempDir(), "fake-python")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
