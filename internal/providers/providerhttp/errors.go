package providerhttp

import "fmt"

// HTTPStatusError is the public-safe error returned for non-2xx provider HTTP
// responses. It intentionally records only structured metadata and the raw body
// size; the response body itself is never stored or printed because providers can
// echo API keys, auth headers, private URLs, or other secrets in error payloads.
type HTTPStatusError struct {
	Provider   string
	Operation  string
	StatusCode int
	BodyBytes  int
	Category   string
}

func NewHTTPStatusError(provider, operation string, statusCode int, body []byte) error {
	return &HTTPStatusError{
		Provider:   provider,
		Operation:  operation,
		StatusCode: statusCode,
		BodyBytes:  len(body),
		Category:   statusCategory(statusCode),
	}
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "provider HTTP error"
	}
	category := e.Category
	if category == "" {
		category = statusCategory(e.StatusCode)
	}
	return fmt.Sprintf("%s: %s returned HTTP %d (%s; response body redacted, %d bytes)", e.Provider, e.Operation, e.StatusCode, category, e.BodyBytes)
}

func statusCategory(statusCode int) string {
	switch {
	case statusCode == 401 || statusCode == 403:
		return "auth"
	case statusCode == 408 || statusCode == 429 || statusCode == 502 || statusCode == 503 || statusCode == 504:
		return "transient"
	case statusCode >= 500:
		return "server"
	case statusCode >= 400:
		return "client"
	default:
		return "unexpected"
	}
}
