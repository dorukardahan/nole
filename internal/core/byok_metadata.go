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
}

// BYOKProviders is the single source of truth for keyed-provider metadata.
// Update this list rather than maintaining separate maps in cli, mcpserver
// and the hints builder.
var BYOKProviders = []BYOKProvider{
	{
		Name:            "brave",
		EnvVars:         []string{"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: false,
		SignupURL:       "https://api.search.brave.com",
		FreeTierNote:    "1000 calls/month — credit card required at signup; Nólë caps usage at the local monthly quota.",
		EnvExample:      "export BRAVE_API_KEY=BSA...",
		Unlocks:         []string{"fast_general_search", "news_search_quality"},
	},
	{
		Name:            "tavily",
		EnvVars:         []string{"TAVILY_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://tavily.com",
		FreeTierNote:    "1000 calls/month, no credit card required.",
		EnvExample:      "export TAVILY_API_KEY=tvly-...",
		Unlocks:         []string{"url_extraction", "semantic_search_quality"},
	},
	{
		Name:            "firecrawl",
		EnvVars:         []string{"FIRECRAWL_API_KEY"},
		FreeQuota:       1000,
		RefreshWindow:   RefreshMonthly,
		SupportsSearch:  true,
		SupportsExtract: true,
		SignupURL:       "https://firecrawl.dev",
		FreeTierNote:    "1000 calls/month free tier — verify dashboard balance before high-volume use.",
		EnvExample:      "export FIRECRAWL_API_KEY=fc-...",
		Unlocks:         []string{"url_extraction"},
	},
}

// LookupBYOK returns the entry for a provider name, or false if not BYOK.
// DDGS is keyless and not present in BYOKProviders.
func LookupBYOK(name string) (BYOKProvider, bool) {
	for _, p := range BYOKProviders {
		if p.Name == name {
			return p, true
		}
	}
	return BYOKProvider{}, false
}
