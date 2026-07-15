package httpfetch

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
	xhtml "golang.org/x/net/html"
)

// htmlToText converts an HTML document to readable plain text and extracts the
// document title. It uses Go's pure-Go HTML5 parser so malformed markup, nested
// same-name elements, arbitrary custom elements and quoted '>' characters obey a
// real DOM boundary rather than a regex approximation. It does not execute
// JavaScript and remains weaker than Scrapling/Firecrawl on SPA/JS-rendered pages.
//
// Non-visible element bodies and explicitly hidden DOM subtrees are excluded,
// block-level tags become newlines, entities are decoded by the parser, and
// whitespace is collapsed. Like every Nólë primitive it never judges content
// quality and never panics.
var (
	reInlineWS = regexp.MustCompile(`[ \t\r\f\v]+`)
	reBlankRun = regexp.MustCompile(`\n\s*\n+`)

	blockHTMLTags = map[string]bool{
		"p": true, "div": true, "section": true, "article": true,
		"header": true, "footer": true, "main": true, "br": true,
		"li": true, "tr": true, "td": true, "th": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"ul": true, "ol": true, "dl": true, "dt": true, "dd": true,
		"table": true, "blockquote": true, "pre": true, "hr": true,
	}
)

func htmlToText(body []byte) (text string, title string) {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		// bytes.Reader cannot return an I/O error in normal operation. Fail
		// closed rather than falling back to a text path that could leak script
		// or hidden DOM bodies.
		return "", ""
	}
	title = firstElementText(doc, "title")

	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		switch node.Type {
		case xhtml.CommentNode:
			return
		case xhtml.TextNode:
			out.WriteString(node.Data)
			return
		case xhtml.ElementNode:
			tag := strings.ToLower(node.Data)
			if core.HTMLNodeHiddenKind(node) != "" {
				return
			}
			if tag == "br" || tag == "hr" {
				out.WriteByte('\n')
				return
			}
			if blockHTMLTags[tag] {
				out.WriteByte('\n')
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			if blockHTMLTags[tag] {
				out.WriteByte('\n')
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	text = reInlineWS.ReplaceAllString(out.String(), " ")
	text = reBlankRun.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text), title
}

func firstElementText(root *xhtml.Node, tag string) string {
	var found string
	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if found != "" {
			return
		}
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, tag) {
			var text strings.Builder
			appendNodeText(&text, node)
			found = strings.Join(strings.Fields(text.String()), " ")
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return found
}

func appendNodeText(out *strings.Builder, node *xhtml.Node) {
	if node.Type == xhtml.TextNode {
		out.WriteString(node.Data)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		appendNodeText(out, child)
	}
}
