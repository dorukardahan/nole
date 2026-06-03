package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/dorukardahan/nole/internal/nolelog"
	"github.com/dorukardahan/nole/internal/safeerr"
	"github.com/dorukardahan/nole/internal/safenet"
)

// maxSearchLimit caps the requested result count across every entry point
// (MCP, HTTP, CLI). It matches Brave's documented per-request maximum, so a
// caller asking for more never produces a guaranteed provider 422; a value
// <= 0 falls back to a sensible default instead of leaking through to a
// provider as "no limit".
const maxSearchLimit = 20

// maxExtractTop caps how many top search results SearchAndExtract will also
// extract in a single call, keeping the combined response context-bounded.
const maxExtractTop = 3

type Service struct {
	registry *Registry
	ledger   QuotaLedger
	router   *Router
	cache    ResponseCache
	// sfSearch/sfExtract collapse concurrent identical requests into a single
	// upstream fetch and a single quota debit (cache-miss stampede guard). The
	// zero value is ready to use; keys match the cache keys so coalescing and
	// caching agree on request identity.
	sfSearch  singleflight.Group
	sfExtract singleflight.Group
	// log receives the gateway's diagnostic events (e.g. a non-fatal research
	// step failure). It is always written to stderr at the call site (never
	// stdout) and is secret-safe by construction. A nil log is a safe no-op, so
	// a Service built without WithLogger simply stays silent.
	log *nolelog.Logger
}

type ServiceOption func(*Service)

func WithResponseCache(cache ResponseCache) ServiceOption {
	return func(s *Service) {
		s.cache = cache
	}
}

// WithLogger injects the structured diagnostic logger. Mirrors
// WithResponseCache: an optional dependency the CLI wires from
// nolelog.FromEnv(os.Stderr). core depends on nolelog one-directionally
// (nolelog imports neither core nor cli), so there is no import cycle.
func WithLogger(log *nolelog.Logger) ServiceOption {
	return func(s *Service) {
		s.log = log
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
	// Resolve the task as the very first step: an explicitly-supplied known task
	// wins; otherwise the deterministic planner classifies the query so the
	// curated task→provider matrix actually fires instead of everything
	// defaulting to general. `source` is observability only — the planner is a
	// static keyword table, not intelligence.
	task, source := resolveTask(req)
	req.Task = task
	if req.Limit <= 0 {
		req.Limit = 5
	} else if req.Limit > maxSearchLimit {
		req.Limit = maxSearchLimit
	}
	route := s.routeFor(req.Task)
	// A caller that is already cancelled does no work and surfaces its own
	// cancellation immediately.
	if err := ctx.Err(); err != nil {
		return cancelledSearchResponse(req, route, source), err
	}
	if s.cache != nil {
		if cached, ok := s.cache.GetSearch(req); ok {
			cached.Query = req.Query
			cached.Task = req.Task
			// Resolution happens before the cache key (which keys on the resolved
			// task), so an omitted-task and an explicit-task caller can share one
			// entry via different sources. Overwrite with THIS call's source so it
			// stays honest, mirroring the Query/Task overwrite above.
			cached.TaskSource = source
			if len(cached.Route) == 0 {
				cached.Route = append([]string(nil), route...)
			}
			cached.RouteTrace = []RouteAttempt{cacheHitAttempt(cached.Provider, len(cached.Results))}
			cached.RoutingInsight = BuildSearchRoutingInsight(cached)
			return cached, nil
		}
	}
	// Collapse concurrent identical queries into one upstream fetch + one quota
	// debit (browser retries, client fan-out, or an abusive caller on a widened
	// --listen would otherwise each miss the cache and each debit the free
	// tier). Two correctness requirements drive the shape:
	//   - DoChan + a per-caller select: each caller observes ITS OWN
	//     cancellation, so one client disconnecting never fails its peers.
	//   - context.WithoutCancel for the shared fetch: a leaving/flaky leader (or
	//     one with a tighter deadline) must not poison live followers with its
	//     cancellation/deadline. The detached fetch stays bounded by the
	//     per-provider HTTP client timeouts. singleflight drops the key once the
	//     fetch returns, so distinct/sequential queries are never coalesced.
	key := searchCacheKey(req)
	ch := s.sfSearch.DoChan(key, func() (any, error) {
		return s.searchUncached(context.WithoutCancel(ctx), req, route, source)
	})
	select {
	case <-ctx.Done():
		return cancelledSearchResponse(req, route, source), ctx.Err()
	case res := <-ch:
		resp := res.Val.(SearchResponse)
		resp.Query = req.Query
		resp.Task = req.Task
		resp.TaskSource = source
		// A follower coalesced onto a leader with the same resolved task but a
		// different source receives the leader's response, including its
		// already-built insight. Rebuild it so the "(task detected/default)"
		// qualifier matches THIS caller's TaskSource. Skip on the error path: its
		// insight (BuildErrorRoutingInsight) carries no source qualifier, so there
		// is nothing to reconcile.
		if res.Err == nil {
			resp.RoutingInsight = BuildSearchRoutingInsight(resp)
		}
		return resp, res.Err
	}
}

// resolveTask decides the task a search will route on, and how that task was
// chosen. An explicitly-supplied known search task is honored verbatim
// (supplied); otherwise — empty, unknown, or the extract key on a search call —
// the deterministic planner classifies the query (detected when it finds a
// signal, default when it falls back to general). Leniency lives here: a bogus
// task never errors, it just classifies. The planner is pure keyword matching,
// so this stays deterministic.
func resolveTask(req SearchRequest) (TaskType, TaskSource) {
	if IsKnownSearchTask(req.Task) {
		return req.Task, TaskSourceSupplied
	}
	classification := ClassifyQuery(req.Query, PlanOptions{})
	if classification.PrimaryTask != TaskGeneral {
		return classification.PrimaryTask, TaskSourceDetected
	}
	return TaskGeneral, TaskSourceDefault
}

// cancelledSearchResponse builds the minimal response returned when the caller's
// context is already done, so callers can still read Query/Task/Route/TaskSource.
func cancelledSearchResponse(req SearchRequest, route []string, source TaskSource) SearchResponse {
	resp := SearchResponse{Query: req.Query, Task: req.Task, TaskSource: source, Route: append([]string(nil), route...)}
	resp.RoutingInsight = BuildErrorRoutingInsight("search", resp.Route, nil)
	return resp
}

func (s *Service) searchUncached(ctx context.Context, req SearchRequest, route []string, source TaskSource) (SearchResponse, error) {
	var lastErr error
	trace := make([]RouteAttempt, 0, len(route)+1)
	if s.cache != nil {
		trace = append(trace, cacheMissAttempt())
	}
	// This walk runs on a context detached from any single caller (see Search):
	// caller-facing cancellation is handled at the wrapper boundary via DoChan +
	// select, so the walk completes for the benefit of all coalesced callers and
	// the cache, bounded by per-provider client timeouts.
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
			// Drift: the provider rejected this as over-quota (429) while our
			// local counter still showed room. Record an advisory signal (no
			// debit, no route change). This is a best-effort EARLY signal —
			// once repeated 429s trip the breaker, calls short-circuit with
			// ErrCircuitOpen (not a 429) and drift stops firing by design.
			if isQuotaExhausted(err) && decision.FreeRemaining > 0 {
				s.ledger.RecordDrift(name, "provider returned 429 while local free_remaining > 0")
			}
			trace = append(trace, attemptWithDecision(name, "failed", "provider_error", decision, latency, 0))
			continue
		}
		resp.Query = req.Query
		resp.Task = req.Task
		resp.TaskSource = source
		resp.Provider = provider.Name()
		resp.Route = append([]string(nil), route...)
		if len(resp.Results) == 0 {
			lastErr = fmt.Errorf("search provider %s returned empty results", name)
			trace = append(trace, attemptWithDecision(name, "failed", "empty_results", decision, latency, 0))
			continue
		}
		// Surface the provider's freshness signal in date order for recency tasks
		// (news/factcheck). Pure pass-through reorder of provider-supplied dates —
		// never drops/filters/judges, never touches Score. Run once here so the
		// cached slice and the returned slice are identical.
		applyRecencySort(req.Task, resp.Results)
		// Only debit quota on a successful response. Recording before the
		// provider call (the prior shape) burned free-tier quota on
		// transient outages, 5xx responses, empty results and invalid keys,
		// which became user-visible once BYOK keys started defaulting to
		// free-tier-BYOK instead of premium-capable.
		//
		// If Record() itself fails here (TOCTOU race with another process
		// consuming the last slot between Decide and Record, or the ledger
		// becoming unavailable mid-call), the upstream provider request has
		// already happened — discarding the response would deny the user a
		// result they effectively paid for upstream. Return the response
		// and surface the bookkeeping miss via the trace reason so
		// observability sees it. Local quota may overshoot by one slot per
		// race, bounded by the file lock taken inside Record.
		traceReason := "success"
		if err := s.ledger.Record(name); err != nil {
			refreshed := s.ledger.Decide(name)
			suffix := refreshed.Reason
			if suffix == "" {
				suffix = "quota_record_failed"
			}
			traceReason = "success_" + suffix
		}
		trace = append(trace, attemptWithDecision(name, "success", traceReason, decision, latency, len(resp.Results)))
		resp.RouteTrace = trace
		resp.RoutingInsight = BuildSearchRoutingInsight(resp)
		if s.cache != nil {
			s.cache.SetSearch(req, resp)
		}
		return resp, nil
	}
	if lastErr != nil {
		resp := SearchResponse{Query: req.Query, Task: req.Task, TaskSource: source, Route: append([]string(nil), route...), RouteTrace: trace}
		resp.RoutingInsight = BuildErrorRoutingInsight("search", resp.Route, resp.RouteTrace)
		return resp, lastErr
	}
	resp := SearchResponse{Query: req.Query, Task: req.Task, TaskSource: source, Route: append([]string(nil), route...), RouteTrace: trace}
	resp.RoutingInsight = BuildErrorRoutingInsight("search", resp.Route, resp.RouteTrace)
	return resp, NoFreeQuotaError{Task: req.Task, Provider: route}
}

func (s *Service) Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error) {
	req.URL = strings.TrimSpace(req.URL)
	if req.Format == "" {
		req.Format = "markdown"
	}
	if err := safenet.ValidateURLContext(ctx, req.URL); err != nil {
		return ExtractResponse{}, fmt.Errorf("url validation: %w", err)
	}
	route := s.routeFor(TaskExtract)
	if err := ctx.Err(); err != nil {
		return cancelledExtractResponse(req, route), err
	}
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
	}
	// See Search for the DoChan + context.WithoutCancel rationale: each caller
	// observes its own cancellation, and a leaving leader cannot poison live
	// followers coalesced on the same extract.
	key := extractCacheKey(req)
	ch := s.sfExtract.DoChan(key, func() (any, error) {
		return s.extractUncached(context.WithoutCancel(ctx), req, route)
	})
	select {
	case <-ctx.Done():
		return cancelledExtractResponse(req, route), ctx.Err()
	case res := <-ch:
		resp := res.Val.(ExtractResponse)
		resp.URL = req.URL
		return resp, res.Err
	}
}

// SearchAndExtract runs a search, then extracts the top ExtractTop result URLs in
// a single call — the combined "search then read the top hit" primitive. Each
// extract reuses Service.Extract, so the SSRF preflight, cache, quota debit, and
// routing all apply per URL. A per-URL extract failure is non-fatal and recorded
// in ExtractErrors; only a search failure (or caller cancellation) aborts. URLs
// are de-duplicated so a repeated result never double-debits the extract quota.
func (s *Service) SearchAndExtract(ctx context.Context, req SearchAndExtractRequest) (SearchAndExtractResponse, error) {
	n := req.ExtractTop
	if n <= 0 {
		n = 1
	}
	if n > maxExtractTop {
		n = maxExtractTop
	}

	searchResp, err := s.Search(ctx, SearchRequest{Query: req.Query, Task: req.Task, Limit: req.Limit})
	if err != nil {
		// A search failure (or cancellation surfaced by Search) is fatal — there
		// is nothing to extract.
		return SearchAndExtractResponse{Search: searchResp}, err
	}

	out := SearchAndExtractResponse{Search: searchResp}
	seen := make(map[string]bool)
	attempted := 0
	for _, r := range searchResp.Results {
		url := strings.TrimSpace(r.URL)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		er, eerr := s.Extract(ctx, ExtractRequest{URL: url, Format: "markdown"})
		if eerr != nil {
			// Surface caller cancellation immediately; never bury a Ctrl-C as a
			// per-URL "extract error" on an otherwise-successful response.
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			out.ExtractErrors = append(out.ExtractErrors, ExtractError{URL: url, Error: safeerr.Message(eerr)})
		} else {
			out.Extracts = append(out.Extracts, er)
		}
		attempted++
		if attempted >= n {
			break
		}
	}
	return out, nil
}

func cancelledExtractResponse(req ExtractRequest, route []string) ExtractResponse {
	resp := ExtractResponse{URL: req.URL, Route: append([]string(nil), route...)}
	resp.RoutingInsight = BuildErrorRoutingInsight("extract", resp.Route, nil)
	return resp
}

func (s *Service) extractUncached(ctx context.Context, req ExtractRequest, route []string) (ExtractResponse, error) {
	trace := make([]RouteAttempt, 0, len(route)+1)
	if s.cache != nil {
		trace = append(trace, cacheMissAttempt())
	}
	var lastErr error
	// See searchUncached: runs on a detached context; caller cancellation is
	// handled at the Extract wrapper boundary.
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
			if isQuotaExhausted(err) && decision.FreeRemaining > 0 {
				s.ledger.RecordDrift(name, "provider returned 429 while local free_remaining > 0")
			}
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
		// path above for the rationale, including why a failed Record() does
		// not discard the response.
		traceReason := "success"
		if err := s.ledger.Record(name); err != nil {
			refreshed := s.ledger.Decide(name)
			suffix := refreshed.Reason
			if suffix == "" {
				suffix = "quota_record_failed"
			}
			traceReason = "success_" + suffix
		}
		trace = append(trace, attemptWithDecision(name, "success", traceReason, decision, latency, resultCount))
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

func (s *Service) ProviderStatus(ctx context.Context) ProviderStatusResponse {
	providers := s.registry.List()
	statuses := make([]ProviderStatus, 0, len(providers))
	configured := make(map[string]bool)

	byokNames := make(map[string]bool, len(byokProviders))
	for _, b := range byokProviders {
		byokNames[b.Name] = true
	}

	// Recent drift signals, keyed by provider, so each status can carry a
	// DriftWarning. Read once (a single ledger lock) before the per-provider
	// loop; aging is already applied by BudgetStatus.
	driftByProvider := map[string]DriftSignal{}
	for _, sig := range s.ledger.BudgetStatus().DriftSignals {
		driftByProvider[sig.Provider] = sig
	}

	// extractAvailable: does URL extraction work AT ALL right now? True when any
	// available registered provider advertises extract — including the always-on
	// keyless httpfetch backstop. Drives whether a missing keyed extract provider is
	// pitched as a disabled-feature unlock (false) or a fidelity upgrade (true).
	extractAvailable := false
	for _, provider := range providers {
		status := provider.Status(ctx) // called once per provider
		if byokNames[provider.Name()] {
			configured[provider.Name()] = status.Available
		}
		if status.Available && HasCapability(status.Capabilities, CapabilityExtract) {
			extractAvailable = true
		}
		merged := mergeProviderCostStatus(status, s.ledger.Decide(provider.Name()))
		if sig, ok := driftByProvider[provider.Name()]; ok {
			merged.DriftWarning = sig.Reason
		}
		statuses = append(statuses, merged)
	}
	suggestions := BuildSetupSuggestions(configured, extractAvailable)
	return ProviderStatusResponse{
		Providers:        statuses,
		SetupSuggestions: suggestions,
	}
}

// HasExtractCapableProvider reports whether the registry contains a provider that
// advertises CapabilityExtract. The MCP server uses it to decide whether to
// advertise the extract / search_and_extract tools.
//
// It is intentionally a CAPABILITY check, NOT a live-health check: it never calls
// Status(). Tool advertisement is a registration-time surface decision and must
// not (a) execute a provider's Status() — e.g. Scrapling launches a Python
// subprocess — on every `nole mcp` startup, nor (b) make the advertised tool set
// flap with transient provider health (a breaker-open keyed extractor must not
// un-advertise extract when the keyless httpfetch backstop still serves it). With
// httpfetch unconditionally registered by the default service, an extract-capable
// provider is always present, so extract is advertised out of the box; the live
// route walk (with its own per-provider status/quota checks) decides which
// provider actually serves each call.
func (s *Service) HasExtractCapableProvider() bool {
	for _, p := range s.registry.List() {
		if HasCapability(p.Capabilities(), CapabilityExtract) {
			return true
		}
	}
	return false
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
