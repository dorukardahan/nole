package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/brave"
	"github.com/dorukardahan/nole/internal/providers/ddgs"
	"github.com/dorukardahan/nole/internal/providers/firecrawl"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/dorukardahan/nole/internal/providers/tavily"
	"github.com/dorukardahan/nole/internal/safeerr"
)

func defaultService() *core.Service {
	registry := core.NewRegistry()

	firecrawlKey := os.Getenv("FIRECRAWL_API_KEY")
	braveKey := os.Getenv("BRAVE_API_KEY")
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_SEARCH_API_KEY")
	}
	tavilyKey := os.Getenv("TAVILY_API_KEY")

	// Firecrawl — real adapter (search + extract)
	if firecrawlKey != "" {
		_ = registry.Register(firecrawl.New(firecrawl.WithAPIKey(firecrawlKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("firecrawl"))
	}

	// Brave — real adapter (search only)
	if braveKey != "" {
		_ = registry.Register(brave.New(brave.WithAPIKey(braveKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("brave"))
	}

	// Tavily — real adapter (search + extract)
	if tavilyKey != "" {
		_ = registry.Register(tavily.New(tavily.WithAPIKey(tavilyKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("tavily"))
	}

	// DDGS — keyless free, always available
	_ = registry.Register(ddgs.New())

	entries := defaultQuotaEntries(braveKey, tavilyKey, firecrawlKey)
	ledger := defaultQuotaLedger(defaultQuotaPolicyFromEnv(), entries)

	opts := []core.ServiceOption{}
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
	}
}

func defaultQuotaLedger(policy core.QuotaPolicy, entries []core.QuotaEntry) core.QuotaLedger {
	path := strings.TrimSpace(os.Getenv("NOLE_QUOTA_LEDGER_PATH"))
	if path == "" || strings.EqualFold(path, "memory") || strings.EqualFold(path, "off") || strings.EqualFold(path, "none") {
		ledger := core.NewMemoryQuotaLedgerWithPolicy(policy)
		for _, entry := range entries {
			ledger.Set(entry)
		}
		return ledger
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

func defaultResponseCacheFromEnv() core.ResponseCache {
	if ttl := defaultCacheTTL(); ttl > 0 {
		return core.NewMemoryResponseCache(ttl)
	}
	return nil
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
	if raw := strings.TrimSpace(os.Getenv("NOLE_HARD_CAP_CENTS")); raw != "" {
		if cents, err := strconv.Atoi(raw); err == nil && cents > 0 {
			policy.HardCapCents = cents
		}
	}
	return policy
}

// byokFreeDefaults is the canonical per-provider free-tier seed used when a
// BYOK key is present and the user has not explicitly opted into paid usage.
// Numbers are conservative anchors matching each provider's current monthly
// free tier; see docs/PROVIDER-KEYS.md for sourcing.
var byokFreeDefaults = map[string]struct {
	Quota  int
	Window core.RefreshWindow
}{
	"brave":     {Quota: 1000, Window: core.RefreshMonthly},
	"tavily":    {Quota: 1000, Window: core.RefreshMonthly},
	"firecrawl": {Quota: 1000, Window: core.RefreshMonthly},
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
		return core.QuotaEntry{Provider: provider, CostClass: core.CostClassDisabledNoKey}
	}
	if isProviderPaidMode(provider) {
		return core.QuotaEntry{
			Provider:           provider,
			CostClass:          core.CostClassPremiumCapable,
			EstimatedCostCents: providerEstimatedCostCents(provider),
		}
	}
	if def, ok := byokFreeDefaults[provider]; ok {
		return core.QuotaEntry{
			Provider:      provider,
			CostClass:     core.CostClassFreeTierBYOK,
			FreeRemaining: def.Quota,
			FreeQuota:     def.Quota,
			RefreshWindow: def.Window,
			PeriodStart:   core.CurrentMonthISO(),
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

func runSearch(query string, task core.TaskType, limit int) (core.SearchResponse, error) {
	if query == "" {
		return core.SearchResponse{}, fmt.Errorf("query is required")
	}
	return defaultService().Search(context.Background(), core.SearchRequest{Query: query, Task: task, Limit: limit})
}
