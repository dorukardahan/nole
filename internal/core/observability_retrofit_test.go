package core

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/nolelog"
)

// A non-fatal research search-step failure must be routed through the injected
// nolelog.Logger (os.Stderr in production) as a structured, secret-safe event —
// never to stdout. This locks the core retrofit (research.go) plus the
// WithLogger wiring that replaced the old raw fmt.Fprintf(os.Stderr,...).
func TestResearchStepFailureLogsThroughInjectedLogger(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"brave", "tavily", "firecrawl", "ddgs"} {
		_ = registry.Register(failingProvider{fakeProvider{name: name}})
	}
	ledger := NewMemoryQuotaLedger()
	for _, name := range []string{"brave", "tavily", "firecrawl", "ddgs"} {
		ledger.Set(QuotaEntry{Provider: name, FreeRemaining: 5})
	}

	var buf bytes.Buffer
	svc := NewService(registry, ledger, DefaultRouteMatrix(), WithLogger(nolelog.New(&buf, nolelog.ModeJSON)))

	report, err := svc.Research(context.Background(), "what is the latest go release", 2)
	if err != nil {
		t.Fatalf("research must not hard-fail on per-step provider errors: %v", err)
	}
	if report == nil {
		t.Fatal("expected a (possibly source-less) report, got nil")
	}

	out := buf.String()
	if !strings.Contains(out, "research.search_step_failed") {
		t.Fatalf("expected a structured search-step-failure event, got %q", out)
	}
	if !strings.Contains(out, `"level":"warn"`) {
		t.Fatalf("expected warn level in the structured event, got %q", out)
	}
	// Every emitted line must be one complete JSON object (the gateway never
	// emits a half line that could be mistaken for protocol output).
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Fatalf("non-object log line emitted: %q", line)
		}
	}
}

// A Service built WITHOUT WithLogger must tolerate a research step failure
// without panicking — the nil *nolelog.Logger is a safe no-op.
func TestResearchStepFailureWithNilLoggerDoesNotPanic(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(failingProvider{fakeProvider{name: "ddgs"}})
	ledger := NewMemoryQuotaLedger()
	ledger.Set(QuotaEntry{Provider: "ddgs", FreeRemaining: 5})
	svc := NewService(registry, ledger, DefaultRouteMatrix())
	if _, err := svc.Research(context.Background(), "hello world", 1); err != nil {
		t.Fatalf("research should tolerate provider failure with no logger: %v", err)
	}
}
