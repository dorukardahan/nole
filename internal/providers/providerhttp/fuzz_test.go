package providerhttp

import (
	"bytes"
	"testing"
)

// FuzzDecodeJSONLimited exercises the bounded body readers against truncated,
// garbage, and adversarially-sized input. The corpus stays cheap: bodies are
// capped at 64 KiB and max is normalized into a sane range so the engine never
// allocates a multi-MiB buffer or exercises the max+1 overflow (out of scope).
//
// Invariants are the OOM-safety contract, not the parse result:
//   - neither reader ever panics, for any (body, max) including negative max
//   - ReadAllLimited returns at most clamp(max, 0, ..) bytes (the memory bound)
//
// We deliberately do NOT assert that DecodeJSONLimited's error omits body bytes:
// the underlying json.Decoder legitimately echoes the offending character
// (e.g. `invalid character 'q'`). Only the dedicated "too_large" path is
// redaction-safe, and that is covered by readbody_test.go.
func FuzzDecodeJSONLimited(f *testing.F) {
	type seed struct {
		body []byte
		max  int64
	}
	seeds := []seed{
		{[]byte(`{"query":"x","web":{"results":[]}}`), 1 << 16},
		{[]byte(`{"results":[{"title":"t","url":"u"}]}`), 64},
		{[]byte(`{"truncated":`), 8},
		{[]byte(`not json at all`), 16},
		{[]byte(``), 0},
		{[]byte(`[1,2,3]`), 2},
		{[]byte(`{}`), -5},
		{bytes.Repeat([]byte("a"), 1024), 10},
	}
	for _, s := range seeds {
		f.Add(s.body, s.max)
	}

	f.Fuzz(func(t *testing.T, body []byte, max int64) {
		// Keep the corpus cheap and away from the max+1 overflow edge.
		if len(body) > 1<<16 {
			return
		}
		if max > 1<<20 {
			max = 1 << 20
		}
		if max < -16 {
			max = -16
		}

		out, _ := ReadAllLimited(bytes.NewReader(body), max) // must never panic
		clamped := max
		if clamped < 0 {
			clamped = 0
		}
		if int64(len(out)) > clamped {
			t.Fatalf("ReadAllLimited returned %d bytes, exceeds clamp(max)=%d", len(out), clamped)
		}

		var v any
		_ = DecodeJSONLimited(bytes.NewReader(body), max, &v) // must never panic
	})
}
