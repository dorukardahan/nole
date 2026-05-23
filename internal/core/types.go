package core

import "context"

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

type Capability string

const (
	CapabilitySearch  Capability = "search"
	CapabilityExtract Capability = "extract"
	CapabilityStatus  Capability = "status"
)

type SearchRequest struct {
	Query string   `json:"query"`
	Task  TaskType `json:"task"`
	Limit int      `json:"limit"`
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
}

type BudgetStatus struct {
	Policy            CostPolicy   `json:"policy"`
	HardCapCents      int          `json:"hard_cap_cents"`
	SpentCents        int          `json:"spent_cents"`
	NoHiddenPaidSpend bool         `json:"no_hidden_paid_spend"`
	LedgerState       LedgerState  `json:"ledger_state,omitempty"`
	LedgerWarning     string       `json:"ledger_warning,omitempty"`
	Entries           []QuotaEntry `json:"entries"`
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
