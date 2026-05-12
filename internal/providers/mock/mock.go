package mock

import (
	"context"
	"fmt"

	"github.com/dorukardahan/searchmcp/internal/core"
)

type Provider struct {
	ProviderName string
}

func New(name string) Provider {
	return Provider{ProviderName: name}
}

func (p Provider) Name() string {
	if p.ProviderName == "" {
		return "mock"
	}
	return p.ProviderName
}

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 3
	}
	results := make([]core.SearchResult, 0, limit)
	for i := 1; i <= limit; i++ {
		results = append(results, core.SearchResult{
			Title:    fmt.Sprintf("Mock result %d for %s", i, req.Query),
			URL:      fmt.Sprintf("https://example.com/search/%d", i),
			Snippet:  "Deterministic mock search result used before real providers are configured.",
			Provider: p.Name(),
		})
	}
	return core.SearchResponse{Query: req.Query, Task: req.Task, Provider: p.Name(), Results: results}, nil
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	return core.ExtractResponse{URL: req.URL, Provider: p.Name(), Content: "Mock extracted content for " + req.URL}, nil
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: p.Name(), Available: true, Capabilities: p.Capabilities(), Reason: "mock adapter active; real API adapter pending"}
}
