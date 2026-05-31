package providerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func statusResponse(code int) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}
}

// failCall / succeedCall simulate one full breaker-gated call (admit via Allow,
// then record with the admitted generation), mirroring DoWithRetryBreaker. They
// no-op if Allow denies (open within cooldown / a probe already in flight).
func failCall(b *Breaker) {
	if ok, gen := b.Allow(); ok {
		b.RecordFailure(gen)
	}
}

func succeedCall(b *Breaker) {
	if ok, gen := b.Allow(); ok {
		b.RecordSuccess(gen)
	}
}

func allowed(b *Breaker) bool {
	ok, _ := b.Allow()
	return ok
}

func TestBreakerTripsAfterNConsecutiveFailures(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 3, Cooldown: time.Minute, now: func() time.Time { return now }})

	if !allowed(b) {
		t.Fatal("a fresh (closed) breaker must allow")
	}
	failCall(b)
	failCall(b)
	if !allowed(b) {
		t.Fatal("breaker must stay closed below the threshold")
	}
	failCall(b) // 3rd consecutive → trip
	if allowed(b) {
		t.Fatal("breaker must be open once the threshold is reached")
	}
	if !b.IsOpen() {
		t.Fatal("IsOpen() must report the open breaker")
	}

	// A success resets the counter even after partial accumulation.
	b2 := NewBreaker(BreakerOptions{Threshold: 3, Cooldown: time.Minute, now: func() time.Time { return now }})
	failCall(b2)
	failCall(b2)
	succeedCall(b2)
	failCall(b2)
	failCall(b2)
	if !allowed(b2) {
		t.Fatal("a success must reset the consecutive-failure counter")
	}
}

func TestBreakerOpenShortCircuitsWithoutCallingDownstream(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: func() time.Time { return now }})
	failCall(b) // threshold 1 → open immediately

	var calls int
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return okResponse(), nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/", nil)

	resp, err := DoWithRetryBreaker(context.Background(), client, req, RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, Sleep: noSleep}, b)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if resp != nil {
		t.Fatal("an open breaker must not return a response")
	}
	if calls != 0 {
		t.Fatalf("downstream was called %d times, want 0 (short-circuit)", calls)
	}
}

func TestBreakerHalfOpenAllowsExactlyOneProbe(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	failCall(b) // open at t=0

	if allowed(b) {
		t.Fatal("within cooldown, open breaker must deny")
	}
	now = now.Add(2 * time.Minute) // past cooldown
	ok, _ := b.Allow()
	if !ok {
		t.Fatal("after cooldown, the first Allow must permit a single probe")
	}
	if allowed(b) {
		t.Fatal("a second Allow during the in-flight probe must be denied")
	}
}

func TestBreakerHalfOpenOutcomeClosesOrReopens(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }

	// Probe success closes the breaker.
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	failCall(b)
	now = now.Add(2 * time.Minute)
	succeedCall(b) // half-open probe succeeds → closed
	if !allowed(b) {
		t.Fatal("a successful probe must close the breaker")
	}

	// Probe failure re-opens the breaker and restarts the cooldown.
	now = time.Unix(0, 0)
	b2 := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	failCall(b2)
	now = now.Add(2 * time.Minute)
	failCall(b2) // half-open probe fails → re-open
	if allowed(b2) {
		t.Fatal("a failed probe must re-open the breaker for a fresh cooldown")
	}
}

// A success admitted while the breaker was CLOSED can return LATE, after
// concurrent failures already tripped it OPEN (the lock-free I/O window). That
// stale success carries an old generation and must be ignored — it must not
// re-close the breaker.
func TestBreakerStaleSuccessDoesNotReopenTrippedBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute, now: func() time.Time { return now }})

	_, genA := b.Allow() // call A admitted while closed (gen 0)
	failCall(b)          // two concurrent failures trip the breaker (new generation)
	failCall(b)
	if allowed(b) {
		t.Fatal("breaker should be open after reaching the threshold")
	}
	b.RecordSuccess(genA) // A finishes success with its stale generation
	if allowed(b) {
		t.Fatal("a stale success must not re-close a breaker that just tripped open")
	}
	if !b.IsOpen() {
		t.Fatal("breaker must remain open after a stale success")
	}
}

// The P2 case: a call admitted while CLOSED finishes successfully AFTER the
// breaker has opened, the cooldown elapsed, and a half-open probe is in flight.
// The stale success (old generation) must NOT be mistaken for the probe and
// close the breaker — only the actual probe's outcome governs recovery.
func TestBreakerStaleSuccessDuringHalfOpenIsIgnored(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})

	_, genStale := b.Allow() // call A admitted while closed (gen 0)
	failCall(b)              // trip → open at t=0 (new generation)

	now = now.Add(2 * time.Minute) // cooldown elapses
	okProbe, genProbe := b.Allow() // probe P admitted → half-open (newer generation)
	if !okProbe {
		t.Fatal("a probe should be admitted after the cooldown")
	}

	b.RecordSuccess(genStale) // stale success lands during half-open — must be ignored
	if allowed(b) {
		t.Fatal("a stale success during half-open must not close the breaker; the probe is still in flight")
	}

	b.RecordFailure(genProbe) // the real probe fails → breaker re-opens
	if !b.IsOpen() {
		t.Fatal("the actual probe outcome must govern: a failed probe re-opens the breaker")
	}
}

// A failure admitted while OPEN/CLOSED that lands after a transition carries a
// stale generation and must not bump openedAt / extend the cooldown.
func TestBreakerStaleFailureDoesNotExtendCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})

	_, genStale := b.Allow() // admitted while closed (gen 0)
	failCall(b)              // trip → open at t=0 (generation bumped)

	now = now.Add(30 * time.Second)
	b.RecordFailure(genStale)       // stale failure mid-cooldown — ignored, openedAt unchanged
	now = now.Add(30 * time.Second) // t=60s: original cooldown has elapsed
	if !allowed(b) {
		t.Fatal("a stale failure must not extend the cooldown past the original open time")
	}
}

func TestShouldTripClassification(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	transportErr := errors.New("dial tcp: connection reset by peer")

	cases := []struct {
		name string
		code int
		err  error
		ctx  context.Context
		want bool
	}{
		{"500 server error", 500, nil, live, true},
		{"502 bad gateway", 502, nil, live, true},
		{"503 unavailable", 503, nil, live, true},
		{"504 gateway timeout", 504, nil, live, true},
		{"429 too many requests", 429, nil, live, true},
		{"408 request timeout", 408, nil, live, true},
		{"transport error, live ctx", 0, transportErr, live, true},
		{"client-timeout DeadlineExceeded, live caller ctx", 0, context.DeadlineExceeded, live, true},
		{"200 ok", 200, nil, live, false},
		{"301 redirect", 301, nil, live, false},
		{"400 bad request", 400, nil, live, false},
		{"401 unauthorized", 401, nil, live, false},
		{"403 forbidden", 403, nil, live, false},
		{"404 not found", 404, nil, live, false},
		{"422 unprocessable", 422, nil, live, false},
		{"explicit context.Canceled error", 0, context.Canceled, live, false},
		{"cancelled ctx with 503", 503, nil, cancelled, false},
		{"cancelled ctx with transport error", 0, transportErr, cancelled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldTrip(tc.code, tc.err, tc.ctx); got != tc.want {
				t.Fatalf("ShouldTrip(%d, %v) = %v, want %v", tc.code, tc.err, got, tc.want)
			}
		})
	}
}

// DoWithRetry retries internally; the breaker must record exactly ONE outcome
// per logical call, not one per retry attempt — otherwise the threshold trips
// MaxAttempts× too fast.
func TestDoWithRetryBreakerRecordsOneOutcomePerCall(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute, now: func() time.Time { return now }})

	var calls int
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return statusResponse(http.StatusServiceUnavailable), nil // 503 every attempt
	})}
	opts := RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, Sleep: noSleep}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/", nil)
	resp, err := DoWithRetryBreaker(context.Background(), client, req, opts, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if calls != 3 {
		t.Fatalf("downstream calls = %d, want 3 (DoWithRetry retried 503)", calls)
	}
	// One logical failure recorded → with threshold 2 the breaker is still
	// closed. Per-retry counting would have recorded 3 and tripped it open.
	if !allowed(b) {
		t.Fatal("breaker tripped after one logical call: failures counted per-retry, not per-call")
	}

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/", nil)
	resp2, _ := DoWithRetryBreaker(context.Background(), client, req2, opts, b)
	if resp2 != nil {
		resp2.Body.Close()
	}
	if allowed(b) {
		t.Fatal("breaker must be open after two logical failing calls (threshold 2)")
	}
}

// The breaker's primary job is catching slow/hung upstreams. Providers rely on
// http.Client.Timeout (no per-request deadline), which fires as
// context.DeadlineExceeded while the CALLER context is still live. This must
// trip — regression guard for the bug where such timeouts were recorded as
// success and the breaker never tripped on a hung provider.
func TestDoWithRetryBreakerTripsOnClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond) // outlast the client timeout
	}))
	defer server.Close()
	client := &http.Client{Timeout: 20 * time.Millisecond}

	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute})
	opts := RetryOptions{MaxAttempts: 1, BaseDelay: time.Millisecond, Sleep: noSleep}

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		resp, err := DoWithRetryBreaker(context.Background(), client, req, opts, b)
		if err == nil {
			resp.Body.Close()
			t.Fatalf("attempt %d: expected a client-timeout error", i)
		}
	}
	if allowed(b) {
		t.Fatal("breaker must trip after consecutive client timeouts — catching hung upstreams is its core purpose")
	}
}

// A call aborted by the caller's own cancellation is neither success nor
// failure: it must not reset a closed-state failure streak (recording it as a
// spurious success would).
func TestDoWithRetryBreakerRecordsNeitherOnCallerCancellation(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute, now: func() time.Time { return now }})
	failCall(b) // one real failure: consecFails = 1 (threshold 2, still closed)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://provider.invalid/", nil)
	_, _ = DoWithRetryBreaker(ctx, client, req, RetryOptions{MaxAttempts: 1, BaseDelay: time.Millisecond, Sleep: noSleep}, b)

	// The cancelled call must have recorded nothing. One more real failure should
	// now reach the threshold (1 + 1 = 2). If the cancellation had been recorded
	// as a success, the streak would have reset and this would NOT trip.
	failCall(b)
	if allowed(b) {
		t.Fatal("a caller-cancelled call must record neither success nor failure (it reset the streak)")
	}
}

// A half-open probe aborted by caller cancellation is inconclusive: it must
// re-open the breaker (conservative), not close it and not wedge it half-open
// with no probe in flight.
func TestBreakerCancelledProbeReopens(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	failCall(b) // open at t=0

	now = now.Add(2 * time.Minute) // cooldown elapsed

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://provider.invalid/", nil)
	// Admits the half-open probe (cooldown elapsed), then the cancelled outcome
	// re-opens it via RecordCancellation.
	_, _ = DoWithRetryBreaker(ctx, client, req, RetryOptions{MaxAttempts: 1, BaseDelay: time.Millisecond, Sleep: noSleep}, b)

	if allowed(b) {
		t.Fatal("a cancelled half-open probe must re-open the breaker, not close or wedge it")
	}
	if !b.IsOpen() {
		t.Fatal("breaker must be open after a cancelled probe")
	}
}

func TestDoWithRetryBreakerNilIsPassthrough(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return okResponse(), nil
	})}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/", nil)
	resp, err := DoWithRetryBreaker(context.Background(), client, req, RetryOptions{MaxAttempts: 1, BaseDelay: time.Millisecond, Sleep: noSleep}, nil)
	if err != nil {
		t.Fatalf("nil breaker passthrough failed: %v", err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (nil breaker is a plain DoWithRetry)", calls)
	}
}
