# Route evidence summary

This file records the evidence used for the current default route matrix. It is
summary-only: no raw provider payloads, provider key values, auth headers,
private URLs or private queries are included.

## Deterministic Offline Contract

Fixture version: `2026-05-17.offline.v1`
Mode: offline
Artifact kind: deterministic_fixture_eval
Private data: none included
Keys: presence/status only, no values
Network required: false
Secrets required: false

### Methodology

Scores a versioned fixture set against deterministic observations and the
configured route matrix; it makes no provider network calls.

Data source: repository fixtures plus deterministic in-code observations; not
live provider data.

### Measures

- routing and fallback contract coverage
- fixture coverage by task and language
- deterministic selected-provider behavior for the route matrix

### Does Not Measure

- live web result quality
- currentness of real provider indexes
- provider uptime or production availability
- actual cost/quota behavior or provider account balances
- statistically significant provider ranking

### Reproduction

- `go test ./internal/bench ./internal/cli`
- `nole bench --json`
- `nole bench --evidence-md`

### Raw Artifact Policy

No raw provider payloads exist in offline mode; generated summaries are
public-safe and fixture-only.

### Offline Case Summary

| task | provider | cases | success | result range | latency bucket | notes |
| --- | --- | ---: | ---: | --- | --- | --- |
| general | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| news | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| docs | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| code | tavily | 1 | 1 | 0-5 | <=3s | offline fixture summary; does not measure live web result quality |
| academic | tavily | 1 | 1 | 0-5 | <=3s | offline fixture summary; does not measure live web result quality |
| factcheck | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| pricing | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| people | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| social | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| semantic | tavily | 1 | 1 | 0-5 | <=3s | offline fixture summary; does not measure live web result quality |
| docs | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| extract | scrapling | 1 | 1 | 0-1 | <=1s | offline fixture summary; does not measure live web result quality |
| general | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| docs | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| factcheck | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| pricing | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| docs | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |

## Live Task-Provider Run 2026-05-26

Mode: live task-provider sample
Artifact kind: sanitized_live_task_provider_summary
Private data: none included
Keys: presence/status only, no values
Network required: true
Secrets required: provider keys/local runtime where configured
Policy: `free-first`

### Methodology

The run forced each capable provider against public fixtures instead of using
the router, so each provider/task pair could be observed independently. It
stored only sanitized metrics: success/failure, coarse task-fit scores, result
counts, latency, content length, domains and error class. It did not save raw
provider bodies, snippets, auth headers or key values.

Task coverage:

- Search: `general`, `news`, `docs`, `code`, `academic`, `factcheck`,
  `pricing`, `people`, `social`, `semantic`, `research`
- Extract: six public URL extraction fixtures covering static docs, long docs,
  modern docs, pricing, release pages and vendor API docs

Scale:

- cases: 39
- measurements: 150
- search providers: Brave, DDGS, Firecrawl, Tavily
- extract providers: Firecrawl, Scrapling, Tavily

### Does Not Measure

- global provider quality
- provider uptime or SLA
- provider dashboard billing or account balance
- all languages, regions or private/user-specific queries
- live web result quality beyond this local fixture sample

### Raw Artifact Policy

The local JSON artifact stayed in scratch space and is not committed. Shared
evidence is this sanitized summary only.

### Route Evidence Table

The route order below is based on success rate first, then task-fit score, then
latency for this approved local run.

| task | cases | route order from this run | aggregate notes |
| --- | ---: | --- | --- |
| general | 3 | brave -> tavily -> firecrawl -> ddgs | Brave: 3/3, avg score 95.4; Tavily and Firecrawl: 3/3, avg score 91.0; DDGS: 0/3 rate_limited |
| news | 3 | firecrawl -> tavily -> brave -> ddgs | Firecrawl: 3/3, avg score 96.2; Tavily: 3/3, avg score 95.3; Brave: 3/3, avg score 94.6; DDGS: 0/3 rate_limited |
| docs | 3 | firecrawl -> brave -> tavily -> ddgs | Firecrawl: 3/3, avg score 99.5; Brave: 3/3, avg score 98.0; Tavily: 3/3, avg score 81.3; DDGS: 0/3 rate_limited |
| code | 3 | tavily -> firecrawl -> brave -> ddgs | Tavily: 3/3, avg score 94.6; Firecrawl: 3/3, avg score 94.2; Brave: 3/3, avg score 93.3; DDGS: 0/3 rate_limited |
| academic | 3 | tavily -> firecrawl -> brave -> ddgs | Tavily: 3/3, avg score 91.7; Firecrawl: 3/3, avg score 91.6; Brave: 3/3, avg score 90.3; DDGS: 0/3 rate_limited |
| factcheck | 3 | firecrawl -> tavily -> brave -> ddgs | Firecrawl: 3/3, avg score 95.4; Tavily: 3/3, avg score 82.9; Brave: 3/3, avg score 78.4; DDGS: 0/3 rate_limited |
| pricing | 3 | firecrawl -> brave -> tavily -> ddgs | Firecrawl: 3/3, avg score 96.2; Brave: 3/3, avg score 95.3; Tavily: 3/3, avg score 94.9; DDGS: 0/3 rate_limited |
| people | 3 | firecrawl -> brave -> tavily -> ddgs | Firecrawl: 3/3, avg score 99.0; Brave: 3/3, avg score 98.6; Tavily: 3/3, avg score 80.7; DDGS: 0/3 rate_limited |
| social | 3 | firecrawl -> tavily -> brave -> ddgs | Firecrawl: 3/3, avg score 92.2; Tavily: 3/3, avg score 91.3; Brave: 3/3, avg score 84.6; DDGS: 0/3 rate_limited |
| semantic | 3 | tavily -> brave -> firecrawl -> ddgs | Tavily: 3/3, avg score 94.5; Brave: 3/3, avg score 92.1; Firecrawl: 3/3, avg score 88.7; DDGS: 0/3 rate_limited |
| research | 3 | firecrawl -> tavily -> brave -> ddgs | Firecrawl: 3/3, avg score 95.1; Tavily: 3/3, avg score 84.4; Brave: 3/3, avg score 80.0; DDGS: 0/3 rate_limited |
| extract | 6 | scrapling -> firecrawl -> tavily | Scrapling: 6/6, avg score 90.2; Firecrawl: 6/6, avg score 85.8; Tavily: 5/6 with one empty_content, avg score 95.0 on successes |

### Route Matrix Result

The default route matrix follows the route order from the live task-provider
summary above, while keeping service-level provider status checks and
free-first cost policy gates in place. For extract, configured local Scrapling
is tried first; when it is not configured, the service skips it and continues
to Firecrawl and Tavily.

## Wikipedia/MediaWiki insertion (v1.1.0)

The keyless `wikipedia` provider (MediaWiki Action API) was added to the
`factcheck`, `people`, and `academic` routes only, positioned immediately before
`ddgs` — after the keyed providers, before the last-resort general fallback.

This is a **capability-based extension, not a measured re-ranking**: Wikipedia
was not part of the 2026-05-26 live run above, so no benchmark score is claimed
for it, and the existing keyed providers' relative order is unchanged. The
rationale is coverage, not a quality judgement (Nólë never judges quality): a
primary encyclopedic source is a strong keyless complement for biographical
(`people`), factual (`factcheck`), and scholarly-topic (`academic`) queries, and
being keyless it is always available without consuming any BYOK free-tier quota.
`ddgs` remains the final, last-resort entry on every search route (including
these three). Wikipedia was deliberately NOT added to `general` or any other
route, so it never becomes a general fallback.
