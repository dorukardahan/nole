package providerhttp

import (
	"sync"
	"testing"
	"time"
)

func TestBreakerStateReaders(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute, now: clock})

	if b.State() != "closed" || b.ConsecFails() != 0 || !b.OpenedAt().IsZero() {
		t.Fatalf("fresh breaker: state=%q consec=%d openedAt=%v", b.State(), b.ConsecFails(), b.OpenedAt())
	}

	failCall(b)
	if b.State() != "closed" || b.ConsecFails() != 1 {
		t.Fatalf("after 1 fail: state=%q consec=%d, want closed/1", b.State(), b.ConsecFails())
	}

	failCall(b) // hits threshold 2 → open
	if b.State() != "open" {
		t.Fatalf("after threshold fails: state=%q, want open", b.State())
	}
	if b.OpenedAt().IsZero() {
		t.Fatal("OpenedAt should be set once the breaker opens")
	}
	if !b.IsOpen() {
		t.Fatal("IsOpen should be true within cooldown")
	}

	// Past cooldown: raw State stays "open" (no probe admitted yet) but IsOpen()
	// flips to false (probe-eligible). This divergence is intended and is why
	// /health uses IsOpen-derived Available, not the raw State string.
	now = now.Add(2 * time.Minute)
	if b.State() != "open" {
		t.Fatalf("raw State past cooldown = %q, want open (no probe yet)", b.State())
	}
	if b.IsOpen() {
		t.Fatal("IsOpen past cooldown should be false (probe-eligible)")
	}

	if !allowed(b) {
		t.Fatal("a probe should be admitted past cooldown")
	}
	if b.State() != "half-open" {
		t.Fatalf("after probe admitted: state=%q, want half-open", b.State())
	}
}

func TestBreakerReadersNilSafe(t *testing.T) {
	var b *Breaker
	if b.State() != "closed" || b.ConsecFails() != 0 || !b.OpenedAt().IsZero() {
		t.Fatal("nil breaker readers should be closed/0/zero-time")
	}
	state, consec, openedAt := BreakerStatusFields(nil)
	if state != "" || consec != 0 || openedAt != "" {
		t.Fatalf("BreakerStatusFields(nil) = (%q,%d,%q), want empties", state, consec, openedAt)
	}
}

func TestBreakerStatusFieldsClosedOmitsOpenedAt(t *testing.T) {
	b := NewBreaker(DefaultBreakerOptions())
	state, consec, openedAt := BreakerStatusFields(b)
	if state != "closed" || consec != 0 || openedAt != "" {
		t.Fatalf("closed breaker fields = (%q,%d,%q), want (closed,0,'')", state, consec, openedAt)
	}
}

func TestBreakerStatusFieldsOpenIncludesOpenedAt(t *testing.T) {
	now := time.Unix(1000, 0)
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: func() time.Time { return now }})
	failCall(b) // threshold 1 → open immediately
	state, _, openedAt := BreakerStatusFields(b)
	if state != "open" || openedAt == "" {
		t.Fatalf("open breaker fields = (state=%q, openedAt=%q), want open + a timestamp", state, openedAt)
	}
}

func TestBreakerReadersNoRace(t *testing.T) {
	b := NewBreaker(BreakerOptions{Threshold: 3, Cooldown: time.Millisecond})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failCall(b)
			_ = b.State()
			_ = b.ConsecFails()
			_ = b.OpenedAt()
			_, _, _ = BreakerStatusFields(b)
			_ = b.IsOpen()
		}()
	}
	wg.Wait()
}
