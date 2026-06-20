# Live benchmark summary template

Use this template only after an explicitly approved low-limit live benchmark run. Fill in public-safe summary fields only. Delete sections that do not apply. Do not paste raw provider JSON, headers, credentials, private URLs, private queries, local transcripts or env file contents.

## Run metadata

- Run date: YYYY-MM-DD
- Operator: maintainer-approved operator name or role
- Repo commit: `<short-or-full-sha>`
- Mode: live smoke summary
- Approval reference: `<issue/PR/chat summary without private transcript>`
- Policy mode: `free-first` / `cost-capped` / `quality-first`
- Max live cases approved: `<number>`
- Cases actually run: `<number>`
- Network required: true
- Secrets required: `false` / `provider keys present in local environment; values not included`
- Raw artifact policy: raw provider payloads and credential-bearing logs are not committed or shared

## Provider inventory

Report presence/status only. Never include values.

| Provider | Included? | Key status | Cost class/policy outcome | Notes |
| --- | --- | --- | --- | --- |
| Brave | yes/no | present/absent/not checked | free-tier-BYOK/premium-capable/disabled-no-key |  |
| Tavily | yes/no | present/absent/not checked | free-tier-BYOK/premium-capable/disabled-no-key |  |
| Firecrawl | yes/no | present/absent/not checked | keyless-free/free-tier-BYOK/premium-capable | absent key may still be included via keyless mode; present key means account-backed quota |
| DDGS | yes/no | not required | keyless-free fallback/control |  |
| Scrapling | yes/no | configured/not configured/not checked | keyless-free local extract fallback |  |
| httpfetch | yes/no | not required | keyless-free extract backstop | always available in normal builds; no JavaScript rendering |

## Scenario summary

Do not include private queries. Use scenario labels and public-safe descriptions.

| Scenario | Task/intents | Cases | Selected provider(s) | Success count | Result-count bucket | Latency bucket | Sanitized notes |
| --- | --- | ---: | --- | ---: | --- | --- | --- |
| Documentation lookup | docs |  |  |  |  |  |  |
| News/freshness | news |  |  |  |  |  |  |
| Academic/research | academic |  |  |  |  |  |  |
| Fact-check | factcheck |  |  |  |  |  |  |
| Pricing and limits | pricing |  |  |  |  |  |  |
| People/company | people |  |  |  |  |  |  |
| Code/GitHub/release notes | code |  |  |  |  |  |  |
| Extraction | extract |  |  |  |  |  |  |
| Multilingual optional | mixed |  |  |  |  |  |  |

## Provider result summary

| Provider | Attempted cases | Selected cases | Successes | Failures | Common sanitized categories | Notes |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| Brave |  |  |  |  |  |  |
| Tavily |  |  |  |  |  |  |
| Firecrawl |  |  |  |  |  |  |
| DDGS |  |  |  |  |  |  |
| Scrapling |  |  |  |  |  |  |
| httpfetch |  |  |  |  |  |  |

Allowed sanitized categories include:

- `success`
- `empty_results`
- `provider_error`
- `network_error`
- `auth_error`
- `quota_blocked`
- `premium_blocked_free_first`
- `cost_cap_exceeded`
- `skipped_no_key`
- `skipped_policy`

## Manual review notes

Record short, public-safe notes only:

- Relevance/source quality:
- Freshness/currentness:
- Source diversity:
- Extraction quality:
- Policy/fallback behavior:
- Follow-up issues:

## Limitations

This summary does not measure:

- comprehensive live web quality;
- statistically significant provider ranking;
- provider uptime or SLA;
- provider dashboard balance or actual billing beyond reviewed account settings;
- behavior for every query, region, language or task;
- public release readiness by itself.

## Explicit non-claims

Do not use this summary to claim:

- Do not claim a provider is the global top choice, lowest-latency option, lowest-cost option or categorically preferable.
- Do not claim a provider always works.
- Do not claim DDGS is a primary docs benchmark provider.
- Do not claim generic MCP clients are verified.
- Do not claim route ordering should change without maintainer review and supporting evidence.

## Redaction checklist

Before committing or sharing, confirm:

- [ ] No provider key values.
- [ ] No bearer-token values.
- [ ] No auth-header values.
- [ ] No API-key header values.
- [ ] No raw provider payloads.
- [ ] No private URLs.
- [ ] No private queries.
- [ ] No local transcripts.
- [ ] No env file contents.
- [ ] No machine-specific runtime logs.
- [ ] Public URLs, if included, were intentionally reviewed as safe.
- [ ] Result counts and latencies are bucketed or summarized.
- [ ] Failure reasons are sanitized categories.

## Raw artifact handling

- Local raw artifacts path: `<local scratch path; do not commit>`
- Deleted after review: yes/no
- If kept locally, reason and gitignore status: `<summary only>`
