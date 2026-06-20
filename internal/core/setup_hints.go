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
//
// `extractAvailable` reports whether URL extraction works AT ALL in the running
// service — true whenever any available extract provider is registered, including
// the always-on keyless httpfetch backstop. When true, a missing keyed extract
// provider is a FIDELITY upgrade (JS rendering, markdown, paywall handling), not a
// disabled-feature unlock: it is never classified "high", and its workaround names
// Nólë's own keyless backstop rather than the AI tool's built-in fetch.
func BuildSetupSuggestions(configured map[string]bool, extractAvailable bool) []SetupSuggestion {
	providers := BYOKProviders()
	hasKeyedExtract := false
	for _, p := range providers {
		if p.SupportsExtract && configured[p.Name] {
			hasKeyedExtract = true
			break
		}
	}

	out := []SetupSuggestion{}
	for _, p := range providers {
		if configured[p.Name] {
			continue
		}
		impact := classifyImpact(p, hasKeyedExtract, extractAvailable)
		out = append(out, SetupSuggestion{
			MissingKey:        p.EnvVars[0], // primary env var name
			Impact:            impact,
			Unlocks:           append([]string(nil), p.Unlocks...),
			CurrentWorkaround: currentWorkaroundFor(p, hasKeyedExtract, extractAvailable),
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

func classifyImpact(p BYOKProvider, hasKeyedExtract, extractAvailable bool) string {
	// HIGH: the provider unlocks a capability NOTHING currently delivers. The only
	// such case is url_extraction when extract is unavailable entirely — which does
	// not happen in the default service (the keyless httpfetch backstop makes
	// extractAvailable true), only in a build with no extract provider at all.
	if p.SupportsExtract && !extractAvailable {
		return "high"
	}
	// MEDIUM: the provider materially improves a feature that already works. A keyed
	// extract provider, when extract currently runs only on the keyless backstop
	// (no other keyed extract), is a FIDELITY upgrade (JS rendering, markdown,
	// paywall handling) — meaningful, not a disabled-feature unlock. Brave gives
	// faster search than DDGS; Tavily adds semantic/people quality on top of an
	// already-keyed extract provider.
	if p.SupportsExtract && !hasKeyedExtract {
		return "medium"
	}
	if p.Name == "brave" {
		return "medium"
	}
	if p.Name == "tavily" && hasKeyedExtract {
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

func currentWorkaroundFor(p BYOKProvider, hasKeyedExtract, extractAvailable bool) string {
	switch {
	case p.SupportsExtract && !extractAvailable:
		return "AI tool's built-in HTTP fetch (works, but no markdown conversion or paywall handling)"
	case p.SupportsExtract && !hasKeyedExtract:
		return "Nólë's keyless httpfetch extract backstop (works, but no JavaScript rendering, markdown conversion or paywall handling)"
	case p.Name == "brave":
		return "DDGS keyless fallback (slower, weaker on multilingual queries)"
	case p.Name == "tavily" && hasKeyedExtract:
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
		if len(high) == 0 {
			b.WriteString("Nólë already works with keyless fallbacks. ")
			fmt.Fprintf(&b, "Configuring %s can improve search speed or extract fidelity. ", joinOr(medium))
		} else {
			fmt.Fprintf(&b, "Configuring %s can also improve search speed or extract fidelity. ", joinOr(medium))
		}
	}
	if len(high) > 0 {
		b.WriteString("Until then, Nólë will use keyless/provider fallbacks where available.")
	}
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
