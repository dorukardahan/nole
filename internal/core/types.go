package core

import (
	"context"
	"strings"
	"unicode/utf8"
)

// TruncateRunes returns s unchanged when it contains at most max runes;
// otherwise it truncates on a rune boundary (never mid-UTF-8-sequence) and
// appends an ellipsis. Provider snippets and extracted content are frequently
// non-ASCII, so byte-slicing (s[:max]) could split a multibyte rune and emit
// invalid UTF-8 (mojibake) in the trailing characters. A negative max is
// treated as 0.
func TruncateRunes(s string, max int) string {
	if max < 0 {
		max = 0
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}

type TaskType string

const (
	TaskGeneral   TaskType = "general"
	TaskNews      TaskType = "news"
	TaskDocs      TaskType = "docs"
	TaskAcademic  TaskType = "academic"
	TaskFactcheck TaskType = "factcheck"
	TaskSemantic  TaskType = "semantic"
	TaskCode      TaskType = "code"
	TaskSocial    TaskType = "social"
	TaskPeople    TaskType = "people"
	TaskPricing   TaskType = "pricing"
	TaskExtract   TaskType = "extract"
	TaskResearch  TaskType = "research"
)

// TaskSource records how a search's task was determined: supplied by the caller,
// detected by the deterministic planner when the caller omitted it, or the
// general default when the planner found no signal. Observability only — Nólë
// never "decides" beyond reading a static keyword table.
type TaskSource string

const (
	TaskSourceSupplied TaskSource = "supplied"
	TaskSourceDetected TaskSource = "detected"
	TaskSourceDefault  TaskSource = "default"
)

type Capability string

const (
	CapabilitySearch  Capability = "search"
	CapabilityExtract Capability = "extract"
	CapabilityStatus  Capability = "status"
)

type SearchRequest struct {
	Query   string        `json:"query"`
	Task    TaskType      `json:"task"`
	Limit   int           `json:"limit"`
	Options SearchOptions `json:"options,omitempty"`
}

// SearchOptions carries caller-supplied search controls that providers apply
// when their public API supports the field. The service validates and normalizes
// the struct before routing, so providers receive canonical values and the cache
// keys options deterministically.
type SearchOptions struct {
	Country    string `json:"country,omitempty"`
	SearchLang string `json:"search_lang,omitempty"`
	UILang     string `json:"ui_lang,omitempty"`
	SafeSearch string `json:"safesearch,omitempty"`
	Freshness  string `json:"freshness,omitempty"`
}

type ExtractRequest struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

type SearchResult struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Snippet  string `json:"snippet"`
	Provider string `json:"provider"`
	// Score and PublishedAt are provider-native signals passed through verbatim
	// for the AGENT to judge relevance/recency. Nólë never computes, normalizes,
	// or judges them; nil/empty when the provider supplies none. Score is a
	// pointer so a genuine 0.0 is distinguishable from "absent". Treat *Score as
	// immutable after adapter construction — the cache shares the pointer across
	// entries (see cloneSearchResponse), so an in-place mutation would race.
	Score       *float64 `json:"score,omitempty"`
	PublishedAt string   `json:"published_at,omitempty"`
}

type SearchResponse struct {
	Query          string         `json:"query"`
	Task           TaskType       `json:"task"`
	Provider       string         `json:"provider"`
	Results        []SearchResult `json:"results"`
	Route          []string       `json:"route"`
	RoutingInsight string         `json:"routing_insight,omitempty"`
	RouteTrace     []RouteAttempt `json:"route_trace,omitempty"`
	SetupTip       *SetupTip      `json:"setup_tip,omitempty"`
	// TaskSource reports how Task was chosen (supplied/detected/default), so the
	// agent can see whether it drove the route or Nólë's planner inferred it.
	TaskSource TaskSource `json:"task_source,omitempty"`
}

type ExtractResponse struct {
	URL            string            `json:"url"`
	Provider       string            `json:"provider"`
	Content        string            `json:"content"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Route          []string          `json:"route,omitempty"`
	RoutingInsight string            `json:"routing_insight,omitempty"`
	RouteTrace     []RouteAttempt    `json:"route_trace,omitempty"`
}

// SearchAndExtractRequest drives the combined search→read primitive: search,
// then extract the top ExtractTop result URLs in a single call.
type SearchAndExtractRequest struct {
	Query      string        `json:"query"`
	Task       TaskType      `json:"task,omitempty"`
	Limit      int           `json:"limit"`
	ExtractTop int           `json:"extract_top"`
	Options    SearchOptions `json:"options,omitempty"`
}

// SearchAndExtractResponse pairs the search results with the extracted content of
// the top result(s). ExtractErrors records per-URL extract failures so a single
// bad URL is observable rather than silently dropped — the call itself stays
// successful as long as the search succeeded.
type SearchAndExtractResponse struct {
	Search        SearchResponse    `json:"search"`
	Extracts      []ExtractResponse `json:"extracts"`
	ExtractErrors []ExtractError    `json:"extract_errors,omitempty"`
}

// ExtractError is a sanitized per-URL extract failure inside SearchAndExtract.
type ExtractError struct {
	URL            string `json:"url"`
	Error          string `json:"error"`
	RoutingInsight string `json:"routing_insight,omitempty"`
}

type RouteAttempt struct {
	Provider           string            `json:"provider"`
	Status             string            `json:"status"`
	Reason             string            `json:"reason,omitempty"`
	CostPolicy         CostPolicy        `json:"cost_policy,omitempty"`
	CostClass          ProviderCostClass `json:"cost_class,omitempty"`
	CacheStatus        string            `json:"cache_status,omitempty"`
	LatencyMS          int64             `json:"latency_ms,omitempty"`
	ResultCount        int               `json:"result_count,omitempty"`
	EstimatedCostCents int               `json:"estimated_cost_cents,omitempty"`
}

// SetupSuggestion describes one missing BYOK key and what configuring it
// would unlock. Surfaced inside the provider_status response (after the
// Task 5 envelope migration) and, in compact summary form, inside
// SearchResponse.SetupTip.
type SetupSuggestion struct {
	MissingKey        string   `json:"missing_key"`
	Impact            string   `json:"impact"` // "high" | "medium" | "low"
	Unlocks           []string `json:"unlocks"`
	CurrentWorkaround string   `json:"current_workaround"`
	FreeTier          string   `json:"free_tier"`
	SignupURL         string   `json:"signup_url"`
	EnvExample        string   `json:"env_example"`
}

// SetupTip is the once-per-MCP-session message embedded into the first
// SearchResponse when at least one BYOK key is missing. AI tools surface
// this to the user as a one-time upgrade hint; subsequent search calls on
// the same connection omit the field entirely.
type SetupTip struct {
	Summary string `json:"summary"`
	SeeAlso string `json:"see_also"`
}

// ProviderStatusResponse wraps the per-provider status slice with optional
// metadata. Today it carries SetupSuggestions: a list of missing BYOK keys
// and what configuring them would unlock. Returned by Service.ProviderStatus.
type ProviderStatusResponse struct {
	Providers        []ProviderStatus  `json:"providers"`
	SetupSuggestions []SetupSuggestion `json:"setup_suggestions,omitempty"`
}

type ProviderStatus struct {
	Name               string            `json:"name"`
	Available          bool              `json:"available"`
	Capabilities       []Capability      `json:"capabilities,omitempty"`
	Reason             string            `json:"reason,omitempty"`
	CostPolicy         CostPolicy        `json:"cost_policy,omitempty"`
	CostClass          ProviderCostClass `json:"cost_class,omitempty"`
	AllowedByPolicy    bool              `json:"allowed_by_policy"`
	PolicyReason       string            `json:"policy_reason,omitempty"`
	FreeRemaining      int               `json:"free_remaining,omitempty"`
	EstimatedCostCents int               `json:"estimated_cost_cents,omitempty"`
	SpentCents         int               `json:"spent_cents,omitempty"`
	// Circuit-breaker observability (set by breakered providers' own Status()).
	// BreakerState is the raw lifecycle state ("closed"/"open"/"half-open") and
	// is passed through verbatim for the agent to reason about — Nólë never
	// computes a healing ETA or a recommended fallback. Omitted for unbreakered
	// providers (DDGS, Scrapling). Note: a breaker whose cooldown has elapsed
	// reports BreakerState=="open" yet is probe-eligible; the binary
	// "currently short-circuiting" truth is folded into Available instead.
	BreakerState       string `json:"breaker_state,omitempty"`
	BreakerConsecFails int    `json:"breaker_consec_fails,omitempty"`
	BreakerOpenedAt    string `json:"breaker_opened_at,omitempty"` // RFC3339 UTC
	// DriftWarning is present when a recent (<24h) drift signal exists for this
	// provider — i.e. it returned 429 while the local counter showed room.
	DriftWarning string `json:"drift_warning,omitempty"`
}

type BudgetStatus struct {
	Policy            CostPolicy  `json:"policy"`
	HardCapCents      int         `json:"hard_cap_cents"`
	HardCapSource     string      `json:"hard_cap_source,omitempty"`
	SpentCents        int         `json:"spent_cents"`
	NoHiddenPaidSpend bool        `json:"no_hidden_paid_spend"`
	LedgerState       LedgerState `json:"ledger_state,omitempty"`
	LedgerWarning     string      `json:"ledger_warning,omitempty"`
	// EstimateNote states that per-provider FreeRemaining counts are Nólë's own
	// issued-request estimate, not a live provider-dashboard balance. Present
	// when at least one entry is EstimateOnly.
	EstimateNote string `json:"estimate_note,omitempty"`
	// HasDrift / DriftSignals surface providers that rejected a call as
	// over-quota (HTTP 429) while Nólë's local counter still showed room — a
	// mechanical "my estimate disagreed with the provider" flag, never a
	// learned judgement. Signals age out of this output after 24h.
	HasDrift     bool          `json:"has_drift"`
	DriftSignals []DriftSignal `json:"drift_signals,omitempty"`
	Entries      []QuotaEntry  `json:"entries"`
}

// DriftSignal records that a provider returned HTTP 429 (over-quota/rate-limit)
// while Nólë's local free-tier counter still showed room. It is observability
// only: Nólë never debits on it, never reorders routes from it, and never
// judges whether the provider is "healthy" — it simply reports that its own
// issued-request estimate disagreed with the provider's answer.
type DriftSignal struct {
	Provider   string `json:"provider"`
	Reason     string `json:"reason"`
	ObservedAt string `json:"observed_at"` // RFC3339 UTC
}

type Provider interface {
	Name() string
	Capabilities() []Capability
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)
	Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error)
	Status(ctx context.Context) ProviderStatus
}

func HasCapability(caps []Capability, want Capability) bool {
	for _, cap := range caps {
		if cap == want {
			return true
		}
	}
	return false
}

// TaskTypes returns all defined task types in canonical order.
func TaskTypes() []TaskType {
	return []TaskType{
		TaskGeneral, TaskNews, TaskDocs, TaskAcademic, TaskFactcheck,
		TaskSemantic, TaskCode, TaskSocial, TaskPeople, TaskPricing,
		TaskExtract, TaskResearch,
	}
}

// TaskDescription returns a one-line description for a task type.
func TaskDescription(t TaskType) string {
	switch t {
	case TaskGeneral:
		return "broad web search"
	case TaskNews:
		return "current events and headlines"
	case TaskDocs:
		return "technical documentation lookup"
	case TaskAcademic:
		return "papers and scholarly research"
	case TaskFactcheck:
		return "fact verification queries"
	case TaskSemantic:
		return "conceptual and similarity searches"
	case TaskCode:
		return "code and implementation examples"
	case TaskSocial:
		return "forum and community discussions"
	case TaskPeople:
		return "people and biography lookups"
	case TaskPricing:
		return "product and service pricing"
	case TaskExtract:
		return "URL content extraction"
	case TaskResearch:
		return "deep multi-source research"
	default:
		return "unknown task type"
	}
}

// IsKnownSearchTask reports whether t is a task the search planner/router can
// honor as an explicit caller choice. Every TaskType counts EXCEPT TaskExtract,
// which is an extract-path routing key, never a search intent. The service uses
// this to decide whether to trust a supplied task or fall back to classifying
// the query.
func IsKnownSearchTask(t TaskType) bool {
	if t == TaskExtract {
		return false
	}
	for _, k := range TaskTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// NormalizeTaskParam maps a free-text task parameter (from the MCP/REST surface,
// where callers send arbitrary strings) onto a canonical TaskType, accepting the
// same aliases as the CLI's parseTaskStrict (e.g. community/forum → social).
// Blank, unknown, or "extract" all return "" so the service classifies the
// query instead of misrouting — leniency lives here, not in an error. The CLI
// keeps its own parseTask; this is the shared boundary for MCP/REST.
func NormalizeTaskParam(raw string) TaskType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "news":
		return TaskNews
	case "docs", "technical-docs":
		return TaskDocs
	case "academic":
		return TaskAcademic
	case "factcheck":
		return TaskFactcheck
	case "semantic":
		return TaskSemantic
	case "code":
		return TaskCode
	case "social", "community", "forum", "forums":
		return TaskSocial
	case "people":
		return TaskPeople
	case "pricing":
		return TaskPricing
	case "research", "deep-research":
		return TaskResearch
	case "general":
		return TaskGeneral
	default:
		// blank, unknown, or "extract" → let the service classify the query.
		return ""
	}
}
