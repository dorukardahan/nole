# Route evidence summary deterministic-offline

Fixture version: 2026-05-17.offline.v1
Mode: offline
Artifact kind: deterministic_fixture_eval
Private data: none included
Keys: presence/status only, no values
Network required: false
Secrets required: false

## Methodology

Scores a versioned fixture set against deterministic observations and the configured route matrix; it makes no provider network calls.

Data source: Repository fixtures plus deterministic in-code observations; not live provider data.

## Measures

- routing and fallback contract coverage
- fixture coverage by task and language
- deterministic selected-provider behavior for the route matrix

## Does not measure

- live web result quality
- currentness of real provider indexes
- provider uptime or production availability
- actual cost/quota behavior or provider account balances
- statistically significant provider ranking

## Reproduction

- go test ./internal/bench ./internal/cli
- nole bench --json
- nole bench --evidence-md

## Raw artifact policy

No raw provider payloads exist in offline mode; generated summaries are public-safe and fixture-only.

## Case summary

| task | provider | cases | success | result range | latency bucket | notes |
| --- | --- | --- | --- | --- | --- | --- |
| general | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| news | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| docs | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| code | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| academic | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| factcheck | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| pricing | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| people | tavily | 1 | 1 | 0-5 | <=3s | offline fixture summary; does not measure live web result quality |
| social | firecrawl | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| semantic | tavily | 1 | 1 | 0-5 | <=3s | offline fixture summary; does not measure live web result quality |
| docs | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| extract | tavily | 1 | 1 | 0-1 | <=1s | offline fixture summary; does not measure live web result quality |
| general | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| docs | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
| factcheck | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| pricing | brave | 1 | 1 | 0-5 | <=500ms | offline fixture summary; does not measure live web result quality |
| docs | brave | 1 | 1 | 0-5 | <=1s | offline fixture summary; does not measure live web result quality |
