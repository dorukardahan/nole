package providerhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoWithRetryRetriesTransientStatusThenSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Fatalf("request body was not replayed, got %q", string(body))
		}
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("try later"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithRetry(context.Background(), server.Client(), req, RetryOptions{MaxAttempts: 2, BaseDelay: time.Millisecond, Sleep: noSleep})
	if err != nil {
		t.Fatalf("DoWithRetry failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoWithRetryDoesNotRetryNonTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithRetry(context.Background(), server.Client(), req, RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, Sleep: noSleep})
	if err != nil {
		t.Fatalf("DoWithRetry failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

type countingRetryBody struct {
	read  int64
	total int64
}

func (b *countingRetryBody) Read(p []byte) (int, error) {
	if b.read >= b.total {
		return 0, io.EOF
	}
	n := int64(len(p))
	if remaining := b.total - b.read; n > remaining {
		n = remaining
	}
	b.read += n
	return int(n), nil
}

func (*countingRetryBody) Close() error { return nil }

func TestDoWithRetryBoundsTransientResponseDrain(t *testing.T) {
	body := &countingRetryBody{total: MaxSearchResponseBytes + 1}
	attempts := 0
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: body}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://provider.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithRetry(context.Background(), client, req, RetryOptions{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Sleep: noSleep})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if body.read > MaxSearchResponseBytes {
		t.Fatalf("retry drain exceeded shared response cap: read=%d cap=%d", body.read, MaxSearchResponseBytes)
	}
}

func TestDoWithRetryRespectsRetryAfterHeader(t *testing.T) {
	var slept []time.Duration
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithRetry(context.Background(), server.Client(), req, RetryOptions{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		Sleep: func(ctx context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("DoWithRetry failed: %v", err)
	}
	defer resp.Body.Close()
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept = %#v, want 2s", slept)
	}
}

func TestRetryDelayCapsRetryAfterHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "3600")
	got := retryDelay(headers, RetryOptions{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second}, 1)
	if got != 5*time.Second {
		t.Fatalf("Retry-After seconds delay = %s, want capped 5s", got)
	}

	headers.Set("Retry-After", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	got = retryDelay(headers, RetryOptions{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second}, 1)
	if got > 5*time.Second {
		t.Fatalf("Retry-After date delay = %s, want <= 5s", got)
	}
}

func TestRetryDelayAppliesJitterBoundedByMaxDelay(t *testing.T) {
	// A deterministic jitter func: always return the FULL delay it was handed.
	// This proves (a) jitter is applied to the exponential path and (b) the
	// result stays capped at MaxDelay.
	identity := func(d time.Duration) time.Duration { return d }
	opts := RetryOptions{BaseDelay: time.Second, MaxDelay: 5 * time.Second, Jitter: identity}

	// attempt 1 -> base 1s, attempt 2 -> 2s, attempt 3 -> 4s, attempt 4 -> capped 5s.
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 5 * time.Second} {
		got := retryDelay(http.Header{}, opts, attempt)
		if got != want {
			t.Fatalf("attempt %d: identity jitter delay = %s, want %s", attempt, got, want)
		}
	}

	// A jitter func that tries to BLOW PAST the cap must still be re-capped.
	doubler := func(d time.Duration) time.Duration { return d * 100 }
	opts.Jitter = doubler
	got := retryDelay(http.Header{}, opts, 2)
	if got != 5*time.Second {
		t.Fatalf("over-cap jitter delay = %s, want re-capped 5s", got)
	}
}

func TestRetryDelayNilJitterStaysExact(t *testing.T) {
	// Mirrors the by-hand RetryOptions construction used elsewhere: no Jitter
	// field means deterministic exponential values, unchanged by this feature.
	opts := RetryOptions{BaseDelay: time.Second, MaxDelay: 30 * time.Second}
	if got := retryDelay(http.Header{}, opts, 3); got != 4*time.Second {
		t.Fatalf("nil-jitter attempt 3 delay = %s, want exact 4s", got)
	}
}

func TestRetryDelayJitterNotAppliedToRetryAfter(t *testing.T) {
	// Even with a jitter func present, a server-provided Retry-After must be
	// honored exactly so we never wait less than the server asked for.
	headers := http.Header{}
	headers.Set("Retry-After", "2")
	opts := RetryOptions{BaseDelay: time.Millisecond, MaxDelay: 30 * time.Second, Jitter: func(d time.Duration) time.Duration { return d / 1000 }}
	if got := retryDelay(headers, opts, 1); got != 2*time.Second {
		t.Fatalf("Retry-After with jitter present = %s, want exact 2s", got)
	}
}

func noSleep(ctx context.Context, d time.Duration) error { return nil }
