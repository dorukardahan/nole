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
//
// generation is bumped on every state transition. A caller captures the
// generation at Allow and passes it back to Record*; an outcome whose
// generation no longer matches is from a call admitted in a previous regime
// (the lock-free I/O window in DoWithRetryBreaker lets a stale call finish after
// the breaker has moved on) and is ignored, so it can never be mis-attributed —
// e.g. a slow call admitted while CLOSED cannot masquerade as the half-open
// probe and prematurely close a still-unhealthy upstream.
type Breaker struct {
	mu          sync.Mutex
	state       breakerState
	consecFails int
	openedAt    time.Time
	generation  uint64
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

// Allow reports whether a call may proceed and returns the breaker generation
// the call is admitted under. The generation MUST be passed back to
// RecordSuccess/RecordFailure so an outcome from a call admitted in a previous
// regime is ignored rather than mis-attributed (see the Breaker doc). A closed
// breaker always allows. An open breaker allows once the cooldown has elapsed,
// transitioning to half-open and handing out exactly one probe token; a second
// concurrent Allow during the in-flight probe returns false so a flood cannot
// stampede a recovering upstream.
func (b *Breaker) Allow() (bool, uint64) {
	if b == nil {
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.toHalfOpenLocked() // admit exactly one probe under a fresh generation
			return true, b.generation
		}
		return false, b.generation
	case breakerHalfOpen:
		return false, b.generation // exactly one probe in flight
	default: // closed
		return true, b.generation
	}
}

// RecordSuccess registers a successful call admitted under generation gen. If
// the breaker has since changed regime (gen != current) the outcome is stale
// and ignored. Otherwise: a half-open probe success closes the breaker; a
// closed-state success breaks the consecutive-failure streak.
func (b *Breaker) RecordSuccess(gen uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.generation {
		return // outcome from a previous regime — not the admitted call we track
	}
	switch b.state {
	case breakerHalfOpen:
		b.toClosedLocked() // the probe succeeded
	case breakerClosed:
		b.consecFails = 0
	}
}

// RecordFailure registers a failed call admitted under generation gen. Stale
// outcomes (gen mismatch) are ignored. Otherwise: a half-open probe failure
// re-opens the breaker and restarts the cooldown; a closed-state failure trips
// it open once the consecutive-failure threshold is reached.
func (b *Breaker) RecordFailure(gen uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.generation {
		return
	}
	switch b.state {
	case breakerHalfOpen:
		b.toOpenLocked() // the probe failed
	case breakerClosed:
		b.consecFails++
		if b.consecFails >= b.threshold {
			b.toOpenLocked()
		}
	}
}

// RecordCancellation registers that a call admitted under gen was aborted by
// the CALLER (not the provider) — e.g. a client disconnect. Stale outcomes (gen
// mismatch) are ignored. A closed-state cancellation is a no-op: the caller
// leaving must not reset the consecutive-failure streak (recording it as a
// success would). A half-open probe that is cancelled is INCONCLUSIVE — no
// healthy upstream response was observed — so the breaker re-opens
// conservatively rather than closing on it or wedging half-open with no probe
// in flight.
func (b *Breaker) RecordCancellation(gen uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.generation {
		return
	}
	if b.state == breakerHalfOpen {
		b.toOpenLocked()
	}
}

// Transition helpers bump the generation so any in-flight call admitted under
// the prior regime is ignored when it reports. Callers hold b.mu.
func (b *Breaker) toOpenLocked() {
	b.state = breakerOpen
	b.openedAt = b.now()
	b.generation++
}

func (b *Breaker) toHalfOpenLocked() {
	b.state = breakerHalfOpen
	b.generation++
}

func (b *Breaker) toClosedLocked() {
	b.state = breakerClosed
	b.consecFails = 0
	b.generation++
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

// State returns the raw stored lifecycle state ("closed" | "open" |
// "half-open") WITHOUT applying the cooldown check — a side-effect-free peek
// for observability that never consumes the half-open probe token. This can
// legitimately differ from IsOpen(): a breaker whose cooldown has elapsed still
// reports State()=="open" (it has not yet been probed) even though IsOpen()
// returns false because the next call is probe-eligible. Callers needing the
// binary "currently short-circuiting" truth use IsOpen(); State() is the honest
// raw lifecycle surfaced in provider_status. A nil breaker reports "closed".
func (b *Breaker) State() string {
	if b == nil {
		return "closed"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// ConsecFails returns the current consecutive-failure count (read under lock).
// A nil breaker reports 0. Observability only.
func (b *Breaker) ConsecFails() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.consecFails
}

// OpenedAt returns the wall-clock time the breaker last transitioned to open,
// or the zero time if it has never opened. Read under lock; nil → zero time.
// Surfaced (as an RFC3339 timestamp) only while the breaker is open/half-open so
// the agent can compute its own recovery window — Nólë never pre-computes one.
func (b *Breaker) OpenedAt() time.Time {
	if b == nil {
		return time.Time{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openedAt
}

// BreakerStatusFields builds the observability triple a provider's Status()
// reports: the raw lifecycle state, the consecutive-failure count, and (only
// while open/half-open) the open-since timestamp in RFC3339 UTC. A nil breaker
// (unbreakered providers like DDGS/Scrapling) yields all-empty so the
// ProviderStatus fields stay omitted. It deliberately exposes neither the
// breaker's generation nor its threshold — those are internal mechanics with no
// agent use.
func BreakerStatusFields(b *Breaker) (state string, consecFails int, openedAt string) {
	if b == nil {
		return "", 0, ""
	}
	state = b.State()
	consecFails = b.ConsecFails()
	if state == "open" || state == "half-open" {
		if t := b.OpenedAt(); !t.IsZero() {
			openedAt = t.UTC().Format(time.RFC3339)
		}
	}
	return state, consecFails, openedAt
}

// ShouldTrip classifies one logical HTTP outcome as a breaker failure. It trips
// on transport/dial errors and on 5xx / transient statuses (>=500, plus 429/408
// via isTransientStatus). It does NOT trip on success/redirect/4xx client
// errors (a bad key or query is not an upstream outage) or on caller-driven
// cancellation, which is never the provider's fault.
//
// Crucially, an http.Client.Timeout firing on a slow/hung upstream surfaces as
// context.DeadlineExceeded WHILE the caller's context is still live — that is a
// provider failure and MUST trip (catching slow upstreams is the breaker's
// primary job). So we exclude only genuine caller-driven cancellation: a
// context we were handed that is already done (ctx.Err() != nil) or an explicit
// context.Canceled. A bare DeadlineExceeded with a live caller context is NOT
// excluded — it is the upstream timing out.
func ShouldTrip(statusCode int, err error, ctx context.Context) bool {
	if ctx != nil && ctx.Err() != nil {
		return false // caller went away — not the provider's fault
	}
	if errors.Is(err, context.Canceled) {
		return false // explicit caller cancellation
	}
	if err != nil {
		return true // transport/dial error or client-side timeout (DeadlineExceeded) on a live caller
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
	allowed, gen := b.Allow()
	if !allowed {
		return nil, ErrCircuitOpen
	}
	resp, err := DoWithRetry(ctx, client, req, opts)
	// A call aborted by the CALLER's own cancellation/deadline is not a provider
	// success: a disconnecting client must not heal a failing breaker or reset a
	// closed-state failure streak. RecordCancellation handles it — a no-op when
	// closed, a conservative re-open when it was the half-open probe (so the
	// breaker neither closes on an unobserved upstream nor wedges with no probe
	// in flight). A client-side http.Client.Timeout has a LIVE caller ctx here,
	// so it does NOT take this branch — it flows to ShouldTrip and trips, which
	// is exactly how a slow/hung upstream is caught.
	if ctx != nil && ctx.Err() != nil {
		b.RecordCancellation(gen)
		return resp, err
	}
	if ShouldTrip(statusCodeOf(resp), err, ctx) {
		b.RecordFailure(gen)
	} else {
		b.RecordSuccess(gen)
	}
	return resp, err
}

func statusCodeOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
