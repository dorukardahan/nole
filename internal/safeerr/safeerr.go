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
	regexp.MustCompile(`https?://[^\s,)]+`),
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
