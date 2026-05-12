package core

import "context"

type Service struct {
	registry *Registry
	ledger   QuotaLedger
	router   *Router
}

func NewService(registry *Registry, ledger QuotaLedger, matrix RouteMatrix) *Service {
	return &Service{registry: registry, ledger: ledger, router: NewRouter(registry, ledger, matrix)}
}

func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if req.Task == "" {
		req.Task = TaskGeneral
	}
	var lastErr error
	route := s.router.matrix[req.Task]
	if len(route) == 0 {
		route = s.router.matrix[TaskGeneral]
	}
	for _, name := range route {
		provider, ok := s.registry.Get(name)
		if !ok || !HasCapability(provider.Capabilities(), CapabilitySearch) || !s.ledger.Allow(name) {
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			continue
		}
		resp, err := provider.Search(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = s.ledger.Record(name)
		resp.Query = req.Query
		resp.Task = req.Task
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		return resp, nil
	}
	if lastErr != nil {
		return SearchResponse{}, lastErr
	}
	return SearchResponse{}, NoFreeQuotaError{Task: req.Task, Provider: route}
}

func (s *Service) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	route := s.router.matrix[TaskExtract]
	var lastErr error
	for _, name := range route {
		provider, ok := s.registry.Get(name)
		if !ok || !HasCapability(provider.Capabilities(), CapabilityExtract) || !s.ledger.Allow(name) {
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			continue
		}
		resp, err := provider.Extract(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = s.ledger.Record(name)
		resp.URL = req.URL
		resp.Provider = provider.Name()
		return resp, nil
	}
	if lastErr != nil {
		return ExtractResponse{}, lastErr
	}
	return ExtractResponse{}, NoFreeQuotaError{Task: TaskExtract, Provider: route}
}

func (s *Service) ProviderStatus(ctx context.Context) []ProviderStatus {
	providers := s.registry.List()
	out := make([]ProviderStatus, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Status(ctx))
	}
	return out
}

func (s *Service) BudgetStatus() BudgetStatus {
	return BudgetStatus{HardCapCents: 0, Entries: s.ledger.Entries()}
}
