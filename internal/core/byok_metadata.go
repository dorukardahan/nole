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
		FreeTierNote:    "Free tier changed Feb 2026: new accounts get ~1000 queries/month from a $5 auto-renewing monthly credit ($0.005/query on the Search plan), then metered billing on the card on file (no cap by default, but you can set a usage limit in the Brave dashboard to bound overages). The old flat free tier (2000+/month) was eliminated 12 Feb 2026; legacy-account grandfathering is unconfirmed. Brave requires attribution on your site to grant the credit. Nólë caps at 1000/month as a fail-safe floor and counts its own calls.",
		EnvExample:      "export BRAVE_API_KEY=BSA...",
		Unlocks:         []string{"fast_general_search", "news_search_quality"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "~1000 queries/month from the $5 monthly credit ($0.005/query, 1 call = 1 query) + a 50 req/sec Search-plan rate cap Nólë does not track; beyond the free credit Brave bills the card on file unless you set a usage limit in the Brave dashboard.",
		EstimateOnly:    true,
	},
	{
		Name:    "tavily",
		EnvVars: []string{"TAVILY_API_KEY"},
		// FreeQuota is a CALL floor, not the 1000-credit grant: Tavily meters in
		// variable credits (basic search 1, advanced search/extract 2) while the
		// ledger debits 1 per call, so the never-overcount floor is 1000 credits /
		// 2 worst-case-credits-per-call = 500 calls. Undercounting basic-only usage
		// is the safe direction; the drift signal catches the rest.
		FreeQuota:       500,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://tavily.com",
		FreeTierNote:    "~1000 API credits/month on the free Researcher plan, no credit card required. Credits are variable-cost (basic search 1, advanced search/extract 2), so Nólë seeds a 500-call fail-safe floor (1000 credits / 2 worst-case per call) and counts its own calls.",
		EnvExample:      "export TAVILY_API_KEY=tvly-...",
		Unlocks:         []string{"url_extraction", "semantic_search_quality"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "credit-based: basic search ~1 credit, advanced search/extract ~2 (Nólë uses advanced depth only for research-task queries); Nólë debits 1 per call against a 500-call floor, so heavy advanced use can still exhaust the dashboard before the local count - verify your dashboard. Free dev tier ~100 RPM, not tracked.",
		EstimateOnly:    true,
	},
	{
		Name:    "firecrawl",
		EnvVars: []string{"FIRECRAWL_API_KEY"},
		// FreeQuota is a CALL floor, not the 1000-credit grant: Firecrawl meters in
		// variable credits (scrape 1/page; search 2 credits per 10 results, so a
		// 20-result search — which Service permits up to maxSearchLimit=20 — costs
		// 4; Enhanced Mode 5, which Nólë never issues) while the ledger debits 1 per
		// call. The floor uses the priciest op Nólë can issue: 1000 credits / 4 =
		// 250 calls.
		FreeQuota:       250,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://firecrawl.dev",
		FreeTierNote:    "Firecrawl supports limited keyless API calls for zero-setup use. FIRECRAWL_API_KEY is optional and switches Nólë to account-backed quota: 1000 credits/month on Firecrawl's free plan, reset monthly with no rollover, no credit card required. Credits are variable-cost (scrape 1/page; search 2 credits per 10 results, so a 20-result search costs 4; Enhanced Mode 5/request, which Nólë does not use), so keyed mode seeds a 250-call fail-safe floor (1000 credits / 4 worst-case per call) and counts its own calls - verify your dashboard.",
		EnvExample:      "export FIRECRAWL_API_KEY=fc-...",
		Unlocks:         []string{"url_extraction"},
		MeteringModel:   "credit-based",
		RateLimitNote:   "keyless mode is capped upstream per IP per day by both request and credit limits; crossing either returns 429. Nólë adds no local keyless quota and does not pretend to know remote balance. Keyed mode is credit-based: scrape ~1 credit/page, search ~2 credits per 10 results (up to 4 for a 20-result call; Nólë never issues the 5-credit Enhanced Mode); Nólë debits 1 per call against a 250-call floor. Free-tier rate limits /scrape 10 rpm, /search 5 rpm, not tracked.",
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
