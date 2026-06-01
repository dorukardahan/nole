package core

import "errors"

// httpStatuser is satisfied by any error that carries a structured HTTP status
// code. providerhttp.HTTPStatusError implements it via HTTPStatus(). Using a
// duck-typed interface keeps the domain core free of any import of the provider
// HTTP-infrastructure package while still letting the drift detector classify a
// provider 429 deterministically — never by string-matching an error message.
type httpStatuser interface{ HTTPStatus() int }

// isQuotaExhausted reports whether err unwraps to a provider HTTP 429
// (over-quota / too-many-requests). errors.As walks the %w wrap chain to find
// the first error implementing httpStatuser. It is the sole input to the drift
// signal and is NEVER consulted for any routing decision (drift is
// one-directional: the service writes it, only BudgetStatus/ProviderStatus
// read it — the Router never sees it).
func isQuotaExhausted(err error) bool {
	var s httpStatuser
	if errors.As(err, &s) {
		return s.HTTPStatus() == 429
	}
	return false
}
