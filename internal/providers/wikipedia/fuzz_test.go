package wikipedia

import (
	"testing"
	"unicode/utf8"
)

// FuzzStripSearchMatch exercises the MediaWiki snippet sanitizer against
// arbitrary input.
//
// Invariants are restricted to the function's TRUE post-conditions. Like
// ddgs.cleanHTML, stripSearchMatch strips <...> tags BEFORE decoding entities,
// so "&lt;script&gt;" legitimately becomes "<script>" — the output may contain
// tag-shaped substrings and is not idempotent. Asserting "no tags in output" or
// idempotence would be wrong. What holds:
//   - never panics
//   - deterministic (same input -> same output)
//   - preserves UTF-8 validity (valid in -> valid out)
func FuzzStripSearchMatch(f *testing.F) {
	seeds := []string{
		"plain text",
		`<span class="searchmatch">term</span>`,
		"a &amp; b",
		"It&#39;s &quot;quoted&quot; &lt;ok&gt;",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"nested <span><span>x</span></span>",
		"broken <span class=",
		"multi   spaces\n\ttabs",
		"&nbsp;&nbsp;trim&nbsp;",
		"",
		"<<<>>>",
		"&amp;amp;",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := stripSearchMatch(s) // must never panic

		if got := stripSearchMatch(s); got != out {
			t.Fatalf("stripSearchMatch not deterministic: %q vs %q for input %q", out, got, s)
		}

		if utf8.ValidString(s) && !utf8.ValidString(out) {
			t.Fatalf("stripSearchMatch turned valid UTF-8 input %q into invalid output %q", s, out)
		}
	})
}
