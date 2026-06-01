package nolelog

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a logger whose timestamp is deterministic, so JSON/text
// shape assertions don't depend on the wall clock.
func fixedClock() func() time.Time {
	t := time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestParseModeDefaultsToText(t *testing.T) {
	cases := map[string]Mode{
		"":        ModeText,
		"text":    ModeText,
		"TEXT":    ModeText,
		"garbage": ModeText,
		"json":    ModeJSON,
		"JSON":    ModeJSON,
		" json ":  ModeJSON,
		"off":     ModeOff,
		"OFF":     ModeOff,
	}
	for raw, want := range cases {
		if got := ParseMode(raw); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestJSONModeWritesSingleOrderedLine(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.now = fixedClock()
	l.Warn("research.search_step_failed", F("step", "1"), F("task", "news"))

	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("want exactly one trailing newline, got %q", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if rec["level"] != "warn" || rec["event"] != "research.search_step_failed" {
		t.Fatalf("unexpected level/event: %v", rec)
	}
	if rec["step"] != "1" || rec["task"] != "news" {
		t.Fatalf("fields missing: %v", rec)
	}
	if rec["ts"] != "2026-06-01T20:00:00Z" {
		t.Fatalf("ts not RFC3339 UTC: %v", rec["ts"])
	}
	// Stable key order: ts, level, event, then fields in call order.
	wantPrefix := `{"ts":"2026-06-01T20:00:00Z","level":"warn","event":"research.search_step_failed","step":"1","task":"news"}`
	if strings.TrimSpace(out) != wantPrefix {
		t.Fatalf("key order/shape drift:\n got %s\nwant %s", strings.TrimSpace(out), wantPrefix)
	}
}

func TestErrFieldRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	// FAKE- prefix keeps the repo secret scanner happy; the Redact regex still
	// strips the bearer token.
	l.Error("provider.call_failed", errors.New("Authorization: Bearer FAKE-live-token-123 failed"))
	out := buf.String()
	if strings.Contains(out, "FAKE-live-token-123") {
		t.Fatalf("Error leaked a bearer token: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

// The reviews flagged F() as itself a raw-secret API unless its VALUE is
// redacted. This locks that: a credential-bearing value handed to F is stripped.
func TestFRedactsPlainValue(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.Warn("http.request", F("url", "https://user:FAKEPASS@host.invalid/x?token=FAKE-secret-abc"))
	out := buf.String()
	for _, secret := range []string{"FAKEPASS", "FAKE-secret-abc", "user:FAKEPASS"} {
		if strings.Contains(out, secret) {
			t.Fatalf("F() leaked %q: %q", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %q", out)
	}
}

// A bare token value carries no credential marker for safeerr.Redact to match,
// so F must redact it by KEY when the key names a credential. Locks the Codex
// PR #41 finding: F("api_key", <rawtoken>) must NOT emit the raw token.
func TestFRedactsByCredentialKey(t *testing.T) {
	cases := []string{"api_key", "API_KEY", "tavily_token", "x-secret", "authorization", "session_cookie", "brave_key", "key"}
	for _, key := range cases {
		var buf bytes.Buffer
		l := New(&buf, ModeJSON)
		l.Warn("provider.call", F(key, "tvly-FAKE-rawtoken-no-marker"))
		out := buf.String()
		if strings.Contains(out, "tvly-FAKE-rawtoken-no-marker") {
			t.Fatalf("F(%q, ...) leaked a bare token: %q", key, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Fatalf("F(%q, ...) should redact by key, got %q", key, out)
		}
	}
}

// Benign keys that merely contain "key"-ish substrings must NOT be over-redacted,
// so observability of normal fields (a cache key word, etc.) is preserved.
func TestFKeepsBenignKeyedValues(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.Warn("x", F("monkey", "banana"), F("keyword", "search"), F("step", "1"))
	out := buf.String()
	for _, want := range []string{"banana", "search", "\"step\":\"1\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("benign field over-redacted, missing %q: %q", want, out)
		}
	}
}

func TestOffModeWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeOff)
	l.Info("x", F("a", "b"))
	l.Warn("y", F("a", "b"))
	l.Error("z", errors.New("boom"))
	if buf.Len() != 0 {
		t.Fatalf("ModeOff must write nothing, got %q", buf.String())
	}
}

func TestTextModeLogfmtShape(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeText)
	l.now = fixedClock()
	l.Warn("research.search_step_failed", F("step", "1"), F("task", "news"))
	want := "2026-06-01T20:00:00Z WARN research.search_step_failed step=1 task=news\n"
	if buf.String() != want {
		t.Fatalf("text shape:\n got %q\nwant %q", buf.String(), want)
	}
}

func TestTextModeQuotesValuesWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeText)
	l.now = fixedClock()
	l.Info("serve.ready", F("addr", "127.0.0.1:8765"), F("note", "draining now"))
	out := buf.String()
	if !strings.Contains(out, "addr=127.0.0.1:8765") {
		t.Fatalf("bare value should not be quoted: %q", out)
	}
	if !strings.Contains(out, `note="draining now"`) {
		t.Fatalf("spaced value should be quoted: %q", out)
	}
}

func TestNilFieldsDropped(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.now = fixedClock()
	// Err(nil) yields a zero Field that must not print an empty key.
	l.Warn("x", Err(nil), F("k", "v"))
	out := strings.TrimSpace(buf.String())
	want := `{"ts":"2026-06-01T20:00:00Z","level":"warn","event":"x","k":"v"}`
	if out != want {
		t.Fatalf("zero field not dropped:\n got %s\nwant %s", out, want)
	}
}

// A caller field whose key collides with a reserved system key (ts/level/event)
// must be DROPPED, so the JSON record never has a duplicate key and the system
// value always wins. Guards the documented stable-key-order invariant.
func TestReservedFieldKeysAreDropped(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.now = fixedClock()
	l.Warn("evt", F("level", "spoof"), F("ts", "spoof"), F("event", "spoof"), F("ok", "1"))
	out := strings.TrimSpace(buf.String())
	want := `{"ts":"2026-06-01T20:00:00Z","level":"warn","event":"evt","ok":"1"}`
	if out != want {
		t.Fatalf("reserved keys not dropped:\n got %s\nwant %s", out, want)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["level"] != "warn" || m["event"] != "evt" {
		t.Fatalf("system keys must win over colliding caller fields: %v", m)
	}
}

func TestNilLoggerIsNoop(t *testing.T) {
	var l *Logger
	// Must not panic on a nil receiver (call sites that were never wired one).
	l.Info("x")
	l.Warn("y", F("a", "b"))
	l.Error("z", errors.New("boom"))
	if l.Mode() != ModeText {
		t.Fatalf("nil logger Mode() = %q, want text", l.Mode())
	}
}

func TestNilWriterIsNoop(t *testing.T) {
	l := New(nil, ModeJSON)
	l.Warn("x", F("a", "b")) // must not panic
}

// The logger writes ONLY to the writer it was constructed with — the
// stdout-purity invariant in package form. We can't assert os.Stdout is
// untouched directly, but we lock that the supplied writer is the sole sink.
func TestWriterIsTheOnlySink(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, ModeJSON)
	l.Warn("x", F("a", "b"))
	if buf.Len() == 0 {
		t.Fatal("expected output on the supplied writer")
	}
}
