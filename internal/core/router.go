package core

type RouteMatrix map[TaskType][]string

func DefaultRouteMatrix() RouteMatrix {
	// Route ordering is based on historical sanitized evidence plus provider
	// capability contracts. Treat this as a routing prior, not as proof of current
	// live-web quality or provider ranking. See docs/BENCHMARKS.md and
	// docs/ROUTE-EVIDENCE.md; do not reorder providers without new evidence.
	return RouteMatrix{
		// Search tasks
		TaskGeneral:   {"brave", "firecrawl", "ddgs", "tavily"},
		TaskNews:      {"brave", "ddgs", "tavily", "firecrawl"},
		TaskDocs:      {"brave", "firecrawl", "tavily", "ddgs"},
		TaskAcademic:  {"brave", "tavily", "firecrawl", "ddgs"},
		TaskFactcheck: {"brave", "tavily", "ddgs", "firecrawl"},
		TaskSemantic:  {"tavily", "firecrawl", "brave", "ddgs"},
		TaskCode:      {"brave", "firecrawl", "ddgs", "tavily"},
		TaskSocial:    {"firecrawl", "ddgs", "brave", "tavily"},
		TaskPeople:    {"tavily", "firecrawl", "brave", "ddgs"},
		TaskPricing:   {"brave", "tavily", "firecrawl", "ddgs"},
		TaskResearch:  {"brave", "firecrawl", "ddgs", "tavily"},
		// Extract tasks: only providers with extraction capability. Brave/DDGS are
		// intentionally excluded even if URL-query benchmark scores are high.
		// Scrapling is a local keyless fallback; keep the evidence-backed remote
		// extraction order until local quality evidence supports reordering.
		TaskExtract: {"tavily", "firecrawl", "scrapling"},
	}
}

type Router struct {
	registry *Registry
	ledger   QuotaLedger
	matrix   RouteMatrix
}

func NewRouter(registry *Registry, ledger QuotaLedger, matrix RouteMatrix) *Router {
	return &Router{registry: registry, ledger: ledger, matrix: matrix}
}

func (r *Router) Select(task TaskType, capability Capability) (Provider, []string, error) {
	if task == "" {
		task = TaskGeneral
	}
	route := r.matrix[task]
	if len(route) == 0 {
		route = r.matrix[TaskGeneral]
	}
	seen := make([]string, 0, len(route))
	for _, name := range route {
		seen = append(seen, name)
		provider, ok := r.registry.Get(name)
		if !ok {
			continue
		}
		if !HasCapability(provider.Capabilities(), capability) {
			continue
		}
		if !r.ledger.Decide(name).Allowed {
			continue
		}
		return provider, append([]string(nil), route...), nil
	}
	return nil, append([]string(nil), route...), NoFreeQuotaError{Task: task, Provider: seen}
}
