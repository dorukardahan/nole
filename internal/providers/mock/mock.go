package mock

import (
	"context"
	"fmt"

	"github.com/dorukardahan/nole/internal/core"
)

type Provider struct {
	ProviderName string
	available    bool
	caps         []core.Capability
}

func defaultCaps() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

func New(name string) Provider {
	return Provider{ProviderName: name, available: true, caps: defaultCaps()}
}

// NewUnavailable creates a mock provider that reports as unavailable (used as placeholder).
func NewUnavailable(name string) Provider {
	return Provider{ProviderName: name, available: false, caps: defaultCaps()}
}

// NewSearchOnly creates an available mock that advertises SEARCH (and status) but
// NOT extract — for tests that need a registry with no extract-capable provider,
// e.g. the MCP extract-gate negative path.
func NewSearchOnly(name string) Provider {
	return Provider{ProviderName: name, available: true, caps: []core.Capability{core.CapabilitySearch, core.CapabilityStatus}}
}

func (p Provider) Name() string {
	if p.ProviderName == "" {
		return "mock"
	}
	return p.ProviderName
}

func (p Provider) Capabilities() []core.Capability {
	if p.caps == nil {
		return defaultCaps()
	}
	return p.caps
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
	if p.available {
		return core.ProviderStatus{Name: p.Name(), Available: true, Capabilities: p.Capabilities(), Reason: "mock adapter active; real API adapter pending"}
	}
	return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: "no API key configured; provider is placeholder only"}
}
