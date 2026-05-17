package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dorukardahan/nole/internal/safenet"
)

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
	route := s.routeFor(req.Task)
	trace := make([]RouteAttempt, 0, len(route))
	for _, name := range route {
		provider, ok := s.registry.Get(name)
		if !ok {
			trace = append(trace, skippedAttempt(name, "not_registered"))
			continue
		}
		if !HasCapability(provider.Capabilities(), CapabilitySearch) {
			trace = append(trace, skippedAttempt(name, "missing_search_capability"))
			continue
		}
		if !s.ledger.Allow(name) {
			trace = append(trace, skippedAttempt(name, "quota_blocked"))
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			reason := status.Reason
			if reason == "" {
				reason = "provider_unavailable"
			}
			trace = append(trace, skippedAttempt(name, reason))
			continue
		}
		start := time.Now()
		resp, err := provider.Search(ctx, req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = err
			trace = append(trace, RouteAttempt{Provider: name, Status: "failed", Reason: "provider_error", LatencyMS: latency})
			continue
		}
		_ = s.ledger.Record(name)
		resp.Query = req.Query
		resp.Task = req.Task
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		if len(resp.Results) == 0 {
			lastErr = fmt.Errorf("search provider %s returned empty results", name)
			trace = append(trace, RouteAttempt{Provider: name, Status: "failed", Reason: "empty_results", LatencyMS: latency, ResultCount: 0})
			continue
		}
		trace = append(trace, RouteAttempt{Provider: name, Status: "success", Reason: "success", LatencyMS: latency, ResultCount: len(resp.Results)})
		resp.RouteTrace = trace
		return resp, nil
	}
	if lastErr != nil {
		return SearchResponse{Route: append([]string(nil), route...), RouteTrace: trace}, lastErr
	}
	return SearchResponse{Route: append([]string(nil), route...), RouteTrace: trace}, NoFreeQuotaError{Task: req.Task, Provider: route}
}

func (s *Service) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	if err := safenet.ValidateURL(req.URL); err != nil {
		return ExtractResponse{}, fmt.Errorf("url validation: %w", err)
	}
	route := s.routeFor(TaskExtract)
	trace := make([]RouteAttempt, 0, len(route))
	var lastErr error
	for _, name := range route {
		provider, ok := s.registry.Get(name)
		if !ok {
			trace = append(trace, skippedAttempt(name, "not_registered"))
			continue
		}
		if !HasCapability(provider.Capabilities(), CapabilityExtract) {
			trace = append(trace, skippedAttempt(name, "missing_extract_capability"))
			continue
		}
		if !s.ledger.Allow(name) {
			trace = append(trace, skippedAttempt(name, "quota_blocked"))
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			reason := status.Reason
			if reason == "" {
				reason = "provider_unavailable"
			}
			trace = append(trace, skippedAttempt(name, reason))
			continue
		}
		start := time.Now()
		resp, err := provider.Extract(ctx, req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = err
			trace = append(trace, RouteAttempt{Provider: name, Status: "failed", Reason: "provider_error", LatencyMS: latency})
			continue
		}
		_ = s.ledger.Record(name)
		resp.URL = req.URL
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		resultCount := contentResultCount(resp.Content)
		if strings.TrimSpace(resp.Content) == "" {
			lastErr = fmt.Errorf("extract provider %s returned empty content", name)
			trace = append(trace, RouteAttempt{Provider: name, Status: "failed", Reason: "empty_content", LatencyMS: latency, ResultCount: resultCount})
			continue
		}
		trace = append(trace, RouteAttempt{Provider: name, Status: "success", Reason: "success", LatencyMS: latency, ResultCount: resultCount})
		resp.RouteTrace = trace
		return resp, nil
	}
	if lastErr != nil {
		return ExtractResponse{URL: req.URL, Route: append([]string(nil), route...), RouteTrace: trace}, lastErr
	}
	return ExtractResponse{URL: req.URL, Route: append([]string(nil), route...), RouteTrace: trace}, NoFreeQuotaError{Task: TaskExtract, Provider: route}
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

func (s *Service) routeFor(task TaskType) []string {
	route := s.router.matrix[task]
	if len(route) == 0 {
		route = s.router.matrix[TaskGeneral]
	}
	return route
}

func skippedAttempt(provider, reason string) RouteAttempt {
	return RouteAttempt{Provider: provider, Status: "skipped", Reason: reason}
}

func contentResultCount(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}
	return 1
}
