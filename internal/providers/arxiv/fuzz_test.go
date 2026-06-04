package arxiv

import (
	"testing"
	"unicode/utf8"
)

// FuzzParseAtom exercises the Atom parse + result mapping against arbitrary bytes.
//
// Invariants are restricted to the pipeline's TRUE post-conditions:
//   - never panics (parseAtom returns an error on malformed XML; resultsFromFeed
//     never indexes out of range and always returns a non-nil slice)
//   - deterministic (same input -> same parse result and same mapped results)
//   - on a parse SUCCESS, every emitted field preserves UTF-8 validity (valid in
//     -> valid out) and every result URL/title/snippet is well-formed text
//
// We do NOT assert "no error" — most random bytes are not valid XML. We assert the
// shape and safety guarantees of whatever IS produced.
func FuzzParseAtom(f *testing.F) {
	seeds := []string{
		fixtureAtom,
		errorEntryAtom,
		emptyAtom,
		"",
		"<feed></feed>",
		"<feed><entry><id>http://arxiv.org/abs/1v1</id><title>t</title><summary>s</summary></entry></feed>",
		"<feed><entry><id>http://arxiv.org/api/errors#x</id></entry></feed>",
		"<feed><entry><id>no-abs-here</id></entry></feed>",
		"<not-a-feed/>",
		"<feed><entry><id>http://arxiv.org/abs/x</id><summary>  a &amp; b 1D-&gt;2D $\\alpha$  </summary></entry></feed>",
		"<<<>>>",
		"<feed><entry><id>http://arxiv.org/abs/x</id><link rel=\"alternate\" type=\"text/html\" href=\"https://arxiv.org/abs/x\"/></entry></feed>",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		feed, err := parseAtom([]byte(s)) // must never panic
		feed2, err2 := parseAtom([]byte(s))
		if (err == nil) != (err2 == nil) {
			t.Fatalf("parseAtom determinism (err): %v vs %v for %q", err, err2, s)
		}
		if err != nil {
			return // malformed XML — nothing more to assert
		}

		results := resultsFromFeed(feed, 0)
		results2 := resultsFromFeed(feed2, 0)
		if results == nil {
			t.Fatalf("resultsFromFeed must return a non-nil slice for %q", s)
		}
		if len(results) != len(results2) {
			t.Fatalf("resultsFromFeed not deterministic for %q: %d vs %d", s, len(results), len(results2))
		}

		validInput := utf8.ValidString(s)
		for i, r := range results {
			if r.Provider != "arxiv" {
				t.Fatalf("result %d provider = %q, want arxiv", i, r.Provider)
			}
			if r.Score != nil {
				t.Fatalf("result %d fabricated a Score: %v", i, *r.Score)
			}
			if r.Title != results2[i].Title || r.URL != results2[i].URL || r.Snippet != results2[i].Snippet {
				t.Fatalf("result %d not deterministic for %q", i, s)
			}
			if validInput {
				if !utf8.ValidString(r.Title) || !utf8.ValidString(r.Snippet) || !utf8.ValidString(r.URL) {
					t.Fatalf("valid UTF-8 input %q produced invalid output in result %d: %#v", s, i, r)
				}
			}
		}
	})
}
