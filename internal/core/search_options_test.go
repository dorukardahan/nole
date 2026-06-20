package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type optionRecorderProvider struct {
	name        string
	calls       int
	lastOptions SearchOptions
	allOptions  []SearchOptions
}

func (p *optionRecorderProvider) Name() string { return p.name }

func (p *optionRecorderProvider) Capabilities() []Capability {
	return []Capability{CapabilitySearch, CapabilityStatus}
}

func (p *optionRecorderProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	p.calls++
	p.lastOptions = req.Options
	p.allOptions = append(p.allOptions, req.Options)
	return SearchResponse{
		Query:    req.Query,
		Task:     req.Task,
		Provider: p.name,
		Results:  []SearchResult{{Title: "result", URL: "https://example.com", Snippet: "snippet", Provider: p.name}},
	}, nil
}

func (p *optionRecorderProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{}, errors.New("extract not supported")
}

func (p *optionRecorderProvider) Status(ctx context.Context) ProviderStatus {
	return ProviderStatus{Name: p.name, Available: true, Capabilities: p.Capabilities()}
}

func TestServiceSearchNormalizesOptionsBeforeProviderAndCacheKey(t *testing.T) {
	provider := &optionRecorderProvider{name: "p"}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewService(registry, freeTierLedger("p", 10), RouteMatrix{TaskGeneral: {"p"}}, WithResponseCache(NewMemoryResponseCache(5*time.Minute)))

	messy := SearchOptions{Country: " US ", SearchLang: " EN ", UILang: " en-US ", SafeSearch: "STRICT", Freshness: "week"}
	if _, err := svc.Search(context.Background(), SearchRequest{Query: "nole", Task: TaskGeneral, Limit: 5, Options: messy}); err != nil {
		t.Fatalf("search with options failed: %v", err)
	}
	want := SearchOptions{Country: "us", SearchLang: "en", UILang: "en-us", SafeSearch: "strict", Freshness: "pw"}
	if provider.lastOptions != want {
		t.Fatalf("provider saw options %#v, want normalized %#v", provider.lastOptions, want)
	}
	if provider.calls != 1 {
		t.Fatalf("calls after first search = %d, want 1", provider.calls)
	}

	canonical := SearchOptions{Country: "us", SearchLang: "en", UILang: "en-us", SafeSearch: "strict", Freshness: "pw"}
	if _, err := svc.Search(context.Background(), SearchRequest{Query: " NOLE ", Task: TaskGeneral, Limit: 5, Options: canonical}); err != nil {
		t.Fatalf("search with canonical options failed: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("canonical equivalent options should hit cache; calls = %d, want 1", provider.calls)
	}

	if _, err := svc.Search(context.Background(), SearchRequest{Query: "nole", Task: TaskGeneral, Limit: 5}); err != nil {
		t.Fatalf("search without options failed: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("search without options must use a distinct cache key; calls = %d, want 2", provider.calls)
	}
}

func TestServiceSearchRejectsInvalidSearchOptionsBeforeProvider(t *testing.T) {
	provider := &optionRecorderProvider{name: "p"}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := NewService(registry, freeTierLedger("p", 10), RouteMatrix{TaskGeneral: {"p"}})

	_, err := svc.Search(context.Background(), SearchRequest{
		Query:   "nole",
		Task:    TaskGeneral,
		Limit:   5,
		Options: SearchOptions{SafeSearch: "anything-goes"},
	})
	if err == nil {
		t.Fatal("expected invalid search options error")
	}
	if !IsInvalidRequest(err) {
		t.Fatalf("expected IsInvalidRequest, got %T %v", err, err)
	}
	if provider.calls != 0 {
		t.Fatalf("invalid options should fail before provider call; calls = %d", provider.calls)
	}
}
