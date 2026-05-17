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

func noSleep(ctx context.Context, d time.Duration) error { return nil }
