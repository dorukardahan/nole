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
