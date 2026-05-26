package cli

import (
	"os"
	"strings"
)

func loadDefaultNoleEnvFile() {
	if envFileLoadingDisabled() {
		return
	}
	path, err := defaultNoleEnvPath()
	if err != nil {
		return
	}
	_ = loadNoleEnvFile(path)
}

func envFileLoadingDisabled() bool {
	raw := strings.TrimSpace(os.Getenv("NOLE_DISABLE_ENV_FILE"))
	return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
}

func loadNoleEnvFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := parseShellEnvAssignment(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return nil
}

func parseShellEnvAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	key, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if !isEnvKey(key) {
		return "", "", false
	}
	value, ok := parseShellEnvValue(rawValue)
	if !ok {
		return "", "", false
	}
	return key, value, true
}

func isEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func parseShellEnvValue(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	switch value[0] {
	case '\'':
		return parseSingleQuotedShellValue(value)
	case '"':
		return parseDoubleQuotedShellValue(value)
	default:
		return trimUnquotedShellComment(value), true
	}
}

func parseSingleQuotedShellValue(value string) (string, bool) {
	var out strings.Builder
	for i := 1; i < len(value); {
		if value[i] != '\'' {
			out.WriteByte(value[i])
			i++
			continue
		}
		i++
		if strings.HasPrefix(value[i:], `"'"'`) {
			out.WriteByte('\'')
			i += len(`"'"'`)
			continue
		}
		rest := strings.TrimSpace(value[i:])
		return out.String(), rest == "" || strings.HasPrefix(rest, "#")
	}
	return "", false
}

func parseDoubleQuotedShellValue(value string) (string, bool) {
	var out strings.Builder
	escaped := false
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if escaped {
			if ch == '$' {
				out.WriteString(escapedDollarPlaceholder)
			} else {
				out.WriteByte(ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			rest := strings.TrimSpace(value[i+1:])
			return restoreEscapedDollars(os.ExpandEnv(out.String())), rest == "" || strings.HasPrefix(rest, "#")
		}
		out.WriteByte(ch)
	}
	return "", false
}

func trimUnquotedShellComment(value string) string {
	for i, r := range value {
		if r == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
			return os.ExpandEnv(strings.TrimSpace(value[:i]))
		}
	}
	return os.ExpandEnv(strings.TrimSpace(value))
}

const escapedDollarPlaceholder = "\x00NOLE_ESCAPED_DOLLAR\x00"

func restoreEscapedDollars(value string) string {
	return strings.ReplaceAll(value, escapedDollarPlaceholder, "$")
}
