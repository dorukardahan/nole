package core

type RouteMatrix map[TaskType][]string

func DefaultRouteMatrix() RouteMatrix {
	// Evidence-based routing from benchmark 2026-05-12.
	// Brave excluded from benchmark script (curl parsing issue) but confirmed working via CLI.
	// Scores: tavily ~100, firecrawl ~98-100, ddgs ~95-100, jina ~38-85 (slow).
	return RouteMatrix{
		// Search tasks
		TaskGeneral:   {"tavily", "brave", "firecrawl", "ddgs"},
		TaskNews:      {"ddgs", "firecrawl", "tavily", "brave"},
		TaskDocs:      {"tavily", "firecrawl", "brave", "ddgs"},
		TaskAcademic:  {"tavily", "firecrawl", "brave", "ddgs"},
		TaskFactcheck: {"tavily", "firecrawl", "ddgs", "brave"},
		TaskSemantic:  {"tavily", "firecrawl", "brave", "ddgs"},
		TaskCode:      {"tavily", "ddgs", "firecrawl", "brave"},
		TaskSocial:    {"tavily", "firecrawl", "ddgs", "brave"},
		TaskPeople:    {"firecrawl", "tavily", "brave", "ddgs"},
		TaskPricing:   {"firecrawl", "tavily", "ddgs", "brave"},
		TaskResearch:  {"tavily", "ddgs", "firecrawl", "brave"},
		// Extract tasks
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
