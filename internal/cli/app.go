package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dorukardahan/searchmcp/internal/core"
	"github.com/dorukardahan/searchmcp/internal/providers/mock"
)

func defaultService() *core.Service {
	registry := core.NewRegistry()
	_ = registry.Register(mock.New("brave"))
	_ = registry.Register(mock.New("tavily"))
	_ = registry.Register(mock.New("ddgs"))
	_ = registry.Register(mock.New("jina"))
	_ = registry.Register(mock.New("firecrawl"))

	ledger := core.NewMemoryQuotaLedger()
	ledger.Set(core.QuotaEntry{Provider: "brave", FreeRemaining: 100})
	ledger.Set(core.QuotaEntry{Provider: "tavily", FreeRemaining: 100})
	ledger.Set(core.QuotaEntry{Provider: "jina", FreeRemaining: 100})
	ledger.Set(core.QuotaEntry{Provider: "firecrawl", FreeRemaining: 100})
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
