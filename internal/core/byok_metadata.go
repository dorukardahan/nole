package core

// BYOKProvider holds the metadata Nólë needs to (1) seed the local free-tier
// quota for a keyed provider, (2) classify it during cost decisions, and
// (3) tell the user what they'd unlock by configuring it. Adding a new BYOK
// provider means appending one entry here — the cli quota wiring, the setup
// hints builder, and the docs all consume from this slice.
type BYOKProvider struct {
	Name            string
	EnvVars         []string // primary first; the rest are accepted aliases
	FreeQuota       int
	RefreshWindow   RefreshWindow
	SupportsSearch  bool
	SupportsExtract bool
	SignupURL       string
	FreeTierNote    string
	EnvExample      string
	Unlocks         []string
	// MeteringModel / RateLimitNote / EstimateOnly are STATIC, build-time
	// descriptive metadata about how the provider's free tier actually meters.
	// They must never be computed from observed runtime behaviour (429s,
	// latency) — that would make Nólë "learn" provider health, which it must
	// not. They describe the provider's published model so budget_status can be
	// honest that FreeRemaining is Nólë's own issued-request estimate, not a
	// live dashboard balance.
	MeteringModel string // "call-count" | "credit-based" | "one-time-grant"
	RateLimitNote string
	EstimateOnly  bool
}

// byokProviders is the authoritative slice; never modify after package init.
var byokProviders = []BYOKProvider{
	{
		Name:            "brave",
		EnvVars:         []string{"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: false,
		SignupURL:       "https://api.search.brave.com",
		FreeTierNote:    "Free tier changed in Feb 2026: new accounts get ~1000 queries/month from a $5 monthly credit, then metered billing (card on file); legacy accounts keep 2000/month. Nólë caps at 1000/month as a fail-safe floor and counts its own calls.",
		EnvExample:      "export BRAVE_API_KEY=BSA...",
		Unlocks:         []string{"fast_general_search", "news_search_quality"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "~1000 queries/month estimate (legacy plans 2000) + a 1 req/sec rate cap Nólë does not track; beyond the free credit Brave bills the card on file.",
		EstimateOnly:    true,
	},
	{
		Name:            "tavily",
		EnvVars:         []string{"TAVILY_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://tavily.com",
		FreeTierNote:    "~1000 API credits/month, no credit card required.",
		EnvExample:      "export TAVILY_API_KEY=tvly-...",
		Unlocks:         []string{"url_extraction", "semantic_search_quality"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "credit-based: a search costs ~1 credit, an advanced extract ~2; Nólë debits 1 per call, so the local count can over-read remaining credits.",
		EstimateOnly:    true,
	},
	{
		Name:            "firecrawl",
		EnvVars:         []string{"FIRECRAWL_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://firecrawl.dev",
		FreeTierNote:    "~1000 credits/month per Firecrawl's pricing (some trackers list 500 one-time — verify your dashboard); no credit card required.",
		EnvExample:      "export FIRECRAWL_API_KEY=fc-...",
		Unlocks:         []string{"url_extraction"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "free tier is in flux between monthly and one-time across Firecrawl's own pages and third-party trackers; Nólë tracks 1000/month as an estimate — verify your dashboard.",
		EstimateOnly:    true,
	},
}

// BYOKProviders returns the configured BYOK provider metadata. The returned
// slice is a copy — mutating it does not affect the package-level data.
func BYOKProviders() []BYOKProvider {
	out := make([]BYOKProvider, len(byokProviders))
	for i, p := range byokProviders {
		ev := make([]string, len(p.EnvVars))
		copy(ev, p.EnvVars)
		ul := make([]string, len(p.Unlocks))
		copy(ul, p.Unlocks)
		p.EnvVars = ev
		p.Unlocks = ul
		out[i] = p
	}
	return out
}

// LookupBYOK returns the entry for a provider name, or false if not BYOK.
// DDGS is keyless and not present in byokProviders.
func LookupBYOK(name string) (BYOKProvider, bool) {
	for _, p := range byokProviders {
		if p.Name == name {
			return p, true
		}
	}
	return BYOKProvider{}, false
}
