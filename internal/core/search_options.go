package core

import (
	"errors"
	"fmt"
	"strings"
)

// InvalidRequestError marks caller-controlled input errors. It is distinct from
// provider/runtime failures so REST can return 400 instead of a misleading 500.
type InvalidRequestError struct {
	Field  string
	Reason string
}

func (e InvalidRequestError) Error() string {
	if e.Field == "" {
		return "invalid request"
	}
	if e.Reason == "" {
		return "invalid " + e.Field
	}
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

func IsInvalidRequest(err error) bool {
	var target InvalidRequestError
	return errors.As(err, &target)
}

func normalizeSearchOptions(opts SearchOptions) (SearchOptions, error) {
	var err error
	if opts.Country, err = normalizeTwoLetterOption("country", opts.Country); err != nil {
		return SearchOptions{}, err
	}
	if opts.SearchLang, err = normalizeLocaleOption("search_lang", opts.SearchLang); err != nil {
		return SearchOptions{}, err
	}
	if opts.UILang, err = normalizeLocaleOption("ui_lang", opts.UILang); err != nil {
		return SearchOptions{}, err
	}
	if opts.SafeSearch, err = normalizeSafeSearch(opts.SafeSearch); err != nil {
		return SearchOptions{}, err
	}
	if opts.Freshness, err = normalizeFreshness(opts.Freshness); err != nil {
		return SearchOptions{}, err
	}
	return opts, nil
}

func normalizeTwoLetterOption(field, value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return "", nil
	}
	if len(v) != 2 || !isASCIILetters(v) {
		return "", InvalidRequestError{Field: field, Reason: "must be a two-letter code such as us or tr"}
	}
	return v, nil
}

func normalizeLocaleOption(field, value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return "", nil
	}
	if len(v) < 2 || len(v) > 20 || strings.HasPrefix(v, "-") || strings.HasSuffix(v, "-") {
		return "", InvalidRequestError{Field: field, Reason: "must be a language/locale code such as en or en-us"}
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return "", InvalidRequestError{Field: field, Reason: "must contain only letters, digits, or hyphen"}
	}
	return v, nil
}

func normalizeSafeSearch(value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "", "off", "moderate", "strict":
		return v, nil
	default:
		return "", InvalidRequestError{Field: "safesearch", Reason: "must be one of off, moderate, strict"}
	}
}

func normalizeFreshness(value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "":
		return "", nil
	case "pd", "day", "d":
		return "pd", nil
	case "pw", "week", "w":
		return "pw", nil
	case "pm", "month", "m":
		return "pm", nil
	case "py", "year", "y":
		return "py", nil
	default:
		return "", InvalidRequestError{Field: "freshness", Reason: "must be one of pd/day, pw/week, pm/month, py/year"}
	}
}

func isASCIILetters(v string) bool {
	for _, r := range v {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func FreshnessTimeRange(freshness string) string {
	switch freshness {
	case "pd":
		return "day"
	case "pw":
		return "week"
	case "pm":
		return "month"
	case "py":
		return "year"
	default:
		return ""
	}
}

func FreshnessTBS(freshness string) string {
	switch freshness {
	case "pd":
		return "qdr:d"
	case "pw":
		return "qdr:w"
	case "pm":
		return "qdr:m"
	case "py":
		return "qdr:y"
	default:
		return ""
	}
}
