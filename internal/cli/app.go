package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/nolelog"
	"github.com/dorukardahan/nole/internal/providers/arxiv"
	"github.com/dorukardahan/nole/internal/providers/brave"
	"github.com/dorukardahan/nole/internal/providers/ddgs"
	"github.com/dorukardahan/nole/internal/providers/firecrawl"
	"github.com/dorukardahan/nole/internal/providers/httpfetch"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/dorukardahan/nole/internal/providers/providerhttp"
	"github.com/dorukardahan/nole/internal/providers/scrapling"
	"github.com/dorukardahan/nole/internal/providers/tavily"
	"github.com/dorukardahan/nole/internal/providers/wikipedia"
	"github.com/dorukardahan/nole/internal/safeerr"
)

func defaultService() *core.Service {
	loadDefaultNoleEnvFile()

	registry := core.NewRegistry()

	firecrawlKey := os.Getenv("FIRECRAWL_API_KEY")
	clientMode := strings.ToLower(strings.TrimSpace(os.Getenv("NOLE_CLIENT")))
	braveKey := os.Getenv("BRAVE_API_KEY")
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_SEARCH_API_KEY")
	}
	tavilyKey := os.Getenv("TAVILY_API_KEY")

	// One circuit breaker per remote provider that is NOT the last-resort
	// fallback. In a long-lived `nole serve` / MCP process, a persistently-failing
	// upstream trips its breaker and is short-circuited (fail fast, no burned
	// timeout, no quota debit) until a cooldown probe recovers it. State is
	// in-memory and per-process; one-shot CLI invocations never accumulate enough
	// failures to trip. The keyless DDGS fallback and the local Scrapling extractor
	// are intentionally left unbreakered so the free last-resort path is never
	// short-circuited. Wikipedia is keyless too but is routed BEFORE that fallback
	// (factcheck/people/academic), so it IS breakered — otherwise a slow
	// en.wikipedia.org would stall those routes ahead of DDGS on every request.
	breakerOpts := providerhttp.DefaultBreakerOptions()

	// Firecrawl — real adapter (search + extract). The direct API behavior is
	// unchanged for generic clients. The dedicated OpenClaw wrapper opts into a
	// host-tool bridge instead; current stable hosts provide web_fetch with a
	// keyless Firecrawl fallback, while newer hosts may also expose firecrawl-free.
	firecrawlOptions := []firecrawl.Option{
		firecrawl.WithAPIKey(firecrawlKey),
		firecrawl.WithBreaker(providerhttp.NewBreaker(breakerOpts)),
	}
	effectiveFirecrawlKey := firecrawlKey
	if clientMode == "openclaw" {
		openClawCLI := strings.TrimSpace(os.Getenv("NOLE_OPENCLAW_CLI"))
		if openClawCLI == "" {
			openClawCLI = "openclaw"
		}
		bridgeMode := firecrawl.OpenClawBridgeMode(strings.TrimSpace(os.Getenv("NOLE_OPENCLAW_BRIDGE")))
		if bridgeMode != firecrawl.OpenClawBridgeFull && bridgeMode != firecrawl.OpenClawBridgeFetchOnly {
			bridgeMode = firecrawl.OpenClawBridgeFull
		}
		firecrawlOptions = append(firecrawlOptions, firecrawl.WithOpenClawBridgeMode(openClawCLI, bridgeMode))
		// OpenClaw owns the upstream call/quota. An inherited generic Firecrawl
		// key is intentionally not charged or suggested in this wrapper process.
		effectiveFirecrawlKey = ""
	}
	_ = registry.Register(firecrawl.New(firecrawlOptions...))

	// Brave — real adapter (search only)
	if braveKey != "" {
		_ = registry.Register(brave.New(brave.WithAPIKey(braveKey), brave.WithBreaker(providerhttp.NewBreaker(breakerOpts))))
	} else {
		_ = registry.Register(mock.NewUnavailable("brave"))
	}

	// Tavily — real adapter (search + extract)
	if tavilyKey != "" {
		_ = registry.Register(tavily.New(tavily.WithAPIKey(tavilyKey), tavily.WithBreaker(providerhttp.NewBreaker(breakerOpts))))
	} else {
		_ = registry.Register(mock.NewUnavailable("tavily"))
	}

	// DDGS — keyless free, always available (last-resort general fallback)
	_ = registry.Register(ddgs.New())

	// Wikipedia/MediaWiki — keyless free, reinforces factcheck/people/academic.
	// Breakered (it is routed before the DDGS fallback, so a slow upstream must
	// not stall those routes on every request).
	_ = registry.Register(wikipedia.New(wikipedia.WithBreaker(providerhttp.NewBreaker(breakerOpts))))

	// arXiv — keyless free, reinforces the academic route with primary-source
	// scholarly preprints. Breakered for the same reason as Wikipedia (routed
	// before the DDGS fallback on academic).
	_ = registry.Register(arxiv.New(arxiv.WithBreaker(providerhttp.NewBreaker(breakerOpts))))

	// Scrapling — local Python extractor, keyless/free when installed
	_ = registry.Register(scrapling.New())

	// httpfetch — keyless pure-Go HTTP-fetch + HTML-to-text extractor. Last-resort
	// extract backstop (the extract-side analogue of DDGS): unbreakered like the
	// other free fallbacks so the keyless extract path is never short-circuited. It
	// makes extract / search_and_extract work with zero keys and zero setup.
	_ = registry.Register(httpfetch.New())

	entries := defaultQuotaEntries(braveKey, tavilyKey, effectiveFirecrawlKey)
	ledger := defaultQuotaLedger(defaultQuotaPolicyFromEnv(), entries)

	opts := []core.ServiceOption{
		// Diagnostic events (e.g. a non-fatal research step failure) go to stderr
		// only, formatted per NOLE_LOG (text|json|off). stdout stays reserved for
		// MCP JSON-RPC, REST bodies, and --json command output.
		core.WithLogger(nolelog.FromEnv(os.Stderr)),
		core.WithClientMode(clientMode),
	}
	if cache := defaultResponseCacheFromEnv(); cache != nil {
		opts = append(opts, core.WithResponseCache(cache))
	}
	return core.NewService(registry, ledger, core.DefaultRouteMatrix(), opts...)
}

func defaultQuotaEntries(braveKey, tavilyKey, firecrawlKey string) []core.QuotaEntry {
	return []core.QuotaEntry{
		providerQuotaEntry("brave", braveKey != ""),
		providerQuotaEntry("tavily", tavilyKey != ""),
		providerQuotaEntry("firecrawl", firecrawlKey != ""),
		{Provider: "ddgs", CostClass: core.CostClassKeylessFree, KeylessFree: true},
		{Provider: "wikipedia", CostClass: core.CostClassKeylessFree, KeylessFree: true},
		{Provider: "arxiv", CostClass: core.CostClassKeylessFree, KeylessFree: true},
		{Provider: "scrapling", CostClass: core.CostClassKeylessFree, KeylessFree: true},
		{Provider: "httpfetch", CostClass: core.CostClassKeylessFree, KeylessFree: true},
	}
}

func defaultQuotaLedger(policy core.QuotaPolicy, entries []core.QuotaEntry) core.QuotaLedger {
	raw := strings.TrimSpace(os.Getenv("NOLE_QUOTA_LEDGER_PATH"))

	// Explicit opt-out into memory-only mode. The monthly free-tier guard is
	// not durable across process restarts in this mode, so per-session spawn
	// patterns (e.g. an MCP client that re-launches nole per turn) will reset
	// the counter. Only honour the opt-out when the user typed it.
	if strings.EqualFold(raw, "memory") || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "none") {
		ledger := core.NewMemoryQuotaLedgerWithPolicy(policy)
		for _, entry := range entries {
			ledger.Set(entry)
		}
		return ledger
	}

	// Default: file-backed ledger at $XDG_STATE_HOME/nole/quota-ledger.json
	// (or ~/.local/state/nole/quota-ledger.json on platforms that don't set
	// XDG_STATE_HOME). This makes the "caps usage at the monthly free quota"
	// claim true in brave_note and other surfaces: the counter actually
	// survives process restarts.
	path := raw
	if path == "" {
		path = defaultQuotaLedgerPath()
	}

	if path == "" {
		// Could not resolve a writable default path. Fall back to memory and
		// rely on doctor surfaces to flag the lack of durability if/when the
		// user inspects ledger state. Better than crashing on every startup.
		fallback := core.NewMemoryQuotaLedgerWithPolicy(policy)
		for _, entry := range entries {
			fallback.Set(entry)
		}
		return fallback
	}

	ledger, err := core.NewFileQuotaLedgerWithPolicy(path, policy, entries)
	if err != nil && ledger != nil {
		return ledger
	}
	if ledger != nil {
		return ledger
	}
	fallback := core.NewMemoryQuotaLedgerWithPolicy(policy)
	for _, entry := range entries {
		fallback.Set(entry)
	}
	return fallback
}

// defaultQuotaLedgerPath resolves the on-disk location for the BYOK quota
// ledger when the user has not set NOLE_QUOTA_LEDGER_PATH. Honours XDG when
// available AND absolute; otherwise falls back to
// ~/.local/state/nole/quota-ledger.json per the de-facto Linux/macOS
// convention. Returns "" when no home directory can be resolved; callers
// handle that by falling back to memory mode.
//
// XDG_STATE_HOME is required by spec to be an absolute path; users sometimes
// set it to "~/.local/state" or other relative strings that would resolve
// against the process cwd and leak ledger state into project trees. Reject
// non-absolute values rather than honour them.
func defaultQuotaLedgerPath() string {
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" && filepath.IsAbs(state) {
		return filepath.Join(state, "nole", "quota-ledger.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "nole", "quota-ledger.json")
}

func defaultResponseCacheFromEnv() core.ResponseCache {
	ttl := defaultCacheTTL()
	if ttl <= 0 {
		return nil
	}
	cache := core.NewMemoryResponseCache(ttl)
	if max := defaultCacheMaxEntries(); max > 0 {
		cache.SetMaxEntries(max)
	}
	return cache
}

// defaultCacheMaxEntries reads an optional per-map cache size cap from
// NOLE_CACHE_MAX_ENTRIES. Returns 0 (use the built-in DefaultCacheMaxEntries
// bound) when unset or invalid.
func defaultCacheMaxEntries() int {
	if raw := strings.TrimSpace(os.Getenv("NOLE_CACHE_MAX_ENTRIES")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func defaultCacheTTL() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("NOLE_CACHE_TTL")); raw != "" {
		if ttl, err := time.ParseDuration(raw); err == nil && ttl > 0 {
			return ttl
		}
		return 0
	}
	if raw := strings.TrimSpace(os.Getenv("NOLE_CACHE_TTL_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func defaultQuotaPolicyFromEnv() core.QuotaPolicy {
	policy := core.DefaultQuotaPolicy()
	if parsed, ok := core.ParseCostPolicy(os.Getenv("NOLE_COST_POLICY")); ok {
		policy.Policy = parsed
	}
	capSet := false
	if raw := strings.TrimSpace(os.Getenv("NOLE_HARD_CAP_CENTS")); raw != "" {
		if cents, err := strconv.Atoi(raw); err == nil && cents > 0 {
			policy.HardCapCents = cents
			capSet = true
		}
	}
	// Resolve the cap source alongside the value so an explicit $5 cap stays
	// distinguishable from no cap. We deliberately do NOT default a cost-capped
	// policy to some spend amount: with no cap set we stay fail-closed (premium
	// blocked) and let doctor/budget_status explain it, rather than authorize a
	// bill the user never asked for.
	if policy.Policy == core.CostPolicyCostCapped {
		if capSet {
			policy.HardCapSource = "explicit"
		} else {
			policy.HardCapSource = "unset"
		}
	}
	return policy
}

// isProviderPaidMode reports whether the user has explicitly opted into paid
// usage for a provider via NOLE_<PROVIDER>_PAID=1/true/yes. In paid mode the
// provider is treated as premium-capable; the policy gate then decides whether
// free-first blocks it.
func isProviderPaidMode(provider string) bool {
	raw := strings.TrimSpace(os.Getenv("NOLE_" + strings.ToUpper(provider) + "_PAID"))
	switch strings.ToLower(raw) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func providerQuotaEntry(provider string, keyPresent bool) core.QuotaEntry {
	if !keyPresent {
		if provider == "firecrawl" {
			return core.QuotaEntry{Provider: provider, CostClass: core.CostClassKeylessFree, KeylessFree: true}
		}
		return core.QuotaEntry{Provider: provider, CostClass: core.CostClassDisabledNoKey}
	}
	if isProviderPaidMode(provider) {
		return core.QuotaEntry{
			Provider:           provider,
			CostClass:          core.CostClassPremiumCapable,
			EstimatedCostCents: providerEstimatedCostCents(provider),
		}
	}
	if def, ok := core.LookupBYOK(provider); ok {
		return core.QuotaEntry{
			Provider:      provider,
			CostClass:     core.CostClassFreeTierBYOK,
			FreeRemaining: def.FreeQuota,
			FreeQuota:     def.FreeQuota,
			RefreshWindow: def.RefreshWindow,
			PeriodStart:   core.CurrentMonthISO(),
			MeteringModel: def.MeteringModel,
			EstimateOnly:  def.EstimateOnly,
		}
	}
	// Unknown provider with a key — fall back to premium-capable (legacy
	// behavior). Keeps the door open for future BYOK adapters whose free tier
	// hasn't been characterised yet.
	return core.QuotaEntry{
		Provider:           provider,
		CostClass:          core.CostClassPremiumCapable,
		EstimatedCostCents: providerEstimatedCostCents(provider),
	}
}

func providerEstimatedCostCents(provider string) int {
	envName := "NOLE_" + strings.ToUpper(provider) + "_ESTIMATED_COST_CENTS"
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		if cents, err := strconv.Atoi(raw); err == nil && cents > 0 {
			return cents
		}
	}
	return 0
}

func writeJSON(v any) error {
	return writeJSONTo(os.Stdout, v)
}

func writeJSONTo(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

type cliErrorEnvelope struct {
	Operation      string              `json:"operation"`
	Error          string              `json:"error"`
	Route          []string            `json:"route,omitempty"`
	RoutingInsight string              `json:"routing_insight,omitempty"`
	RouteTrace     []core.RouteAttempt `json:"route_trace,omitempty"`
}

func buildCLIError(operation string, err error, route []string, trace []core.RouteAttempt) cliErrorEnvelope {
	return buildCLIErrorWithInsightMode(operation, err, route, trace, core.InsightCompact)
}

func buildCLIErrorWithInsightMode(operation string, err error, route []string, trace []core.RouteAttempt, mode core.InsightMode) cliErrorEnvelope {
	insight := ""
	if mode != core.InsightOff {
		insight = core.BuildErrorRoutingInsight(operation, route, trace)
	}
	return cliErrorEnvelope{
		Operation:      operation,
		Error:          safeerr.Message(err),
		Route:          append([]string(nil), route...),
		RoutingInsight: insight,
		RouteTrace:     append([]core.RouteAttempt(nil), trace...),
	}
}

func parseInsightModeFlag(raw string) (core.InsightMode, error) {
	mode, ok := core.ParseInsightMode(raw)
	if !ok {
		return "", fmt.Errorf("invalid --insight %q (want compact, off, or verbose)", raw)
	}
	return mode, nil
}

func applySearchInsightMode(resp core.SearchResponse, mode core.InsightMode) core.SearchResponse {
	if mode == core.InsightOff {
		resp.RoutingInsight = ""
		return resp
	}
	if resp.RoutingInsight == "" {
		resp.RoutingInsight = core.BuildSearchRoutingInsight(resp)
	}
	return resp
}

func applyExtractInsightMode(resp core.ExtractResponse, mode core.InsightMode) core.ExtractResponse {
	if mode == core.InsightOff {
		resp.RoutingInsight = ""
		return resp
	}
	if resp.RoutingInsight == "" {
		resp.RoutingInsight = core.BuildExtractRoutingInsight(resp)
	}
	return resp
}

func applyClassificationInsightMode(resp core.QueryClassification, mode core.InsightMode) core.QueryClassification {
	if mode == core.InsightOff {
		resp.RoutingInsight = ""
		return resp
	}
	if resp.RoutingInsight == "" {
		resp.RoutingInsight = core.BuildClassificationRoutingInsight(resp)
	}
	return resp
}

func applyRoutePlanInsightMode(resp core.RoutePlan, mode core.InsightMode) core.RoutePlan {
	if mode == core.InsightOff {
		resp.RoutingInsight = ""
		return resp
	}
	if resp.RoutingInsight == "" {
		resp.RoutingInsight = core.BuildRoutePlanRoutingInsight(resp)
	}
	return resp
}

func writeHumanRoutingInsight(w io.Writer, insight string, trace []core.RouteAttempt, mode core.InsightMode) {
	if mode == core.InsightOff {
		return
	}
	if strings.TrimSpace(insight) != "" {
		fmt.Fprintln(w, insight)
	}
	if mode != core.InsightVerbose {
		return
	}
	for _, line := range core.FormatRouteTraceLines(trace) {
		fmt.Fprintf(w, "route_trace: %s\n", line)
	}
}

func parseTask(raw string) core.TaskType {
	task, ok := parseTaskStrict(raw)
	if !ok {
		return core.TaskGeneral
	}
	return task
}

func parseTaskStrict(raw string) (core.TaskType, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "news":
		return core.TaskNews, true
	case "docs", "technical-docs":
		return core.TaskDocs, true
	case "academic":
		return core.TaskAcademic, true
	case "factcheck":
		return core.TaskFactcheck, true
	case "semantic":
		return core.TaskSemantic, true
	case "code":
		return core.TaskCode, true
	case "social", "community", "forum", "forums":
		return core.TaskSocial, true
	case "people":
		return core.TaskPeople, true
	case "pricing":
		return core.TaskPricing, true
	case "extract":
		return core.TaskExtract, true
	case "research", "deep-research":
		return core.TaskResearch, true
	case "general", "":
		return core.TaskGeneral, true
	default:
		return core.TaskGeneral, false
	}
}

func runSearch(ctx context.Context, query string, task core.TaskType, limit int, options core.SearchOptions) (core.SearchResponse, error) {
	if query == "" {
		return core.SearchResponse{}, fmt.Errorf("query is required")
	}
	return defaultService().Search(ctx, core.SearchRequest{Query: query, Task: task, Limit: limit, Options: options})
}
