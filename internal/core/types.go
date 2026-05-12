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
	Query    string         `json:"query"`
	Task     TaskType       `json:"task"`
	Provider string         `json:"provider"`
	Results  []SearchResult `json:"results"`
	Route    []string       `json:"route"`
}

type ExtractResponse struct {
	URL      string            `json:"url"`
	Provider string            `json:"provider"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ProviderStatus struct {
	Name         string       `json:"name"`
	Available    bool         `json:"available"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Reason       string       `json:"reason,omitempty"`
}

type BudgetStatus struct {
	HardCapCents int          `json:"hard_cap_cents"`
	Entries      []QuotaEntry `json:"entries"`
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
