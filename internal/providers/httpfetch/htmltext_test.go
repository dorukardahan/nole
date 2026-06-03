package httpfetch

import (
	"strings"
	"testing"
)

func TestHTMLToTextExtractsVisibleContent(t *testing.T) {
	in := []byte(`<!DOCTYPE html>
<html>
<head>
  <title>Example Page</title>
  <style>.x{color:red}</style>
  <script>var leak = "SECRET_SCRIPT_TOKEN";</script>
</head>
<body>
  <h1>Heading</h1>
  <p>First paragraph with O&#039;Brien &amp; &quot;quotes&quot;.</p>
  <div>Second block.</div>
  <noscript>noscript-noise</noscript>
</body>
</html>`)
	text, title := htmlToText(in)

	if title != "Example Page" {
		t.Errorf("title = %q, want %q", title, "Example Page")
	}
	// Script/style/noscript bodies must be gone.
	for _, leak := range []string{"SECRET_SCRIPT_TOKEN", "color:red", "noscript-noise", "var leak"} {
		if strings.Contains(text, leak) {
			t.Errorf("text leaked non-visible content %q: %q", leak, text)
		}
	}
	// Entities decoded.
	if !strings.Contains(text, `O'Brien & "quotes"`) {
		t.Errorf("entities not decoded: %q", text)
	}
	// Visible block text present.
	for _, want := range []string{"Heading", "First paragraph", "Second block."} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing visible content %q: %q", want, text)
		}
	}
	// Block tags separate content (Heading not glued to the paragraph).
	if strings.Contains(text, "HeadingFirst") {
		t.Errorf("block tags did not separate content: %q", text)
	}
}

func TestHTMLToTextRemovesUnclosedScript(t *testing.T) {
	// A <script> with no closing tag must not leak its body as text.
	in := []byte(`<p>visible</p><script>alert("UNCLOSED_LEAK")`)
	text, _ := htmlToText(in)
	if strings.Contains(text, "UNCLOSED_LEAK") {
		t.Errorf("unclosed script body leaked: %q", text)
	}
	if !strings.Contains(text, "visible") {
		t.Errorf("visible text dropped: %q", text)
	}
}

func TestHTMLToTextRemovesUnclosedTitle(t *testing.T) {
	// Per the HTML spec, <title> is RCDATA: an unclosed <title> runs to EOF and a
	// later <body> start tag does NOT close it. So a truncated/malformed page must
	// not leak the title's RCDATA content into the extracted body text.
	in := []byte(`<title>UNCLOSED_TITLE_LEAK<body>visible-but-actually-title-rcdata`)
	text, _ := htmlToText(in)
	if strings.Contains(text, "UNCLOSED_TITLE_LEAK") {
		t.Errorf("unclosed title RCDATA leaked into body: %q", text)
	}
}

func TestHTMLToTextCollapsesWhitespace(t *testing.T) {
	in := []byte("<p>a   \t  b</p>\n\n\n\n<p>c</p>")
	text, _ := htmlToText(in)
	if strings.Contains(text, "a   ") || strings.Contains(text, "\t") {
		t.Errorf("intra-line whitespace not collapsed: %q", text)
	}
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("blank-line runs not collapsed: %q", text)
	}
	if strings.TrimSpace(text) != text {
		t.Errorf("output not trimmed: %q", text)
	}
}

func TestHTMLToTextPlainTextPassthrough(t *testing.T) {
	in := []byte("just plain text, no markup")
	text, title := htmlToText(in)
	if text != "just plain text, no markup" {
		t.Errorf("plain text changed: %q", text)
	}
	if title != "" {
		t.Errorf("title should be empty for title-less input, got %q", title)
	}
}

func TestHTMLToTextEmptyAndScriptOnly(t *testing.T) {
	cases := [][]byte{
		[]byte(""),
		[]byte("   \n\t  "),
		[]byte("<script>only()</script><style>.a{}</style>"),
		[]byte("<html><head></head><body></body></html>"),
	}
	for _, c := range cases {
		text, _ := htmlToText(c)
		if strings.TrimSpace(text) != "" {
			t.Errorf("htmlToText(%q) = %q, want empty", c, text)
		}
	}
}

func TestHTMLToTextTitleEntitiesDecoded(t *testing.T) {
	in := []byte(`<title>Tom &amp; Jerry&#039;s Show</title><p>body</p>`)
	_, title := htmlToText(in)
	if title != `Tom & Jerry's Show` {
		t.Errorf("title entities not decoded: %q", title)
	}
}
