// Package nolelog is Nólë's structured diagnostic logger.
//
// It gives operators visibility into a long-lived gateway process
// (`nole serve` / `nole mcp`) and one-shot CLI runs WITHOUT ever violating the
// two invariants the rest of Nólë depends on:
//
//  1. It writes ONLY to the io.Writer it is constructed with. Every production
//     call site passes os.Stderr. The package NEVER references os.Stdout, so
//     structured logging can never corrupt the MCP JSON-RPC stream on stdout, a
//     REST JSON body, or a --json command's machine-readable output. (w is a
//     parameter, not hardcoded, only so tests can capture output.)
//  2. It redacts secrets handed to it, in layers. Error fields flow through
//     safeerr.Message; plain field values through safeerr.Redact (which strips
//     credential-shaped content — a URL with userinfo, an inline "token=..."),
//     AND F additionally redacts the whole value when the field KEY names a
//     credential (api_key/token/secret/...), catching a bare token whose text
//     has no marker. This is defense-in-depth — no call site logs a secret — not
//     a guarantee a caller cannot defeat by mislabelling one under a benign key.
//
// NOLE_LOG selects the format: "text" (default, human-readable logfmt),
// "json" (one compact object per line), or "off" (silent). A nil *Logger and a
// Logger with a nil writer are safe no-ops, so call sites that were never wired
// a logger never panic.
//
// nolelog is a leaf package: it imports only the standard library and
// internal/safeerr. cli and core import it; it imports neither, so wiring it
// into core.Service (via core.WithLogger) introduces no import cycle.
package nolelog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/safeerr"
)

// Mode is the NOLE_LOG output format.
type Mode string

const (
	// ModeText is the default (unset NOLE_LOG): human-readable logfmt to stderr.
	ModeText Mode = "text"
	// ModeJSON emits one compact JSON object per line.
	ModeJSON Mode = "json"
	// ModeOff silences all diagnostic logging.
	ModeOff Mode = "off"
)

// ParseMode maps a NOLE_LOG value to a Mode. Empty or unrecognized input
// resolves to ModeText, so the unset default preserves human-readable stderr
// diagnostics and a typo never silently disables logging. Mirrors
// core.ParseCostPolicy's lenient, never-error shape.
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ModeJSON):
		return ModeJSON
	case string(ModeOff):
		return ModeOff
	default:
		return ModeText
	}
}

// Field is one structured key/value pair on a log record. Construct it only via
// F (redacted plain value) or Err (redacted error) — there is no exported way
// to attach an un-redacted value, which is what keeps secrets out of the log.
type Field struct {
	Key string
	Val string
}

const redactedMarker = "[REDACTED]"

// F builds a field from a plain value with two layers of secret-safety:
//   - if the KEY names a credential (token/secret/password/authorization/
//     bearer/cookie/credential/api_key/apikey/*_key), the value is fully replaced
//     with [REDACTED]. This catches a bare token value (e.g. "tvly-...") whose
//     text carries no credential marker for safeerr.Redact to match — the case
//     `F("api_key", os.Getenv("TAVILY_API_KEY"))`.
//   - otherwise the value is run through safeerr.Redact, which strips
//     credential-shaped content (a URL with userinfo, an inline "token=...").
//
// Keys are developer-controlled constants. This is defense-in-depth, not a
// licence to pass secrets: no call site logs one, and a caller that deliberately
// puts a credential under a benign field name can still defeat it.
func F(key, val string) Field {
	if isSensitiveKey(key) {
		return Field{Key: key, Val: redactedMarker}
	}
	return Field{Key: key, Val: safeerr.Redact(val)}
}

// isSensitiveKey reports whether a field key names a credential, so F redacts its
// value wholesale. Matches common credential nouns plus key-shaped names
// (api_key/apikey/x_key/key) while leaving benign words like "monkey"/"keyword".
func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "key" {
		return true
	}
	for _, marker := range []string{"token", "secret", "password", "passwd", "authorization", "bearer", "cookie", "credential", "apikey", "api_key", "api-key"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return strings.HasSuffix(k, "_key") || strings.HasSuffix(k, "-key") ||
		strings.HasPrefix(k, "key_") || strings.HasPrefix(k, "key-")
}

// Err builds an "error" field from err, routed through safeerr.Message (which
// redacts credentials and unwraps structured provider status errors to their
// body-free text). Returns a zero Field for a nil error so callers can pass it
// unconditionally; zero fields are dropped before emission.
func Err(err error) Field {
	if err == nil {
		return Field{}
	}
	return Field{Key: "error", Val: safeerr.Message(err)}
}

// Logger is a tiny value over an io.Writer and a Mode. It is NOT a mutable
// package global — each surface constructs its own with the writer it owns.
type Logger struct {
	w    io.Writer
	mode Mode
	now  func() time.Time
}

// New returns a Logger writing to w in the given mode.
func New(w io.Writer, mode Mode) *Logger {
	return &Logger{w: w, mode: mode, now: utcNow}
}

// FromEnv builds a logger writing to w with the mode named by NOLE_LOG.
// Production call sites pass os.Stderr.
func FromEnv(w io.Writer) *Logger {
	return New(w, ParseMode(os.Getenv("NOLE_LOG")))
}

func utcNow() time.Time { return time.Now().UTC() }

// isReservedKey reports whether a field key would collide with a system key the
// logger emits itself (ts, level, event). Such fields are dropped so every JSON
// record has a single, stable occurrence of each system key.
func isReservedKey(key string) bool {
	switch key {
	case "ts", "level", "event":
		return true
	default:
		return false
	}
}

// Mode reports the logger's configured mode (ModeText for a nil logger).
func (l *Logger) Mode() Mode {
	if l == nil {
		return ModeText
	}
	return l.mode
}

// Info logs an informational operational event.
func (l *Logger) Info(event string, fields ...Field) { l.emit("info", event, fields) }

// Warn logs a warning operational event.
func (l *Logger) Warn(event string, fields ...Field) { l.emit("warn", event, fields) }

// Error logs event at error level, appending the redacted err as a trailing
// "error" field. A nil err is fine (no error field added).
func (l *Logger) Error(event string, err error, fields ...Field) {
	if err != nil {
		fields = append(fields, Err(err))
	}
	l.emit("error", event, fields)
}

func (l *Logger) emit(level, event string, fields []Field) {
	if l == nil || l.w == nil || l.mode == ModeOff {
		return
	}
	// Drop zero-value fields (e.g. Err(nil)) so an unconditional Err() call
	// never prints an empty key, and drop any field whose key collides with a
	// reserved system key (ts/level/event) — those keys are owned by the logger,
	// so a colliding caller field is dropped rather than emitted as a duplicate
	// JSON key. Dropping (not panicking) keeps a stray log call from ever
	// crashing the gateway.
	kept := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Key == "" || isReservedKey(f.Key) {
			continue
		}
		kept = append(kept, f)
	}
	ts := l.now().Format(time.RFC3339)
	if l.mode == ModeJSON {
		l.emitJSON(ts, level, event, kept)
		return
	}
	l.emitText(ts, level, event, kept)
}

// emitJSON writes one compact line with a STABLE key order (ts, level, event,
// then fields in call order). Built by hand rather than json.Marshal of a map
// so the order is deterministic for downstream parsers and tests.
func (l *Logger) emitJSON(ts, level, event string, fields []Field) {
	var b bytes.Buffer
	b.WriteByte('{')
	writeJSONPair(&b, true, "ts", ts)
	writeJSONPair(&b, false, "level", level)
	writeJSONPair(&b, false, "event", event)
	for _, f := range fields {
		writeJSONPair(&b, false, f.Key, f.Val)
	}
	b.WriteByte('}')
	b.WriteByte('\n')
	_, _ = l.w.Write(b.Bytes())
}

func writeJSONPair(b *bytes.Buffer, first bool, key, val string) {
	if !first {
		b.WriteByte(',')
	}
	kb, _ := json.Marshal(key)
	vb, _ := json.Marshal(val)
	b.Write(kb)
	b.WriteByte(':')
	b.Write(vb)
}

// emitText writes a human-readable logfmt line: "<ts> <LEVEL> <event> k=v ...".
func (l *Logger) emitText(ts, level, event string, fields []Field) {
	var b strings.Builder
	b.WriteString(ts)
	b.WriteByte(' ')
	b.WriteString(strings.ToUpper(level))
	b.WriteByte(' ')
	b.WriteString(event)
	for _, f := range fields {
		b.WriteByte(' ')
		b.WriteString(f.Key)
		b.WriteByte('=')
		b.WriteString(logfmtValue(f.Val))
	}
	b.WriteByte('\n')
	_, _ = io.WriteString(l.w, b.String())
}

// logfmtValue quotes a value when it contains whitespace, an equals sign, a
// quote, or is empty, so each "k=v" token stays unambiguous.
func logfmtValue(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, " \t\r\n\"=") {
		return fmt.Sprintf("%q", v)
	}
	return v
}
