package core

type RouteMatrix map[TaskType][]string

func DefaultRouteMatrix() RouteMatrix {
	// Route ordering is based on historical sanitized evidence plus provider
	// capability contracts. Treat this as a routing prior, not as proof of current
	// live-web quality or provider ranking. See docs/BENCHMARKS.md and
	// docs/ROUTE-EVIDENCE.md; do not reorder providers without new evidence.
	return RouteMatrix{
		// Search tasks. "wikipedia" (keyless MediaWiki) reinforces the
		// encyclopedic routes — factcheck/people/academic — placed before "ddgs"
		// so it is tried before the last-resort general fallback but never
		// displaces it. It is deliberately NOT in TaskGeneral (that would make it
		// a general fallback) nor in any other route. "arxiv" (keyless arXiv Atom
		// API) is the academic-only analogue: a primary-source scholarly-preprint
		// reinforcement on TaskAcademic ONLY, placed just before "wikipedia" (so a
		// keyless academic query reaches papers first, then the encyclopedic
		// overview, then ddgs). It is NOT in any other route. Both keyless
		// reinforcements sit after the keyed remotes and before the ddgs backstop;
		// an empty/error response from either falls through (see service.go).
		TaskGeneral:   {"brave", "tavily", "firecrawl", "ddgs"},
		TaskNews:      {"firecrawl", "tavily", "brave", "ddgs"},
		TaskDocs:      {"firecrawl", "brave", "tavily", "ddgs"},
		TaskAcademic:  {"tavily", "firecrawl", "brave", "arxiv", "wikipedia", "ddgs"},
		TaskFactcheck: {"firecrawl", "tavily", "brave", "wikipedia", "ddgs"},
		TaskSemantic:  {"tavily", "brave", "firecrawl", "ddgs"},
		TaskCode:      {"tavily", "firecrawl", "brave", "ddgs"},
		TaskSocial:    {"firecrawl", "tavily", "brave", "ddgs"},
		TaskPeople:    {"firecrawl", "brave", "tavily", "wikipedia", "ddgs"},
		TaskPricing:   {"firecrawl", "brave", "tavily", "ddgs"},
		TaskResearch:  {"firecrawl", "tavily", "brave", "ddgs"},
		// Extract tasks: only providers with extraction capability. Brave/DDGS are
		// intentionally excluded even if URL-query benchmark scores are high.
		// Scrapling is local and keyless when configured. Service-level status
		// checks skip it automatically when the local runtime is unavailable.
		// "httpfetch" is the keyless pure-Go LAST-RESORT backstop (the extract-side
		// analogue of DDGS on the search routes): placed last so it is reached only
		// when Scrapling is unavailable AND the keyed remotes (firecrawl/tavily) are
		// unregistered/blocked/exhausted. It runs no JavaScript, so it is weaker on
		// SPA pages — but it makes extract work with zero keys and zero setup.
		TaskExtract: {"scrapling", "firecrawl", "tavily", "httpfetch"},
	}
}

type Router struct {
	registry *Registry
	ledger   QuotaLedger
	matrix   RouteMatrix
}

// RouteCandidate is one provider slot in a task route after static routing gates
// have been evaluated. Service still owns provider Status(), provider calls,
// quota Record(), cache, and rich runtime tracing; the router owns route
// fallback plus registration/capability/quota eligibility so Select and Service
// cannot drift on those gates.
type RouteCandidate struct {
	Name            string
	Provider        Provider
	Decision        QuotaDecision
	DecisionChecked bool
	Routable        bool
	SkipReason      string
}

func NewRouter(registry *Registry, ledger QuotaLedger, matrix RouteMatrix) *Router {
	return &Router{registry: registry, ledger: ledger, matrix: matrix}
}

// Route returns the configured route for a task, falling back to TaskGeneral.
// The returned slice is a defensive copy so callers cannot mutate the matrix.
func (r *Router) Route(task TaskType) []string {
	if task == "" {
		task = TaskGeneral
	}
	route := r.matrix[task]
	if len(route) == 0 {
		route = r.matrix[TaskGeneral]
	}
	return append([]string(nil), route...)
}

// Candidates returns the task route annotated with registration, capability, and
// quota-policy gate results. It deliberately does not call Provider.Status() or
// execute provider requests; those are runtime concerns handled by Service.
func (r *Router) Candidates(task TaskType, capability Capability) []RouteCandidate {
	route := r.Route(task)
	candidates := make([]RouteCandidate, 0, len(route))
	for _, name := range route {
		candidates = append(candidates, r.Candidate(name, capability))
	}
	return candidates
}

// Candidate evaluates one provider slot. It is intentionally single-slot so
// Service and Select can preserve the old lazy behavior: quota decisions are made
// only for providers actually reached before a success/fatal exit.
func (r *Router) Candidate(name string, capability Capability) RouteCandidate {
	candidate := RouteCandidate{Name: name}
	provider, ok := r.registry.Get(name)
	if !ok {
		candidate.SkipReason = "not_registered"
		return candidate
	}
	candidate.Provider = provider
	if !HasCapability(provider.Capabilities(), capability) {
		candidate.SkipReason = missingCapabilityReason(capability)
		return candidate
	}
	decision := r.ledger.Decide(name)
	candidate.Decision = decision
	candidate.DecisionChecked = true
	if !decision.Allowed {
		candidate.SkipReason = decision.Reason
		return candidate
	}
	candidate.Routable = true
	return candidate
}

func (r *Router) Select(task TaskType, capability Capability) (Provider, []string, error) {
	if task == "" {
		task = TaskGeneral
	}
	route := r.Route(task)
	for _, name := range route {
		candidate := r.Candidate(name, capability)
		if candidate.Routable {
			return candidate.Provider, route, nil
		}
	}
	return nil, route, NoFreeQuotaError{Task: task, Provider: route}
}

func missingCapabilityReason(capability Capability) string {
	switch capability {
	case CapabilitySearch:
		return "missing_search_capability"
	case CapabilityExtract:
		return "missing_extract_capability"
	case CapabilityStatus:
		return "missing_status_capability"
	default:
		return "missing_capability"
	}
}
