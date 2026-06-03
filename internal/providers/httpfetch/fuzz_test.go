package httpfetch

import (
	"testing"
	"unicode/utf8"
)

// FuzzHTMLToText exercises the HTML-to-text extractor against arbitrary bytes.
//
// Invariants are restricted to the function's TRUE post-conditions. Like
// ddgs.cleanHTML and wikipedia.stripSearchMatch, htmlToText strips tags BEFORE
// decoding entities, so "&lt;b&gt;" legitimately becomes "<b>" — the output may
// contain tag-shaped substrings and is NOT idempotent. Asserting "no tags" or
// idempotence would be wrong. What holds:
//   - never panics
//   - deterministic (same input -> same output, for both returns)
//   - preserves UTF-8 validity (valid in -> valid out, text and title)
func FuzzHTMLToText(f *testing.F) {
	seeds := []string{
		"plain text",
		"<p>hello</p>",
		"<title>T</title><body>b</body>",
		"<script>alert(1)</script>visible",
		"<script>unclosed leak",
		"a &amp; b &#039;c&#039; &quot;d&quot;",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"<div><div><p>nested</p></div></div>",
		"broken <p class=",
		"multi   spaces\n\n\n\ntabs\t\there",
		"&nbsp;&nbsp;trim&nbsp;",
		"<!-- comment --><p>after</p>",
		"<style>.x{}</style><svg><text>icon</text></svg>",
		"",
		"<<<>>>",
		"&amp;amp;",
		"<HEAD><TITLE>caps</TITLE></HEAD><BODY>x</BODY>",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		text, title := htmlToText([]byte(s)) // must never panic

		text2, title2 := htmlToText([]byte(s))
		if text != text2 || title != title2 {
			t.Fatalf("htmlToText not deterministic for %q: (%q,%q) vs (%q,%q)", s, text, title, text2, title2)
		}

		if utf8.ValidString(s) {
			if !utf8.ValidString(text) {
				t.Fatalf("valid UTF-8 input %q produced invalid text output %q", s, text)
			}
			if !utf8.ValidString(title) {
				t.Fatalf("valid UTF-8 input %q produced invalid title output %q", s, title)
			}
		}
	})
}
