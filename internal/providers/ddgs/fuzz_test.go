package ddgs

import (
	"testing"
	"unicode/utf8"
)

// FuzzCleanHTML exercises the DDG HTML title/snippet sanitizer (which includes
// the v0.3.1 &amp; entity normalization) against arbitrary input.
//
// Invariants are restricted to the function's TRUE post-conditions. Note what
// is deliberately NOT asserted: cleanHTML strips <...> tags BEFORE decoding
// &lt;/&gt;, so "&lt;script&gt;" legitimately becomes "<script>" — i.e. the
// output may contain tag-shaped substrings and is not idempotent. Asserting
// "no tags in output" or idempotence would be wrong. What holds:
//   - never panics
//   - deterministic (same input -> same output)
//   - preserves UTF-8 validity (valid in -> valid out)
func FuzzCleanHTML(f *testing.F) {
	seeds := []string{
		"plain text",
		"a &amp; b",
		"<b>bold</b>",
		"It&#39;s &quot;quoted&quot;",
		"&lt;script&gt;",
		"multi   spaces\nnewline",
		`<a href="?x=1&amp;y=2">link</a>`,
		"&amp;amp;",
		"",
		"<<<>>>",
		"&",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := cleanHTML(s) // must never panic

		if got := cleanHTML(s); got != out {
			t.Fatalf("cleanHTML not deterministic: %q vs %q for input %q", out, got, s)
		}

		if utf8.ValidString(s) && !utf8.ValidString(out) {
			t.Fatalf("cleanHTML turned valid UTF-8 input %q into invalid output %q", s, out)
		}
	})
}
