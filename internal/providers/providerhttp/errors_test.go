package providerhttp

import (
	"errors"
	"strings"
	"testing"
)

func TestHTTPStatusErrorIsStructuredAndRedactsResponseBody(t *testing.T) {
	rawBody := []byte(`{"error":"bad key","token":"SECRET_TOKEN","url":"https://private.example/path?api_key=SECRET"}`)
	err := NewHTTPStatusError("brave", "search", 429, rawBody)
	msg := err.Error()
	for _, forbidden := range []string{"SECRET_TOKEN", "api_key=SECRET", "private.example", "bad key"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("sanitized HTTP status error leaked %q in %q", forbidden, msg)
		}
	}
	for _, want := range []string{"brave", "search", "429"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("sanitized HTTP status error missing %q in %q", want, msg)
		}
	}
	var structured *HTTPStatusError
	if !errors.As(err, &structured) {
		t.Fatalf("expected structured HTTPStatusError, got %T", err)
	}
	if structured.Provider != "brave" || structured.Operation != "search" || structured.StatusCode != 429 || structured.BodyBytes != len(rawBody) {
		t.Fatalf("unexpected structured fields: %#v", structured)
	}
}
