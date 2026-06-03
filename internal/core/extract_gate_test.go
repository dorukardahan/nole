package core

import (
	"context"
	"testing"
)

// panicStatusProvider advertises extract but PANICS if Status() is ever called —
// it proves HasExtractCapableProvider performs a capability check only and never
// probes provider health (so MCP startup can't launch e.g. Scrapling's Python).
type panicStatusProvider struct{ name string }

func (p panicStatusProvider) Name() string { return p.name }
func (p panicStatusProvider) Capabilities() []Capability {
	return []Capability{CapabilityExtract, CapabilityStatus}
}
func (p panicStatusProvider) Search(context.Context, SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, nil
}
func (p panicStatusProvider) Extract(context.Context, ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{}, nil
}
func (p panicStatusProvider) Status(context.Context) ProviderStatus {
	panic("HasExtractCapableProvider must not call Status() at tool-registration time")
}

// searchOnlyProvider advertises search but NOT extract.
type searchOnlyProvider struct{ name string }

func (p searchOnlyProvider) Name() string { return p.name }
func (p searchOnlyProvider) Capabilities() []Capability {
	return []Capability{CapabilitySearch, CapabilityStatus}
}
func (p searchOnlyProvider) Search(context.Context, SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, nil
}
func (p searchOnlyProvider) Extract(context.Context, ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{}, nil
}
func (p searchOnlyProvider) Status(context.Context) ProviderStatus {
	return ProviderStatus{Name: p.name, Available: true}
}

func newServiceWith(providers ...Provider) *Service {
	reg := NewRegistry()
	for _, p := range providers {
		_ = reg.Register(p)
	}
	return NewService(reg, NewMemoryQuotaLedger(), DefaultRouteMatrix())
}

func TestHasExtractCapableProviderIsCapabilityOnlyNoStatusProbe(t *testing.T) {
	// An extract-capable provider whose Status() panics must NOT cause a panic:
	// the gate is a capability check and never calls Status().
	svc := newServiceWith(searchOnlyProvider{name: "brave"}, panicStatusProvider{name: "httpfetch"})
	if !svc.HasExtractCapableProvider() {
		t.Fatal("expected true: an extract-capable provider is registered")
	}
}

func TestHasExtractCapableProviderFalseWithoutExtractProvider(t *testing.T) {
	svc := newServiceWith(searchOnlyProvider{name: "brave"}, searchOnlyProvider{name: "ddgs"})
	if svc.HasExtractCapableProvider() {
		t.Fatal("expected false: no registered provider advertises extract")
	}
}
