package core

import (
	"context"
	"testing"
)

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Capabilities() []Capability {
	return []Capability{CapabilitySearch, CapabilityExtract}
}
func (f fakeProvider) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	return SearchResponse{Query: req.Query, Provider: f.name}, nil
}
func (f fakeProvider) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	return ExtractResponse{URL: req.URL, Provider: f.name}, nil
}
func (f fakeProvider) Status(ctx context.Context) ProviderStatus {
	return ProviderStatus{Name: f.name, Available: true}
}

func TestTaskConstants(t *testing.T) {
	cases := map[TaskType]string{
		TaskGeneral: "general",
		TaskNews:    "news",
		TaskDocs:    "docs",
		TaskExtract: "extract",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("expected %q got %q", want, got)
		}
	}
}

func TestTaskTypesReturnsAll(t *testing.T) {
	types := TaskTypes()
	if len(types) != 12 {
		t.Fatalf("expected 12 task types, got %d", len(types))
	}
	seen := map[TaskType]bool{}
	for _, tt := range types {
		if seen[tt] {
			t.Fatalf("duplicate task type %q", tt)
		}
		seen[tt] = true
	}
}

func TestTaskDescriptionKnownTypes(t *testing.T) {
	for _, tt := range TaskTypes() {
		desc := TaskDescription(tt)
		if desc == "" || desc == "unknown task type" {
			t.Errorf("expected meaningful description for %q, got %q", tt, desc)
		}
	}
}

func TestTaskDescriptionUnknownType(t *testing.T) {
	desc := TaskDescription(TaskType("nonexistent"))
	if desc != "unknown task type" {
		t.Errorf("expected unknown task type, got %q", desc)
	}
}

func TestProviderInterface(t *testing.T) {
	var p Provider = fakeProvider{name: "fake"}
	if p.Name() != "fake" {
		t.Fatalf("expected provider name fake, got %q", p.Name())
	}
}
