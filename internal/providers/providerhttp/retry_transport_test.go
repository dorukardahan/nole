package providerhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     make(http.Header),
	}
}

// A transport-level error (connection reset, DNS blip) is transient and should
// be retried up to MaxAttempts, not returned on the first failure.
func TestDoWithRetryRetriesTransportErrorThenSucceeds(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("dial tcp: connection reset by peer")
		}
		return okResponse(), nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithRetry(context.Background(), client, req, RetryOptions{MaxAttempts: 2, BaseDelay: time.Millisecond, Sleep: noSleep})
	if err != nil {
		t.Fatalf("expected transport error to be retried to success, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (one retry after transport error)", calls)
	}
}

func TestDoWithRetryTransportErrorRespectsMaxAttempts(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("dial tcp: connection reset by peer")
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://provider.invalid/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DoWithRetry(context.Background(), client, req, RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, Sleep: noSleep})
	if err == nil {
		t.Fatal("expected error after exhausting attempts on persistent transport failure")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (MaxAttempts)", calls)
	}
}

// A dead context must short-circuit transport retries rather than burn attempts.
func TestDoWithRetryTransportErrorStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("dial tcp: connection reset by peer")
	})}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://provider.invalid/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DoWithRetry(ctx, client, req, RetryOptions{MaxAttempts: 3, BaseDelay: time.Millisecond, Sleep: noSleep})
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
	if calls > 1 {
		t.Fatalf("calls = %d, want <= 1 (no retry on canceled context)", calls)
	}
}

// 408 is labelled transient by statusCategory; the retry policy must agree.
func TestDoWithRetryRetriesRequestTimeout(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
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
		t.Fatalf("attempts = %d, want 2 (408 must be retried)", attempts)
	}
}
