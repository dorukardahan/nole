package safeerr

import (
	"errors"
	"regexp"
	"strings"

	"github.com/dorukardahan/nole/internal/providers/providerhttp"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)["']?(api[_-]?key|token|secret|password)["']?\s*[=:]\s*["']?[^"'\s,;}]+["']?`),
	// Cookie/Set-Cookie headers can carry session tokens; redact the whole value.
	regexp.MustCompile(`(?i)(set-)?cookie\s*:\s*[^\r\n]+`),
	// Any scheme://... URL (not just http/https) may carry userinfo
	// credentials (e.g. ftp://user:pass@host); the leading scheme letter
	// plus the literal "://" keeps benign words like "ftp" from matching.
	regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s,)]+`),
}

func Message(err error) string {
	if err == nil {
		return ""
	}
	var statusErr *providerhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Error()
	}
	return Redact(err.Error())
}

func Redact(text string) string {
	out := strings.TrimSpace(text)
	for _, pattern := range sensitivePatterns {
		out = pattern.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}
