package core

import (
	"fmt"
	"sort"
	"strings"
)

// BuildSetupSuggestions inspects the set of configured BYOK providers and
// returns one suggestion per missing key. Suggestions are returned sorted by
// (impact desc, missing key asc) so the most actionable items come first.
//
// `configured` is keyed by provider name (e.g. "brave"). DDGS is keyless and
// is never in this map.
func BuildSetupSuggestions(configured map[string]bool) []SetupSuggestion {
	providers := BYOKProviders()
	hasExtractCapable := false
	for _, p := range providers {
		if p.SupportsExtract && configured[p.Name] {
			hasExtractCapable = true
			break
		}
	}

	out := []SetupSuggestion{}
	for _, p := range providers {
		if configured[p.Name] {
			continue
		}
		impact := classifyImpact(p, hasExtractCapable)
		out = append(out, SetupSuggestion{
			MissingKey:        p.EnvVars[0], // primary env var name
			Impact:            impact,
			Unlocks:           append([]string(nil), p.Unlocks...),
			CurrentWorkaround: currentWorkaroundFor(p, hasExtractCapable),
			FreeTier:          p.FreeTierNote,
			SignupURL:         p.SignupURL,
			EnvExample:        p.EnvExample,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := impactRank(out[i].Impact), impactRank(out[j].Impact)
		if ri != rj {
			return ri < rj // lower rank means higher priority
		}
		return out[i].MissingKey < out[j].MissingKey
	})
	return out
}

func classifyImpact(p BYOKProvider, hasExtractCapable bool) string {
	// HIGH: the provider unlocks a capability that no currently-configured
	// provider can deliver. The only such case today is url_extraction when
	// no extract-capable provider has a key.
	if p.SupportsExtract && !hasExtractCapable {
		return "high"
	}
	// MEDIUM: the provider materially improves a feature that already works
	// — Brave gives faster search than DDGS, Tavily adds semantic-quality
	// even when extract is already covered by Firecrawl.
	if p.Name == "brave" {
		return "medium"
	}
	if p.Name == "tavily" && hasExtractCapable {
		// Extract is covered, but Tavily still adds semantic/people quality.
		return "medium"
	}
	// LOW: the provider only adds redundancy. Today this is Firecrawl when
	// Tavily already covers extract.
	return "low"
}

func impactRank(impact string) int {
	switch impact {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	}
	return 3
}

func currentWorkaroundFor(p BYOKProvider, hasExtractCapable bool) string {
	switch {
	case p.SupportsExtract && !hasExtractCapable:
		return "AI tool's built-in HTTP fetch (works, but no markdown conversion or paywall handling)"
	case p.Name == "brave":
		return "DDGS keyless fallback (slower, weaker on multilingual queries)"
	case p.Name == "tavily" && hasExtractCapable:
		return "Existing extract provider handles URLs; DDGS handles semantic queries (lower quality)"
	default:
		return "Existing providers cover the capability; this entry is redundancy only"
	}
}

// BuildSetupTip turns a slice of suggestions into the once-per-session
// summary embedded in the first SearchResponse of an MCP connection. Returns
// nil when nothing is missing — the SearchResponse omits the field entirely.
// Also returns nil when the only missing keys are LOW impact (pure redundancy
// is not worth nagging about on every session).
func BuildSetupTip(suggestions []SetupSuggestion) *SetupTip {
	if len(suggestions) == 0 {
		return nil
	}
	high := []string{}
	medium := []string{}
	for _, s := range suggestions {
		switch s.Impact {
		case "high":
			high = append(high, s.MissingKey)
		case "medium":
			medium = append(medium, s.MissingKey)
		}
	}
	if len(high) == 0 && len(medium) == 0 {
		return nil
	}
	var b strings.Builder
	if len(high) > 0 {
		fmt.Fprintf(&b, "Some Nólë features are currently disabled. Set %s to enable them. ", joinOr(high))
	}
	if len(medium) > 0 {
		fmt.Fprintf(&b, "Configuring %s would also improve search quality. ", joinOr(medium))
	}
	b.WriteString("Until then your AI tool will use its own built-in fallbacks where needed.")
	return &SetupTip{
		Summary: strings.TrimSpace(b.String()),
		SeeAlso: "call provider_status for per-key signup links and env examples",
	}
}

func joinOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}
