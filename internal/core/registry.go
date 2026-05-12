package core

import (
	"fmt"
	"sort"
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("provider is nil")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("provider name is empty")
	}
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = p
	return nil
}

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) List() []Provider {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Provider, 0, len(names))
	for _, name := range names {
		out = append(out, r.providers[name])
	}
	return out
}
