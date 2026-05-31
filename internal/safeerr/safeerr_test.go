package safeerr

import (
	"strings"
	"testing"
)

func TestRedactRemovesCommonCredentialFormats(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
	}{
		{
			name:    "json api key",
			input:   `provider error: {"api_key":"fake-api-key-value"}`,
			secrets: []string{"fake-api-key-value"},
		},
		{
			name:    "json token with spaces",
			input:   `provider error: {"token": "fake-token-value"}`,
			secrets: []string{"fake-token-value"},
		},
		{
			name:    "env style password",
			input:   "password=fake-password-value",
			secrets: []string{"fake-password-value"},
		},
		{
			name:    "bearer header",
			input:   "Authorization: Bearer fake-bearer-value",
			secrets: []string{"fake-bearer-value"},
		},
		{
			name:    "private url",
			input:   "request failed: https://example.invalid/private?token=fake-url-token",
			secrets: []string{"https://example.invalid/private?token=fake-url-token", "fake-url-token"},
		},
		{
			name:    "set-cookie session token",
			input:   "Set-Cookie: session=abc123; Path=/; HttpOnly",
			secrets: []string{"session=abc123", "abc123"},
		},
		{
			name:    "bare cookie header",
			input:   "Cookie: session=fake-cookie-value",
			secrets: []string{"session=fake-cookie-value", "fake-cookie-value"},
		},
		{
			name:    "non-http scheme userinfo credentials",
			input:   "dial error: ftp://user:pass@host/x",
			secrets: []string{"ftp://user:pass@host/x", "user:pass"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("redacted output still contains %q: %q", secret, got)
				}
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("expected redaction marker in %q", got)
			}
		})
	}
}

func TestRedactDoesNotOverRedactBenignText(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "i love cookies", input: "I love cookies"},
		{name: "cookie crumbles", input: "cookie crumbles"},
		{name: "bare word ftp", input: "ftp"},
		{name: "ftp word in sentence", input: "please use the ftp upload form"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if got != strings.TrimSpace(tt.input) {
				t.Fatalf("benign text %q was modified to %q", tt.input, got)
			}
			if strings.Contains(got, "[REDACTED]") {
				t.Fatalf("benign text %q triggered redaction: %q", tt.input, got)
			}
		})
	}
}
