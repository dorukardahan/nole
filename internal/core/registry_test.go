package core

import "testing"

func TestRegistryRegistersAndRetrievesProvider(t *testing.T) {
	registry := NewRegistry()
	provider := fakeProvider{name: "fake"}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	got, ok := registry.Get("fake")
	if !ok {
		t.Fatal("expected provider to be retrievable")
	}
	if got.Name() != "fake" {
		t.Fatalf("expected fake, got %q", got.Name())
	}
}

func TestRegistryRejectsDuplicateProvider(t *testing.T) {
	registry := NewRegistry()
	provider := fakeProvider{name: "fake"}
	if err := registry.Register(provider); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	if err := registry.Register(provider); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestRegistryUnknownProvider(t *testing.T) {
	registry := NewRegistry()
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("expected missing provider to return ok=false")
	}
}
