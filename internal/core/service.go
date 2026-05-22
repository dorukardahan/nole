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
	cache    ResponseCache
}

type ServiceOption func(*Service)

func WithResponseCache(cache ResponseCache) ServiceOption {
	return func(s *Service) {
		s.cache = cache
	}
}

func NewService(registry *Registry, ledger QuotaLedger, matrix RouteMatrix, opts ...ServiceOption) *Service {
	service := &Service{registry: registry, ledger: ledger, router: NewRouter(registry, ledger, matrix)}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if req.Task == "" {
		req.Task = TaskGeneral
	}
	var lastErr error
	route := s.routeFor(req.Task)
	trace := make([]RouteAttempt, 0, len(route)+1)
	if s.cache != nil {
		if cached, ok := s.cache.GetSearch(req); ok {
			cached.Query = req.Query
			cached.Task = req.Task
			if len(cached.Route) == 0 {
				cached.Route = append([]string(nil), route...)
			}
			cached.RouteTrace = []RouteAttempt{cacheHitAttempt(cached.Provider, len(cached.Results))}
			cached.RoutingInsight = BuildSearchRoutingInsight(cached)
			return cached, nil
		}
		trace = append(trace, cacheMissAttempt())
	}
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
		decision := s.ledger.Decide(name)
		if !decision.Allowed {
			trace = append(trace, skippedAttemptWithDecision(name, decision))
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			reason := status.Reason
			if reason == "" {
				reason = "provider_unavailable"
			}
			trace = append(trace, skippedAttemptWithReasonAndDecision(name, reason, decision))
			continue
		}
		start := time.Now()
		resp, err := provider.Search(ctx, req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = err
			trace = append(trace, attemptWithDecision(name, "failed", "provider_error", decision, latency, 0))
			continue
		}
		resp.Query = req.Query
		resp.Task = req.Task
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		if len(resp.Results) == 0 {
			lastErr = fmt.Errorf("search provider %s returned empty results", name)
			trace = append(trace, attemptWithDecision(name, "failed", "empty_results", decision, latency, 0))
			continue
		}
		// Only debit quota on a successful response. Recording before the
		// provider call (the prior shape) burned free-tier quota on
		// transient outages, 5xx responses, empty results and invalid keys,
		// which became user-visible once BYOK keys started defaulting to
		// free-tier-BYOK instead of premium-capable.
		if err := s.ledger.Record(name); err != nil {
			lastErr = err
			refreshed := s.ledger.Decide(name)
			reason := refreshed.Reason
			if reason == "" {
				reason = "quota_record_failed"
			}
			trace = append(trace, skippedAttemptWithReasonAndDecision(name, reason, refreshed))
			continue
		}
		trace = append(trace, attemptWithDecision(name, "success", "success", decision, latency, len(resp.Results)))
		resp.RouteTrace = trace
		resp.RoutingInsight = BuildSearchRoutingInsight(resp)
		if s.cache != nil {
			s.cache.SetSearch(req, resp)
		}
		return resp, nil
	}
	if lastErr != nil {
		resp := SearchResponse{Query: req.Query, Task: req.Task, Route: append([]string(nil), route...), RouteTrace: trace}
		resp.RoutingInsight = BuildErrorRoutingInsight("search", resp.Route, resp.RouteTrace)
		return resp, lastErr
	}
	resp := SearchResponse{Query: req.Query, Task: req.Task, Route: append([]string(nil), route...), RouteTrace: trace}
	resp.RoutingInsight = BuildErrorRoutingInsight("search", resp.Route, resp.RouteTrace)
	return resp, NoFreeQuotaError{Task: req.Task, Provider: route}
}

func (s *Service) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	req.URL = strings.TrimSpace(req.URL)
	if req.Format == "" {
		req.Format = "markdown"
	}
	if err := safenet.ValidateURL(req.URL); err != nil {
		return ExtractResponse{}, fmt.Errorf("url validation: %w", err)
	}
	route := s.routeFor(TaskExtract)
	trace := make([]RouteAttempt, 0, len(route)+1)
	if s.cache != nil {
		if cached, ok := s.cache.GetExtract(req); ok {
			cached.URL = req.URL
			if len(cached.Route) == 0 {
				cached.Route = append([]string(nil), route...)
			}
			cached.RouteTrace = []RouteAttempt{cacheHitAttempt(cached.Provider, contentResultCount(cached.Content))}
			cached.RoutingInsight = BuildExtractRoutingInsight(cached)
			return cached, nil
		}
		trace = append(trace, cacheMissAttempt())
	}
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
		decision := s.ledger.Decide(name)
		if !decision.Allowed {
			trace = append(trace, skippedAttemptWithDecision(name, decision))
			continue
		}
		if status := provider.Status(ctx); !status.Available {
			reason := status.Reason
			if reason == "" {
				reason = "provider_unavailable"
			}
			trace = append(trace, skippedAttemptWithReasonAndDecision(name, reason, decision))
			continue
		}
		start := time.Now()
		resp, err := provider.Extract(ctx, req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = err
			trace = append(trace, attemptWithDecision(name, "failed", "provider_error", decision, latency, 0))
			continue
		}
		resp.URL = req.URL
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		resultCount := contentResultCount(resp.Content)
		if strings.TrimSpace(resp.Content) == "" {
			lastErr = fmt.Errorf("extract provider %s returned empty content", name)
			trace = append(trace, attemptWithDecision(name, "failed", "empty_content", decision, latency, resultCount))
			continue
		}
		// Only debit quota on a successful extract. See the matching Search
		// path above for the rationale.
		if err := s.ledger.Record(name); err != nil {
			lastErr = err
			refreshed := s.ledger.Decide(name)
			reason := refreshed.Reason
			if reason == "" {
				reason = "quota_record_failed"
			}
			trace = append(trace, skippedAttemptWithReasonAndDecision(name, reason, refreshed))
			continue
		}
		trace = append(trace, attemptWithDecision(name, "success", "success", decision, latency, resultCount))
		resp.RouteTrace = trace
		resp.RoutingInsight = BuildExtractRoutingInsight(resp)
		if s.cache != nil {
			s.cache.SetExtract(req, resp)
		}
		return resp, nil
	}
	if lastErr != nil {
		resp := ExtractResponse{URL: req.URL, Route: append([]string(nil), route...), RouteTrace: trace}
		resp.RoutingInsight = BuildErrorRoutingInsight("extract", resp.Route, resp.RouteTrace)
		return resp, lastErr
	}
	resp := ExtractResponse{URL: req.URL, Route: append([]string(nil), route...), RouteTrace: trace}
	resp.RoutingInsight = BuildErrorRoutingInsight("extract", resp.Route, resp.RouteTrace)
	return resp, NoFreeQuotaError{Task: TaskExtract, Provider: route}
}

func (s *Service) ProviderStatus(ctx context.Context) []ProviderStatus {
	providers := s.registry.List()
	out := make([]ProviderStatus, 0, len(providers))
	for _, provider := range providers {
		status := provider.Status(ctx)
		out = append(out, mergeProviderCostStatus(status, s.ledger.Decide(provider.Name())))
	}
	return out
}

func (s *Service) BudgetStatus() BudgetStatus {
	return s.ledger.BudgetStatus()
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

func cacheMissAttempt() RouteAttempt {
	return RouteAttempt{Provider: "cache", Status: "skipped", Reason: "cache_miss", CacheStatus: CacheStatusMiss}
}

func cacheHitAttempt(provider string, resultCount int) RouteAttempt {
	if provider == "" {
		provider = "cache"
	}
	return RouteAttempt{Provider: provider, Status: "success", Reason: "cache_hit", CacheStatus: CacheStatusHit, ResultCount: resultCount}
}

func skippedAttemptWithDecision(provider string, decision QuotaDecision) RouteAttempt {
	return skippedAttemptWithReasonAndDecision(provider, decision.Reason, decision)
}

func skippedAttemptWithReasonAndDecision(provider, reason string, decision QuotaDecision) RouteAttempt {
	attempt := attemptWithDecision(provider, "skipped", reason, decision, 0, 0)
	return attempt
}

func attemptWithDecision(provider, status, reason string, decision QuotaDecision, latency int64, resultCount int) RouteAttempt {
	return RouteAttempt{
		Provider:           provider,
		Status:             status,
		Reason:             reason,
		CostPolicy:         decision.Policy,
		CostClass:          decision.CostClass,
		LatencyMS:          latency,
		ResultCount:        resultCount,
		EstimatedCostCents: decision.EstimatedCostCents,
	}
}

func mergeProviderCostStatus(status ProviderStatus, decision QuotaDecision) ProviderStatus {
	status.CostPolicy = decision.Policy
	status.CostClass = decision.CostClass
	status.AllowedByPolicy = decision.Allowed
	status.PolicyReason = decision.Reason
	status.FreeRemaining = decision.FreeRemaining
	status.EstimatedCostCents = decision.EstimatedCostCents
	status.SpentCents = decision.SpentCents
	return status
}

func contentResultCount(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}
	return 1
}
