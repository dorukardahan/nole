# Controlled live benchmark plan

This plan is for a future, explicit, low-limit live-routing benchmark of Nólë providers. It is a preparation document only: adding or editing this file does not run live provider calls, use provider keys, publish a release, create assets, deploy anything or change repository visibility.

## Purpose

Use a small, controlled live smoke run to observe how Nólë routes real search and extraction tasks across configured providers. The run can inform future route evidence, provider-quality notes and follow-up implementation work, but it must not be presented as a scientific benchmark or a statistically significant provider ranking.

## Non-goals

This benchmark is not intended to:

- prove comprehensive live web result quality;
- rank providers globally;
- prove provider uptime or SLA;
- prove provider dashboard billing or account balance;
- justify a route-order change by itself without maintainer review;
- verify new MCP clients;
- publish a release, upload assets, deploy anything or change repo visibility.

## Approval gates

Before execution, record explicit maintainer approval for:

1. running live provider calls;
2. using any keyed provider in the run;
3. allowing any paid-capable provider to participate;
4. accepting any provider-account quota or overage risk;
5. the maximum number of live cases and approximate call budget;
6. the policy mode (`free-first`, `cost-capped` or `quality-first`);
7. the providers included in the run;
8. the sanitized summary location.

Do not start a run if approval is missing or ambiguous. Public release, release assets, deployments and repository visibility are separate approval gates and are out of scope for this benchmark.

## Provider-key inventory rules

Key inventory must be presence-only:

- report only `present`, `absent`, `disabled`, `not checked` or similar status words;
- never print, paste, save or commit key values;
- never paste env files or credential files into docs, PRs, issues or chat;
- never put provider keys in command-line arguments where process listings or logs can expose them;
- prefer checks that test whether an environment variable exists without echoing the value;
- keep raw command output local if there is any chance it includes secret-like material.

Allowed provider-key status examples:

| Provider | Key status wording |
| --- | --- |
| Brave | `present` / `absent` |
| Tavily | `present` / `absent` |
| Firecrawl | `present` / `absent` |
| DDGS | `not required; keyless fallback/control` |
| Scrapling | `configured` / `not configured`; local runtime, no remote key |

## Provider set

The controlled run may include:

- Brave Search API for search tasks when configured and policy-allowed;
- Tavily for search/extract tasks when configured and policy-allowed;
- Firecrawl for search/extract tasks when configured and policy-allowed;
- DDGS as a keyless search fallback/control only;
- Scrapling as a local keyless extract fallback when `nole setup --local-extract` has configured it.

DDGS must not be described as the benchmark-primary docs provider. Keyed providers must not be preferred merely because keys exist; route choices still need task fit, policy and evidence.

## Cost-policy rules

- `free-first` is the default no-hidden-paid-spend policy.
- `cost-capped` may allow premium-capable providers only when local cap and estimate settings keep the call inside the cap.
- `quality-first` must be explicit and means the operator accepts provider-account cost risk for quality/task fit.
- Nólë's quota/cost ledger is local accounting, not provider-dashboard balance.
- Review provider dashboards for free-tier limits, paid-plan status and overage settings before execution.
- Stop immediately if quota or overage status is unclear.

## Suggested call budget

Use the smallest useful run:

| Scope | Suggested max live cases | When to use |
| --- | ---: | --- |
| Smoke preflight | 1-3 | First approved run or uncertain provider state. |
| Controlled sample | 4-8 | Provider keys and overage settings reviewed. |
| Expanded sample | 9-10 | Only after the smoke run is clean; current CLI clamps live cases above 10. |

Record the approved maximum before execution. Do not retry failed cases repeatedly without renewed approval because retries also consume quota and may change evidence.

## Fixture and scenario set

A useful live-routing sample should cover these tasks when the harness supports them:

| Scenario | Task/intents | Example fixture guidance |
| --- | --- | --- |
| Documentation lookup | `docs` | stable public API docs or changelog lookup |
| News/freshness | `news` | date-sensitive announcement or release news |
| Academic/research | `academic` | public paper or benchmark discovery |
| Fact-check | `factcheck` | claim requiring multiple public sources |
| Pricing and limits | `pricing` | public pricing or plan-limit lookup |
| People/company | `people` | public organization, team or funding lookup |
| Code/GitHub/release notes | `code` | public repo, issue, example or release note |
| Extraction | `extract` | public, non-private URL with safe content |
| Multilingual optional | mixed | Turkish or other non-English public query |

Do not use private user questions, private URLs, internal docs, local files or chat transcripts as fixtures.

## Measurement dimensions

Record summary-level observations only:

- selected provider;
- route attempts and provider attempted/skipped reasons;
- success/failure count;
- result count bucket, not raw full payload;
- latency bucket;
- source/citation quality notes from manual review;
- freshness/currentness notes where relevant;
- extraction success and content-usefulness notes where relevant;
- sanitized error category;
- policy outcome such as `premium_blocked_free_first`, `quota_blocked`, `cost_cap_exceeded`, `empty_results`, `provider_error` or `network_error`.

Avoid exact raw response bodies. Prefer buckets such as `0`, `1`, `2-3`, `4-5`, `>5` results and latency buckets such as `<=500ms`, `<=1s`, `<=3s`, `<=8s`, `>8s`.

## Stop conditions

Stop the run and do not commit artifacts if any of these occur:

- unexpected paid-use warning or provider overage warning;
- quota exhaustion or uncertainty about quota/overage state;
- provider authentication or authorization errors;
- repeated network failures that make the sample misleading;
- output includes a secret-like value;
- raw provider JSON, auth headers or credential-bearing logs appear in saved output;
- a private URL, private query or local transcript appears in output;
- the operator cannot verify that the summary is sanitized.

If a stop condition triggers, write a private local note with the sanitized failure category only, then decide whether a separate fix PR is needed.

## Sanitization contract

Allowed to commit or share:

- sanitized Markdown summaries;
- aggregate tables;
- provider names and status words;
- key presence status without values;
- public URLs only when intentionally reviewed and safe;
- route attempt status/reason fields after redaction;
- coarse latency and result-count buckets.

Never commit or share:

- provider key values;
- bearer-token values;
- auth-header values;
- API-key header values;
- raw provider payloads;
- private URLs;
- private queries;
- local transcripts;
- env file contents;
- machine-specific runtime logs.

Raw JSON provider responses, if generated by a future approved run, belong only in local scratch space and should be deleted or kept gitignored. Do not paste raw artifacts into PRs, issues, docs or chat.

## Future approved command pattern

Only after explicit approval, a minimal smoke run may use commands like:

```bash
# Use the approved policy and case limit. Do not run without explicit approval.
NOLE_COST_POLICY=free-first nole bench --live --max-live-cases 3 --json > live-bench.local.json
NOLE_COST_POLICY=free-first nole bench --live --max-live-cases 3 --evidence-md > live-bench.summary.md
```

Before committing any summary, manually review and copy only public-safe, sanitized content into the summary template. Keep local scratch files untracked and remove them when no longer needed.

## What results can support

A clean live smoke summary can support:

- a dated observation that a provider/task path worked in that local environment;
- evidence that a provider was skipped or blocked by policy in that run;
- follow-up issues for harness, provider-adapter or docs improvements;
- a maintainer-reviewed route-evidence discussion.

It cannot support:

- broad provider rankings;
- Do not claim one provider is always the top choice or lowest-latency option;
- claims about provider uptime or SLA;
- claims about provider account billing beyond local estimates and dashboards;
- generic route-order changes without additional review;
- public release readiness by itself.
