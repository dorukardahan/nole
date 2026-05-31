package providerhttp

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ErrCircuitOpen is returned by DoWithRetryBreaker when the breaker is open and
// the request was short-circuited without contacting the upstream. It is a
// static, redaction-safe sentinel (carries no request or secret content).
var ErrCircuitOpen = errors.New("circuit breaker open: upstream temporarily unavailable")

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// BreakerOptions configures a Breaker. Threshold is the number of consecutive
// failures that trips a closed breaker open; Cooldown is how long it stays open
// before allowing a single half-open probe. now is an injectable clock for
// tests (nil -> time.Now).
type BreakerOptions struct {
	Threshold int
	Cooldown  time.Duration
	now       func() time.Time
}

// DefaultBreakerOptions returns env-tunable defaults (threshold 5, cooldown
// 30s), mirroring DefaultRetryOptions' clamp style.
func DefaultBreakerOptions() BreakerOptions {
	threshold := envInt("NOLE_BREAKER_THRESHOLD", 5)
	if threshold < 1 {
		threshold = 1
	}
	if threshold > 100 {
		threshold = 100
	}
	cooldownMS := envInt("NOLE_BREAKER_COOLDOWN_MS", 30000)
	if cooldownMS < 0 {
		cooldownMS = 0
	}
	return BreakerOptions{
		Threshold: threshold,
		Cooldown:  time.Duration(cooldownMS) * time.Millisecond,
	}
}

// Breaker is a per-provider circuit breaker. After Threshold consecutive
// failures it opens and short-circuits calls for Cooldown, then admits exactly
// one half-open probe whose outcome closes it (success) or re-opens it
// (failure). It is in-memory only (no disk persistence) and safe for concurrent
// use. A nil *Breaker is a valid no-op (Allow always true, Record* no-ops).
type Breaker struct {
	mu          sync.Mutex
	state       breakerState
	consecFails int
	openedAt    time.Time
	threshold   int
	cooldown    time.Duration
	now         func() time.Time
}

// NewBreaker builds a closed breaker from opts (clamped to sane values).
func NewBreaker(opts BreakerOptions) *Breaker {
	if opts.Threshold < 1 {
		opts.Threshold = 1
	}
	if opts.Cooldown < 0 {
		opts.Cooldown = 0
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}
	return &Breaker{
		state:     breakerClosed,
		threshold: opts.Threshold,
		cooldown:  opts.Cooldown,
		now:       now,
	}
}

// Allow reports whether a call may proceed. A closed breaker always allows. An
// open breaker allows once the cooldown has elapsed, transitioning to half-open
// and handing out exactly one probe token; a second concurrent Allow during the
// in-flight probe returns false so a flood cannot stampede a recovering upstream.
func (b *Breaker) Allow() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.state = breakerHalfOpen // consume the single probe token
			return true
		}
		return false
	case breakerHalfOpen:
		return false // a probe is already in flight
	default: // closed
		return true
	}
}

// RecordSuccess closes the breaker and resets the failure counter.
func (b *Breaker) RecordSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.consecFails = 0
}

// RecordFailure advances the breaker toward open. A half-open probe failure
// re-opens it and restarts the cooldown; otherwise the consecutive-failure
// count trips it open at the threshold.
func (b *Breaker) RecordFailure() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.state = breakerOpen
		b.openedAt = b.now()
		return
	}
	b.consecFails++
	if b.consecFails >= b.threshold {
		b.state = breakerOpen
		b.openedAt = b.now()
	}
}

// IsOpen reports whether the breaker is currently short-circuiting calls,
// WITHOUT transitioning state (a side-effect-free peek for observability that
// never consumes the half-open probe token).
func (b *Breaker) IsOpen() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return b.now().Sub(b.openedAt) < b.cooldown
	case breakerHalfOpen:
		return true
	default:
		return false
	}
}

// ShouldTrip classifies one logical HTTP outcome as a breaker failure. It trips
// on transport/dial errors (with a live context) and on 5xx / transient
// statuses (>=500, plus 429/408 via isTransientStatus). It does NOT trip on
// success/redirect/4xx client errors (a bad key or query is not an upstream
// outage) or on caller-driven cancellation (context.Canceled /
// DeadlineExceeded), which is never the provider's fault.
func ShouldTrip(statusCode int, err error, ctx context.Context) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if err != nil {
		return true // transport/dial error with a live context
	}
	if statusCode >= 500 {
		return true
	}
	return isTransientStatus(statusCode)
}

// DoWithRetryBreaker wraps DoWithRetry with a circuit breaker. When b is open it
// short-circuits with ErrCircuitOpen (no upstream call, no burned timeout, no
// quota debit since the provider's Record runs only after a successful call).
// The outcome of an allowed call is recorded ONCE per logical call (not per
// retry), so the threshold counts logical failures. A nil breaker is a no-op
// passthrough to DoWithRetry, preserving behaviour for unwired providers.
func DoWithRetryBreaker(ctx context.Context, client *http.Client, req *http.Request, opts RetryOptions, b *Breaker) (*http.Response, error) {
	if b == nil {
		return DoWithRetry(ctx, client, req, opts)
	}
	if !b.Allow() {
		return nil, ErrCircuitOpen
	}
	resp, err := DoWithRetry(ctx, client, req, opts)
	if ShouldTrip(statusCodeOf(resp), err, ctx) {
		b.RecordFailure()
	} else {
		b.RecordSuccess()
	}
	return resp, err
}

func statusCodeOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
