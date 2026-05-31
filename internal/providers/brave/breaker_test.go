package brave

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

// When a wired breaker is open, Search must short-circuit with ErrCircuitOpen
// before making any HTTP call. This is hermetic: the open breaker returns from
// DoWithRetryBreaker.Allow() before the request reaches the real Brave endpoint,
// so no network access occurs even though the provider holds its real client.
func TestBraveSearchShortCircuitsOpenBreaker(t *testing.T) {
	b := providerhttp.NewBreaker(providerhttp.BreakerOptions{Threshold: 1, Cooldown: time.Hour})
	_, gen := b.Allow()
	b.RecordFailure(gen) // trip open (threshold 1)

	p := New(WithAPIKey("test-key"), WithBreaker(b))
	_, err := p.Search(context.Background(), core.SearchRequest{Query: "anything", Task: core.TaskGeneral, Limit: 5})
	if err == nil {
		t.Fatal("expected an error when the breaker is open")
	}
	if !errors.Is(err, providerhttp.ErrCircuitOpen) {
		t.Fatalf("err = %v, want it to wrap ErrCircuitOpen", err)
	}
}
