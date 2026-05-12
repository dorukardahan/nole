# SearchMCP MVP Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Build the first MVP of `searchmcp`: a Go single-binary agent search/retrieval router with MCP stdio, CLI, task-based provider routing, and $0/free-tier fail-closed policy.

**Architecture:** One Go binary with thin front doors and a shared core service. CLI commands, MCP tools, and future HTTP handlers all call `internal/core`; provider adapters are isolated under `internal/providers`; routing uses a task-to-provider matrix and quota ledger before any provider call.

**Tech Stack:** Go, Cobra CLI, mark3labs/mcp-go for MCP stdio, stdlib `net/http`, stdlib JSON, file-backed local quota ledger, TDD with `go test ./...`.

---

## Product Decision Summary

- **Language:** Go.
- **Default surfaces:** MCP stdio + CLI.
- **Advanced surface:** HTTP/Streamable MCP later; create package boundaries now, but do not overbuild v1.
- **Distribution target:** single binary via GitHub Releases/Homebrew/Scoop later.
- **Default cost mode:** `$0 hard cap`; if no free route is available, return structured `no_free_quota`.
- **MVP providers:** mock provider for tests, DDGS placeholder fallback, Jina/Firecrawl adapter shells, Brave/Tavily adapter shells.
- **MVP behavior:** routing, quota checks, provider status, CLI search/extract, MCP search/extract/status.

## Repository Layout

```text
searchmcp/
  go.mod
  go.sum
  main.go
  README.md
  docs/plans/2026-05-12-searchmcp-mvp.md
  internal/
    cli/
      root.go
      search.go
      extract.go
      doctor.go
      providers.go
      mcp.go
    config/
      config.go
    core/
      types.go
      service.go
      router.go
      quota.go
      errors.go
      registry.go
    providers/
      mock/
        mock.go
      ddgs/
        ddgs.go
      jina/
        jina.go
      firecrawl/
        firecrawl.go
      brave/
        brave.go
      tavily/
        tavily.go
    mcpserver/
      server.go
      tools.go
    version/
      version.go
  tests/
    integration_test.go
```

## MVP Task List

### Task 1: Initialize Go module and repository skeleton

**Objective:** Create a buildable empty Go application with version command.

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `internal/version/version.go`
- Create: `internal/cli/root.go`
- Create: `README.md`

**Step 1: Write failing test**

Create `internal/cli/root_test.go`:

```go
package cli

import "testing"

func TestRootCommandExists(t *testing.T) {
    cmd := NewRootCommand()
    if cmd.Use != "searchmcp" {
        t.Fatalf("expected root command use searchmcp, got %q", cmd.Use)
    }
}
```

**Step 2: Run RED**

Run: `go test ./internal/cli -run TestRootCommandExists -v`
Expected: FAIL because `NewRootCommand` does not exist.

**Step 3: Implement minimal CLI root**

Use Cobra. Root command should set `Use: "searchmcp"`, `Short`, `SilenceUsage: true`, and `SilenceErrors: true`.

**Step 4: Run GREEN**

Run: `go test ./internal/cli -run TestRootCommandExists -v`
Expected: PASS.

---

### Task 2: Define core domain types

**Objective:** Define task types, search/extract request/response types, provider interface, and normalized result schema.

**Files:**
- Create: `internal/core/types.go`
- Test: `internal/core/types_test.go`

**Step 1: Write failing tests**

Tests should verify:
- `TaskGeneral`, `TaskNews`, `TaskDocs`, `TaskExtract` constants are stable strings.
- `SearchResult` has title/url/snippet/provider fields.
- `Provider` interface is satisfiable by a fake provider.

**Step 2: Run RED**

Run: `go test ./internal/core -run TestTaskConstants -v`
Expected: FAIL because package/types do not exist.

**Step 3: Implement minimal types**

Types:
- `TaskType string`
- `SearchRequest { Query string; Task TaskType; Limit int }`
- `ExtractRequest { URL string; Format string }`
- `SearchResult { Title, URL, Snippet, Provider string }`
- `SearchResponse { Query string; Task TaskType; Provider string; Results []SearchResult; Route []string }`
- `ExtractResponse { URL, Provider, Content string; Metadata map[string]string }`
- `Provider` interface with `Name`, `Capabilities`, `Search`, `Extract`, `Status`.

**Step 4: Run GREEN**

Run: `go test ./internal/core -v`
Expected: PASS.

---

### Task 3: Implement provider registry

**Objective:** Register providers by name and retrieve providers safely.

**Files:**
- Create: `internal/core/registry.go`
- Test: `internal/core/registry_test.go`

**Step 1: Write failing tests**

Test:
- registering a provider makes it retrievable.
- duplicate registration returns error.
- unknown provider returns false.

**Step 2: RED**

Run: `go test ./internal/core -run TestRegistry -v`
Expected: FAIL.

**Step 3: GREEN**

Implement `Registry` with map and methods:
- `NewRegistry() *Registry`
- `Register(p Provider) error`
- `Get(name string) (Provider, bool)`
- `List() []ProviderStatus`

---

### Task 4: Implement quota ledger

**Objective:** Enforce `$0 hard cap` before provider calls.

**Files:**
- Create: `internal/core/quota.go`
- Test: `internal/core/quota_test.go`

**Step 1: Write failing tests**

Test:
- provider with remaining free calls is allowed.
- provider with zero remaining calls is blocked.
- unknown quota defaults to blocked unless provider is keyless free.
- recording usage decrements free calls.

**Step 2: RED**

Run: `go test ./internal/core -run TestQuota -v`
Expected: FAIL.

**Step 3: GREEN**

Implement in-memory ledger first:
- `QuotaPolicy { HardCapCents int }`
- `QuotaEntry { Provider string; FreeRemaining int; KeylessFree bool; Unknown bool }`
- `QuotaLedger` interface.
- `MemoryQuotaLedger`.
- `Allow(provider string) bool`.
- `Record(provider string) error`.

YAGNI: file persistence later.

---

### Task 5: Implement task-based router

**Objective:** Choose best provider by task, then quota, then capability.

**Files:**
- Create: `internal/core/router.go`
- Create: `internal/core/errors.go`
- Test: `internal/core/router_test.go`

**Step 1: Write failing tests**

Test:
- general search prefers Brave then Tavily then DDGS.
- docs search prefers Brave/Firecrawl/Jina route depending capability.
- extract prefers Jina then Firecrawl.
- quota exhaustion falls back to next allowed provider.
- no allowed provider returns `ErrNoFreeQuota`.

**Step 2: RED**

Run: `go test ./internal/core -run TestRouter -v`
Expected: FAIL.

**Step 3: GREEN**

Implement:
- `RouteMatrix map[TaskType][]string`
- `DefaultRouteMatrix()`
- `Router.Select(req SearchRequest, capability Capability) (Provider, []string, error)`
- `ErrNoFreeQuota` structured error.

---

### Task 6: Implement core service

**Objective:** Expose `Search`, `Extract`, `ProviderStatus`, and `BudgetStatus` using registry/router/quota.

**Files:**
- Create: `internal/core/service.go`
- Test: `internal/core/service_test.go`

**Step 1: Write failing tests**

Test:
- search calls selected provider.
- route is included in response.
- provider errors fall back to next provider.
- extract uses extract route.
- no quota returns structured error.

**Step 2: RED**

Run: `go test ./internal/core -run TestService -v`
Expected: FAIL.

**Step 3: GREEN**

Implement service with context-aware calls.

---

### Task 7: Add mock provider and placeholder real providers

**Objective:** Have testable provider adapters and compile-time shells for real APIs.

**Files:**
- Create: `internal/providers/mock/mock.go`
- Create: `internal/providers/ddgs/ddgs.go`
- Create: `internal/providers/jina/jina.go`
- Create: `internal/providers/firecrawl/firecrawl.go`
- Create: `internal/providers/brave/brave.go`
- Create: `internal/providers/tavily/tavily.go`

**Step 1: Tests**

Use core service tests with mock provider.

**Step 2: Implement**

- Mock provider returns deterministic results.
- Placeholder providers return `not implemented` status but satisfy interface.
- DDGS can be placeholder for MVP; real network implementation later.

---

### Task 8: CLI search/extract/providers/doctor

**Objective:** User can run basic commands from terminal.

**Files:**
- Modify: `internal/cli/root.go`
- Create: `internal/cli/search.go`
- Create: `internal/cli/extract.go`
- Create: `internal/cli/providers.go`
- Create: `internal/cli/doctor.go`

**Step 1: Write tests**

Test command registration and JSON output shape using a buffer.

**Step 2: Implement**

Commands:
- `searchmcp search "query" --task general --json`
- `searchmcp extract "https://example.com" --json`
- `searchmcp providers --json`
- `searchmcp doctor`

Doctor MVP checks:
- binary starts.
- stdout cleanliness note.
- provider statuses.
- no secrets printed.

---

### Task 9: MCP stdio server

**Objective:** Expose `search`, `extract`, `provider_status`, `budget_status` as MCP tools.

**Files:**
- Create: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/tools.go`
- Modify: `internal/cli/mcp.go`

**Step 1: Write tests**

Test server constructor registers expected tool names if SDK exposes list; otherwise test command exists and package compiles.

**Step 2: Implement**

Use `github.com/mark3labs/mcp-go` for ergonomic MVP:
- `server.NewMCPServer("searchmcp", version.Version, server.WithToolCapabilities(false))`
- `mcp.NewTool("search", ...)`
- `server.ServeStdio(s)`

Critical: logs to stderr only.

---

### Task 10: Full verification

**Objective:** Prove MVP builds and behaves.

Run:

```bash
go test ./...
go build -o ./bin/searchmcp .
./bin/searchmcp doctor
./bin/searchmcp providers --json
./bin/searchmcp search "model context protocol go sdk" --task general --json
```

Expected:
- tests pass.
- binary builds.
- doctor prints no secrets.
- search returns mock/DDGS placeholder results or clear structured provider-not-implemented error.

## Initial Success Criteria

- `go test ./...` passes.
- `go build` produces one binary.
- CLI commands are registered and usable.
- Router is task-based, not sequential rotation.
- Quota ledger can fail closed with `no_free_quota`.
- MCP server command exists and uses stdio.
- Provider failures do not crash startup.
- No API keys are printed anywhere.

## Non-Goals for MVP

- Full production Firecrawl/Jina/Brave/Tavily implementations.
- Hosted HTTP deployment.
- OAuth.
- Persistent quota DB.
- Browser scraping.
- Public release packaging.

## Next Phase After MVP

1. Implement Jina Reader/Search adapter.
2. Implement Firecrawl Search/Scrape adapter.
3. Implement Brave/Tavily adapters.
4. Add file-backed quota ledger.
5. Add setup command for Claude/Cursor/Codex/OpenCode/Windsurf.
6. Add HTTP MCP server.
7. Add GitHub Actions + GoReleaser.
