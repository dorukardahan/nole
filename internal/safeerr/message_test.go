package safeerr

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

// Message is the public entry point used by 8 call sites, but only Redact was
// tested. These lock in Message's two branches: the nil guard, the
// HTTPStatusError fast-path (which bypasses Redact), and the Redact fallback.

func TestMessageNilReturnsEmpty(t *testing.T) {
	if got := Message(nil); got != "" {
		t.Fatalf("Message(nil) = %q, want empty string", got)
	}
}

func TestMessageRedactsPlainError(t *testing.T) {
	// Fixture token uses a FAKE- prefix so the repo secret scanner allowlists
	// it as an obvious non-secret; the Redact regex still matches and strips it.
	err := errors.New("provider call failed: Authorization: Bearer FAKE-bearer-token-value")
	got := Message(err)
	if strings.Contains(got, "FAKE-bearer-token-value") {
		t.Fatalf("Message leaked a bearer token: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got %q", got)
	}
}

func TestMessageHTTPStatusErrorBypassesRedactAndStaysBodyFree(t *testing.T) {
	// A wrapped *HTTPStatusError must surface its structured, already-body-free
	// text rather than being run through the regex Redact path. This documents
	// the intentional contract that the outer fmt.Errorf wrapping context is
	// dropped (see safeerr.go: errors.As returns the inner status error).
	// Fixture body uses FAKE- prefixed tokens so the repo secret scanner
	// allowlists them; NewHTTPStatusError never stores the body anyway, so the
	// assertion below proves the body content cannot reach the message text.
	base := providerhttp.NewHTTPStatusError("brave", "search", 503, []byte("api_key=FAKEKEY-leak-me-123456 FAKE-body-content"))
	wrapped := fmt.Errorf("brave provider call context: %w", base)

	got := Message(wrapped)
	if got != base.Error() {
		t.Fatalf("Message should return the structured status error text verbatim, got %q, want %q", got, base.Error())
	}
	if !strings.Contains(got, "HTTP 503") || !strings.Contains(got, "response body redacted") {
		t.Fatalf("structured status error text missing expected markers: %q", got)
	}
	if strings.Contains(got, "FAKEKEY-leak-me-123456") || strings.Contains(got, "FAKE-body-content") {
		t.Fatalf("Message leaked raw response body content: %q", got)
	}
}
