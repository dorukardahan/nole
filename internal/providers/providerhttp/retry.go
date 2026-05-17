package providerhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

type RetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Sleep       func(context.Context, time.Duration) error
}

func DefaultRetryOptions() RetryOptions {
	maxAttempts := envInt("NOLE_RETRY_MAX_ATTEMPTS", 2)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > 5 {
		maxAttempts = 5
	}
	baseDelay := time.Duration(envInt("NOLE_RETRY_BASE_DELAY_MS", 250)) * time.Millisecond
	if baseDelay <= 0 {
		baseDelay = 250 * time.Millisecond
	}
	return RetryOptions{
		MaxAttempts: maxAttempts,
		BaseDelay:   baseDelay,
		MaxDelay:    5 * time.Second,
		Sleep:       sleepContext,
	}
}

func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request, opts RetryOptions) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	opts = normalizeOptions(opts)
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		attemptReq, err := cloneRequestForAttempt(ctx, req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(attemptReq)
		if err != nil {
			lastErr = err
			return nil, err
		}
		if !isTransientStatus(resp.StatusCode) || attempt == opts.MaxAttempts {
			return resp, nil
		}
		delay := retryDelay(resp.Header, opts, attempt)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if err := opts.Sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("retry attempts exhausted")
}

func cloneRequestForAttempt(ctx context.Context, req *http.Request, attempt int) (*http.Request, error) {
	clone := req.Clone(ctx)
	if req.Body == nil || req.Body == http.NoBody {
		return clone, nil
	}
	if req.GetBody == nil {
		if attempt == 1 {
			clone.Body = req.Body
			return clone, nil
		}
		return nil, fmt.Errorf("request body is not replayable for retry")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("recreate request body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

func retryDelay(headers http.Header, opts RetryOptions, attempt int) time.Duration {
	if raw := headers.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if t, err := http.ParseTime(raw); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
		}
	}
	delay := opts.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if opts.MaxDelay > 0 && delay > opts.MaxDelay {
			return opts.MaxDelay
		}
	}
	return delay
}

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func normalizeOptions(opts RetryOptions) RetryOptions {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 2
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 250 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 5 * time.Second
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepContext
	}
	return opts
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
