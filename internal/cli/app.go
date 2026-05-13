package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/brave"
	"github.com/dorukardahan/nole/internal/providers/ddgs"
	"github.com/dorukardahan/nole/internal/providers/firecrawl"
	"github.com/dorukardahan/nole/internal/providers/jina"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/dorukardahan/nole/internal/providers/tavily"
)

func defaultService() *core.Service {
	registry := core.NewRegistry()

	jinaKey := os.Getenv("JINA_API_KEY")
	firecrawlKey := os.Getenv("FIRECRAWL_API_KEY")
	braveKey := os.Getenv("BRAVE_API_KEY")
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_SEARCH_API_KEY")
	}
	tavilyKey := os.Getenv("TAVILY_API_KEY")

	// Jina — real adapter (search + extract)
	if jinaKey != "" {
		_ = registry.Register(jina.New(jina.WithAPIKey(jinaKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("jina"))
	}

	// Firecrawl — real adapter (search + extract)
	if firecrawlKey != "" {
		_ = registry.Register(firecrawl.New(firecrawl.WithAPIKey(firecrawlKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("firecrawl"))
	}

	// Brave — real adapter (search only)
	if braveKey != "" {
		_ = registry.Register(brave.New(brave.WithAPIKey(braveKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("brave"))
	}

	// Tavily — real adapter (search + extract)
	if tavilyKey != "" {
		_ = registry.Register(tavily.New(tavily.WithAPIKey(tavilyKey)))
	} else {
		_ = registry.Register(mock.NewUnavailable("tavily"))
	}

	// DDGS — keyless free, always available
	_ = registry.Register(ddgs.New())

	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "brave", Unknown: true})
	ledger.Set(core.QuotaEntry{Provider: "tavily", Unknown: true})
	ledger.Set(core.QuotaEntry{Provider: "jina", Unknown: true})
	ledger.Set(core.QuotaEntry{Provider: "firecrawl", Unknown: true})
	ledger.Set(core.QuotaEntry{Provider: "ddgs", KeylessFree: true, Unknown: true})

	return core.NewService(registry, ledger, core.DefaultRouteMatrix())
}

func writeJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func parseTask(raw string) core.TaskType {
	switch raw {
	case "news":
		return core.TaskNews
	case "docs", "technical-docs":
		return core.TaskDocs
	case "academic":
		return core.TaskAcademic
	case "factcheck":
		return core.TaskFactcheck
	case "semantic":
		return core.TaskSemantic
	case "code":
		return core.TaskCode
	case "social":
		return core.TaskSocial
	case "people":
		return core.TaskPeople
	case "pricing":
		return core.TaskPricing
	case "extract":
		return core.TaskExtract
	case "research", "deep-research":
		return core.TaskResearch
	default:
		return core.TaskGeneral
	}
}

func runSearch(query string, task core.TaskType, limit int) (core.SearchResponse, error) {
	if query == "" {
		return core.SearchResponse{}, fmt.Errorf("query is required")
	}
	return defaultService().Search(context.Background(), core.SearchRequest{Query: query, Task: task, Limit: limit})
}
