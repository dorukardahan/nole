package brave

import (
	"context"
	"testing"

	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

func TestStatusBreakerFieldsClosed(t *testing.T) {
	b := providerhttp.NewBreaker(providerhttp.DefaultBreakerOptions())
	s := New(WithAPIKey("k"), WithBreaker(b)).Status(context.Background())
	if !s.Available {
		t.Fatal("keyed provider with a closed breaker should be available")
	}
	if s.BreakerState != "closed" {
		t.Fatalf("BreakerState = %q, want closed", s.BreakerState)
	}
	if s.BreakerOpenedAt != "" {
		t.Fatalf("closed breaker should omit BreakerOpenedAt, got %q", s.BreakerOpenedAt)
	}
}

// A provider whose breaker is currently short-circuiting reports Available=false
// with Reason=circuit_open, so the route walk and /health treat it as not-ready —
// while BreakerState still exposes the raw "open" lifecycle for observability.
func TestStatusCircuitOpenMakesUnavailable(t *testing.T) {
	b := providerhttp.NewBreaker(providerhttp.DefaultBreakerOptions()) // threshold 5
	for i := 0; i < 5; i++ {
		if ok, gen := b.Allow(); ok {
			b.RecordFailure(gen)
		}
	}
	if !b.IsOpen() {
		t.Fatal("breaker should be open after threshold failures")
	}
	s := New(WithAPIKey("k"), WithBreaker(b)).Status(context.Background())
	if s.Available {
		t.Fatal("a provider whose breaker is open must report Available=false")
	}
	if s.Reason != "circuit_open" {
		t.Fatalf("Reason = %q, want circuit_open", s.Reason)
	}
	if s.BreakerState != "open" {
		t.Fatalf("BreakerState = %q, want open", s.BreakerState)
	}
	if s.BreakerOpenedAt == "" {
		t.Fatal("expected a BreakerOpenedAt timestamp while open")
	}
}

func TestStatusNoBreakerOmitsFields(t *testing.T) {
	s := New(WithAPIKey("k")).Status(context.Background())
	if !s.Available {
		t.Fatal("keyed provider without a breaker should be available")
	}
	if s.BreakerState != "" {
		t.Fatalf("unbreakered provider should omit BreakerState, got %q", s.BreakerState)
	}
}
