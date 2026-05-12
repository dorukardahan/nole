package core

type RouteMatrix map[TaskType][]string

func DefaultRouteMatrix() RouteMatrix {
	// Evidence-based routing from benchmark 2026-05-12 v2.
	// 12 categories x 5 providers x 2 queries, Brave URL encoding bug fixed.
	// Search findings: Brave is strongest broad/default provider; Firecrawl is
	// strong on social/code/semantic and fast; Tavily leads semantic/people;
	// DDGS is excellent keyless fallback; Jina search is slower/weaker but useful
	// for reader/extraction fallback.
	return RouteMatrix{
		// Search tasks
		TaskGeneral:   {"brave", "firecrawl", "ddgs", "tavily", "jina"},
		TaskNews:      {"brave", "ddgs", "tavily", "firecrawl", "jina"},
		TaskDocs:      {"brave", "firecrawl", "tavily", "ddgs", "jina"},
		TaskAcademic:  {"brave", "tavily", "firecrawl", "ddgs", "jina"},
		TaskFactcheck: {"brave", "tavily", "ddgs", "firecrawl", "jina"},
		TaskSemantic:  {"tavily", "firecrawl", "brave", "ddgs", "jina"},
		TaskCode:      {"brave", "firecrawl", "ddgs", "tavily", "jina"},
		TaskSocial:    {"firecrawl", "ddgs", "brave", "tavily", "jina"},
		TaskPeople:    {"tavily", "firecrawl", "brave", "ddgs", "jina"},
		TaskPricing:   {"brave", "tavily", "firecrawl", "ddgs", "jina"},
		TaskResearch:  {"brave", "firecrawl", "ddgs", "tavily", "jina"},
		// Extract tasks: only providers with extraction capability. Brave/DDGS are
		// intentionally excluded even if URL-query benchmark scores are high.
		TaskExtract: {"tavily", "firecrawl", "jina"},
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
		if !r.ledger.Allow(name) {
			continue
		}
		return provider, append([]string(nil), route...), nil
	}
	return nil, append([]string(nil), route...), NoFreeQuotaError{Task: task, Provider: seen}
}
