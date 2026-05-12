package core

type RouteMatrix map[TaskType][]string

func DefaultRouteMatrix() RouteMatrix {
	return RouteMatrix{
		TaskGeneral:  {"brave", "tavily", "ddgs"},
		TaskNews:     {"brave", "tavily", "ddgs"},
		TaskDocs:     {"brave", "firecrawl", "jina", "ddgs"},
		TaskResearch: {"tavily", "brave", "jina", "firecrawl", "ddgs"},
		TaskExtract:  {"jina", "firecrawl"},
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
