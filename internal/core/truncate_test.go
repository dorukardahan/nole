package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		if got := TruncateRunes("hello", 300); got != "hello" {
			t.Fatalf("got %q, want unchanged", got)
		}
	})
	t.Run("exactly max unchanged", func(t *testing.T) {
		s := strings.Repeat("a", 300)
		if got := TruncateRunes(s, 300); got != s {
			t.Fatalf("string of exactly max runes should be unchanged")
		}
	})
	t.Run("ascii truncates with ellipsis", func(t *testing.T) {
		s := strings.Repeat("a", 305)
		got := TruncateRunes(s, 300)
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("expected ellipsis suffix, got %q", got[len(got)-5:])
		}
		if n := utf8.RuneCountInString(strings.TrimSuffix(got, "...")); n != 300 {
			t.Fatalf("truncated body = %d runes, want 300", n)
		}
	})
	t.Run("multibyte truncation stays valid UTF-8", func(t *testing.T) {
		// 305 three-byte runes: a byte-slice [:300] would cut mid-rune and
		// produce invalid UTF-8 (mojibake). TruncateRunes must not.
		s := strings.Repeat("日", 305)
		got := TruncateRunes(s, 300)
		if !utf8.ValidString(got) {
			t.Fatalf("result is not valid UTF-8: %q", got)
		}
		body := strings.TrimSuffix(got, "...")
		if n := utf8.RuneCountInString(body); n != 300 {
			t.Fatalf("truncated body = %d runes, want 300", n)
		}
	})
	t.Run("negative max treated as zero", func(t *testing.T) {
		if got := TruncateRunes("abc", -5); got != "..." {
			t.Fatalf("got %q, want \"...\"", got)
		}
	})
}
