package arxiv

import (
	"encoding/xml"
	"strings"

	"github.com/dorukardahan/nole/internal/core"
)

// atomFeed is the subset of the arXiv Atom (RFC 4287) query response Nólë reads.
//
// Only ENTRY-scoped fields are modeled. The feed-level <id> (a /api/{token}
// URL), feed <title> (the "arXiv Query: ..." echo) and feed <link> are
// deliberately NOT bound here: Go's encoding/xml matches by local name, so a flat
// struct with an `xml:"id"` field would bind the feed-level id and we would then
// drop every real result (its id "lacks /abs/"). Nesting the entries makes each
// <id>/<title>/<summary>/<link> bind inside its parent <entry>, eliminating that
// collision. encoding/xml also decodes XML entities automatically (&amp; -> &),
// so fields come back as literal characters — never double-unescape them.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Summary   string     `xml:"summary"` // the abstract
	Published string     `xml:"published"`
	Links     []atomLink `xml:"link"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

// parseAtom unmarshals an arXiv Atom feed body. xml.Unmarshal never panics on
// malformed input — it returns an error, which callers treat as a decode failure
// (a contract mismatch, not an upstream outage).
func parseAtom(body []byte) (atomFeed, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return atomFeed{}, err
	}
	return feed, nil
}

// resultsFromFeed maps real paper entries to SearchResults, skipping arXiv
// application-level error entries. arXiv reports a malformed query as an HTTP 200
// feed whose single <entry> has an id under the http://arxiv.org/api/errors
// namespace; an empty result set is a 200 feed with zero entries. Both yield an
// empty (non-nil) slice here, which the service treats as a fall-through to the
// next academic provider (wikipedia -> ddgs) — never an error, never a tripped
// breaker.
//
// Score stays nil (arXiv exposes no numeric relevance score and Nólë never
// fabricates one). PublishedAt is the <published> value verbatim for the agent to
// judge recency. limit (> 0) caps the result count. The returned slice is always
// non-nil.
func resultsFromFeed(feed atomFeed, limit int) []core.SearchResult {
	results := make([]core.SearchResult, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		if !isAbsID(e.ID) {
			continue // error entry (/api/errors) or any non-abstract id — never a result
		}
		results = append(results, core.SearchResult{
			Title:    collapseWS(e.Title),
			URL:      absPageURL(e),
			Snippet:  core.TruncateRunes(collapseWS(e.Summary), 300),
			Provider: "arxiv",
			// Score stays nil: arXiv exposes no relevance score and Nólë never
			// fabricates one. PublishedAt = first-version submit time, verbatim.
			PublishedAt: strings.TrimSpace(e.Published),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

// isAbsID reports whether an entry <id> is a real arXiv abstract identifier
// (http://arxiv.org/abs/<id>vN). The "/abs/" check is the load-bearing guard (an
// arXiv error-entry id is http://arxiv.org/api/errors#... and has no /abs/); the
// explicit "/api/errors" exclusion is belt-and-suspenders for that documented
// error namespace.
func isAbsID(id string) bool {
	id = strings.TrimSpace(id)
	return strings.Contains(id, "/abs/") && !strings.Contains(id, "/api/errors")
}

// absPageURL returns the canonical https abstract-page URL for an entry. It
// prefers the <link rel="alternate" type="text/html"> href arXiv itself publishes
// (already https in the live API), and falls back to deriving https from the entry
// <id> (whose scheme is literally http:// by arXiv convention) only when that link
// is absent. Pure pass-through: no fabricated or normalized field.
func absPageURL(e atomEntry) string {
	for _, l := range e.Links {
		if strings.EqualFold(l.Rel, "alternate") && strings.EqualFold(l.Type, "text/html") {
			if href := strings.TrimSpace(l.Href); href != "" {
				return href
			}
		}
	}
	// Fallback: the entry id is http://arxiv.org/abs/<id>vN; serve it over https.
	// The id scheme is arXiv's identifier convention, not a transport signal.
	id := strings.TrimSpace(e.ID)
	if strings.HasPrefix(id, "http://") {
		return "https://" + strings.TrimPrefix(id, "http://")
	}
	return id
}

// collapseWS trims and collapses every run of whitespace to a single space. arXiv
// wraps <title> and <summary> across lines with indentation (the abstract even
// carries a leading two-space indent), and encoding/xml does not trim chardata, so
// a bare TrimSpace would leave internal newlines. We deliberately do NOT
// html-unescape: encoding/xml already decoded the XML entities, and the text can
// carry literal LaTeX ($\alpha$, ^{-1}) and < > that Nólë passes through verbatim
// (a dumb gateway never strips, sanitizes, or judges content).
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
