package core

import (
	"errors"
	"fmt"
)

// ErrBotBlocked identifies a recognized extraction interstitial without
// retaining any page payload. Providers may wrap it with their static name.
var ErrBotBlocked = errors.New("bot_blocked")

// ErrTargetRateLimited identifies a target-page rate limit, independent of the
// extraction provider's account quota.
var ErrTargetRateLimited = errors.New("rate_limited")

type NoFreeQuotaError struct {
	Task     TaskType `json:"task"`
	Provider []string `json:"providers"`
}

func (e NoFreeQuotaError) Error() string {
	return fmt.Sprintf("no_free_quota: no free provider available for task %q", e.Task)
}

// IsNoFreeQuota reports whether err is (or wraps) a NoFreeQuotaError. It uses
// errors.As so it is robust to %w wrapping anywhere on the call path — matching
// the sibling quota check in drift.go (isQuotaExhausted) and keeping the REST 402
// mapping correct even if a future caller wraps the error. errors.As also matches
// a bare (unwrapped) value, so the current bare-return hot path stays detected.
func IsNoFreeQuota(err error) bool {
	if err == nil {
		return false
	}
	var q NoFreeQuotaError
	if errors.As(err, &q) {
		return true
	}
	var qp *NoFreeQuotaError
	return errors.As(err, &qp)
}
