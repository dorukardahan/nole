package httpfetch

import (
	"html"
	"regexp"
	"strings"
)

// htmlToText converts an HTML document to readable plain text and extracts the
// document title. It is a best-effort, dependency-free extractor built on the
// stdlib (regexp + html.UnescapeString), matching the existing Nólë HTML helpers
// (ddgs.cleanHTML, wikipedia.stripSearchMatch) rather than pulling in an HTML
// tokenizer dependency. It is the algorithm behind the keyless httpfetch
// last-resort extract backstop: it does NOT execute JavaScript, so it is weaker
// than Scrapling/Firecrawl on SPA/JS-rendered pages — an honest, accepted limit.
//
// It ports the Scrapling fallback TextExtractor (internal/providers/scrapling
// extractScript): drop the bodies of non-visible elements (script/style/etc),
// turn block-level tags into newlines so adjacent blocks don't glue together,
// strip the remaining tags, decode entities, then collapse whitespace.
//
// Like every Nólë primitive it never judges quality and never panics. It is
// deliberately NOT idempotent (tags are stripped before entities are decoded, so
// "&lt;b&gt;" becomes "<b>"), and it preserves UTF-8 validity — the fuzz target
// pins those invariants.
var (
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// Title capture tolerates an UNCLOSED <title> (match to end-of-input): per the
	// HTML spec <title> is RCDATA that ends only at </title> or EOF — a later
	// <body> start tag does NOT close it. The non-capturing (?:...|$) keeps group 1
	// = the title content.
	reTitle = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)(?:</title\s*>|$)`)

	// Elements whose ENTIRE body is non-visible (or, for <title>, separately
	// captured) content. All tolerate an UNCLOSED tag (match to end-of-input) so a
	// truncated or hostile page cannot leak e.g. raw JavaScript — or, for <title>,
	// RCDATA title text — into the body output. <title> is a leaf element, so the
	// end-of-input fallback is browser-faithful and cannot swallow a real body the
	// way an unclosed <head> could.
	reSkipUnclosed = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?(</script\s*>|$)`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?(</style\s*>|$)`),
		regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?(</noscript\s*>|$)`),
		regexp.MustCompile(`(?is)<template\b[^>]*>.*?(</template\s*>|$)`),
		regexp.MustCompile(`(?is)<svg\b[^>]*>.*?(</svg\s*>|$)`),
		regexp.MustCompile(`(?is)<title\b[^>]*>.*?(</title\s*>|$)`),
	}
	// <head> is matched CLOSED-ONLY: an unclosed <head> must not swallow the whole
	// document body (browsers auto-close head at <body>).
	reSkipClosed = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<head\b[^>]*>.*?</head\s*>`),
	}

	// Block-level tags (open or close) become a newline so adjacent blocks stay
	// separated. Mirrors the Scrapling TextExtractor block set (plus a few common
	// table/list/preformatted tags).
	reBlockTag = regexp.MustCompile(`(?i)</?(p|div|section|article|header|footer|main|br|li|tr|td|th|h[1-6]|ul|ol|dl|dt|dd|table|blockquote|pre|hr)\b[^>]*>`)
	reAnyTag   = regexp.MustCompile(`<[^>]*>`)

	// Whitespace normalisation, ported from the Scrapling extractor: collapse
	// intra-line whitespace to a single space, then collapse runs of blank lines
	// to one blank line. \n is intentionally excluded from the first class so
	// block separation survives into the blank-line pass.
	reInlineWS = regexp.MustCompile(`[ \t\r\f\v]+`)
	reBlankRun = regexp.MustCompile(`\n\s*\n+`)
)

func htmlToText(body []byte) (text string, title string) {
	s := string(body)

	// Capture the title (decoded, single-line) before any element removal.
	if m := reTitle.FindStringSubmatch(s); m != nil {
		title = strings.Join(strings.Fields(html.UnescapeString(reAnyTag.ReplaceAllString(m[1], ""))), " ")
	}

	s = reComment.ReplaceAllString(s, "")
	for _, re := range reSkipUnclosed {
		s = re.ReplaceAllString(s, "")
	}
	for _, re := range reSkipClosed {
		s = re.ReplaceAllString(s, "")
	}
	s = reBlockTag.ReplaceAllString(s, "\n")
	s = reAnyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	s = reInlineWS.ReplaceAllString(s, " ")
	s = reBlankRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s), title
}
