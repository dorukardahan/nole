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

func TestProviderInterface(t *testing.T) {
	var p Provider = fakeProvider{name: "fake"}
	if p.Name() != "fake" {
		t.Fatalf("expected provider name fake, got %q", p.Name())
	}
}
