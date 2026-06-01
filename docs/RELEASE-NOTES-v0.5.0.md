# Nólë v0.5.0 release notes

Nólë v0.5.0 makes the **router actually route**. The deterministic task planner
now fires on every search (previously it ran only in the inspection commands),
task-fit shapes the request and the output, and the provider's own
relevance/recency signals are passed through for the agent to judge. Nólë stays
a dumb-but-honest gateway for the agent you already use — no LLM, no quality
judgment, no synthesis. No breaking changes; all new response fields are
additive.

## Routing

- **Task-fit routing on every search.** When no task is supplied, `Service.Search`
  auto-classifies the query with the deterministic multi-intent planner and routes
  on the detected task, so the news/docs/code/academic/pricing routes actually
  apply instead of everything defaulting to the generic route. An explicit
  `--task` (CLI) or `task` (MCP/REST) argument always wins. The planner is pure
  keyword matching — deterministic and reproducible, not intelligence.
- **`task_source` transparency.** Every search response reports how the task was
  chosen — `supplied` (caller gave it), `detected` (planner inferred it), or
  `default` (no signal → general) — so an agent can see whether it drove the
  route or Nólë inferred it.

## Output

- **Relevance and recency pass-through.** `SearchResult` now carries optional
  `score` (the provider's own relevance, e.g. Tavily's) and `published_at`
  (publication date) where the provider supplies them. These are passed through
  verbatim — never computed, normalized, or fabricated — and omitted when the
  provider gives none, so the agent (the only party that can judge quality) gets
  the raw signals.
- **Date-ordered news.** `news` / `factcheck` results are stably ordered
  newest-first using the provider-supplied dates. This is a pass-through of the
  freshness signal, not a quality judgment: `score` is never sorted or filtered
  on, results are never dropped, and undated results keep their original order.

## Requests

- **Task-aware freshness.** `news` / `factcheck` searches send a conservative
  last-month window to providers that support it (Brave `freshness=pm`, Tavily
  `topic=news`/`time_range=month`, Firecrawl `tbs=qdr:m`). Every other task sends
  a byte-identical request to before. The keyless DDGS fallback sends no time
  filter (an undocumented parameter risks raising its anti-bot block).

## Interfaces

- The MCP `search` tool's `task` parameter is now a documented enum with an
  "auto-detected if omitted" note. Unknown or aliased values (e.g. `community` →
  social) are normalized server-side on **both** MCP and REST rather than
  erroring or misrouting.

## Honest limitations

- **Firecrawl news dates:** Firecrawl leads the `news` / `factcheck` routes, but
  its web-source results carry no per-result date in this release, so the
  newest-first ordering is a no-op until the chain falls through to Brave
  (`page_age`) or Tavily (`published_date`). Per-result Firecrawl news dates are
  a planned follow-up.
- Nólë never ranks by `score` or decides which result is "better" — that remains
  the calling agent's job. Nólë routes, fetches, and hands over clean evidence.

## Verified

- `go build ./...`, `go vet ./...`
- `go test -race ./...` (incl. new tests: recency sort, planner regression for
  "concerts this week", task auto-classify + `task_source`, adapter
  score/date/freshness, REST alias normalization, cache-hit source overwrite)
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh --ci`
- Adversarial design review (pre-implementation, 11 agents) and adversarial diff
  review (post-implementation, refute-by-default verification) — both resolved.
- Live dogfood: "what concerts are happening in Istanbul this week" → `news`,
  `task_source: detected`, results carrying `score` + `published_at`; evergreen
  / code queries (e.g. "event listener javascript") correctly stay `general`.
