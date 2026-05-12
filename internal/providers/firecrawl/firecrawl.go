package firecrawl

import (
	"context"
	"fmt"

	"github.com/dorukardahan/searchmcp/internal/core"
)

type Provider struct {}

func New() Provider { return Provider{} }

func (p Provider) Name() string { return "firecrawl" }

func (p Provider) Capabilities() []core.Capability {
	return []core.Capability{core.CapabilitySearch, core.CapabilityExtract, core.CapabilityStatus}
}

func (p Provider) Search(ctx context.Context, req core.SearchRequest) (core.SearchResponse, error) {
	if !core.HasCapability(p.Capabilities(), core.CapabilitySearch) {
		return core.SearchResponse{}, fmt.Errorf("provider %s does not support search", p.Name())
	}
	return core.SearchResponse{}, fmt.Errorf("provider %s search is not implemented yet", p.Name())
}

func (p Provider) Extract(ctx context.Context, req core.ExtractRequest) (core.ExtractResponse, error) {
	if !core.HasCapability(p.Capabilities(), core.CapabilityExtract) {
		return core.ExtractResponse{}, fmt.Errorf("provider %s does not support extract", p.Name())
	}
	return core.ExtractResponse{}, fmt.Errorf("provider %s extract is not implemented yet", p.Name())
}

func (p Provider) Status(ctx context.Context) core.ProviderStatus {
	return core.ProviderStatus{Name: p.Name(), Available: false, Capabilities: p.Capabilities(), Reason: "adapter scaffolded; implementation pending"}
}
