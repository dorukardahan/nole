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

func TestBreakerTripsAfterNConsecutiveFailures(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 3, Cooldown: time.Minute, now: func() time.Time { return now }})

	if !b.Allow() {
		t.Fatal("a fresh (closed) breaker must allow")
	}
	b.RecordFailure()
	b.RecordFailure()
	if !b.Allow() {
		t.Fatal("breaker must stay closed below the threshold")
	}
	b.RecordFailure() // 3rd consecutive → trip
	if b.Allow() {
		t.Fatal("breaker must be open once the threshold is reached")
	}
	if !b.IsOpen() {
		t.Fatal("IsOpen() must report the open breaker")
	}

	// A success resets the counter even after partial accumulation.
	b2 := NewBreaker(BreakerOptions{Threshold: 3, Cooldown: time.Minute, now: func() time.Time { return now }})
	b2.RecordFailure()
	b2.RecordFailure()
	b2.RecordSuccess()
	b2.RecordFailure()
	b2.RecordFailure()
	if !b2.Allow() {
		t.Fatal("a success must reset the consecutive-failure counter")
	}
}

func TestBreakerOpenShortCircuitsWithoutCallingDownstream(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: func() time.Time { return now }})
	b.RecordFailure() // threshold 1 → open immediately

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
	b.RecordFailure() // open at t=0

	if b.Allow() {
		t.Fatal("within cooldown, open breaker must deny")
	}
	now = now.Add(2 * time.Minute) // past cooldown
	if !b.Allow() {
		t.Fatal("after cooldown, the first Allow must permit a single probe")
	}
	if b.Allow() {
		t.Fatal("a second Allow during the in-flight probe must be denied")
	}
}

func TestBreakerHalfOpenOutcomeClosesOrReopens(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }

	// Probe success closes the breaker.
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	b.RecordFailure()
	now = now.Add(2 * time.Minute)
	if !b.Allow() {
		t.Fatal("probe should be allowed after cooldown")
	}
	b.RecordSuccess()
	if !b.Allow() {
		t.Fatal("a successful probe must close the breaker")
	}

	// Probe failure re-opens the breaker and restarts the cooldown.
	b2 := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	b2.RecordFailure()
	now = now.Add(2 * time.Minute)
	if !b2.Allow() {
		t.Fatal("probe should be allowed after cooldown")
	}
	b2.RecordFailure() // probe failed
	if b2.Allow() {
		t.Fatal("a failed probe must re-open the breaker for a fresh cooldown")
	}
}

// A success admitted while the breaker was CLOSED can return LATE, after
// concurrent failures already tripped it OPEN (the lock-free I/O window). That
// stale success must NOT re-close the breaker.
func TestBreakerStaleSuccessDoesNotReopenTrippedBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	b := NewBreaker(BreakerOptions{Threshold: 2, Cooldown: time.Minute, now: func() time.Time { return now }})
	b.RecordFailure()
	b.RecordFailure() // → open
	if b.Allow() {
		t.Fatal("breaker should be open after reaching the threshold")
	}
	b.RecordSuccess() // stale success landing after the trip
	if b.Allow() {
		t.Fatal("a stale success must not re-close a breaker that just tripped open")
	}
	if !b.IsOpen() {
		t.Fatal("breaker must remain open after a stale success")
	}
}

// Symmetric to the stale-success case: a failure that lands while already OPEN
// (a call admitted before the trip) must not bump openedAt and extend the
// cooldown beyond the original open time.
func TestBreakerStaleFailureDoesNotExtendCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	b := NewBreaker(BreakerOptions{Threshold: 1, Cooldown: time.Minute, now: clock})
	b.RecordFailure() // open at t=0
	now = now.Add(30 * time.Second)
	b.RecordFailure()               // stale failure mid-cooldown — must NOT reset openedAt
	now = now.Add(30 * time.Second) // t=60s: original cooldown has elapsed
	if !b.Allow() {
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
	if !b.Allow() {
		t.Fatal("breaker tripped after one logical call: failures counted per-retry, not per-call")
	}

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/", nil)
	resp2, _ := DoWithRetryBreaker(context.Background(), client, req2, opts, b)
	if resp2 != nil {
		resp2.Body.Close()
	}
	if b.Allow() {
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
	if b.Allow() {
		t.Fatal("breaker must trip after consecutive client timeouts — catching hung upstreams is its core purpose")
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
