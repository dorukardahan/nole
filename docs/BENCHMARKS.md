# Benchmarks and route evidence

Nólë uses benchmark/evidence language carefully. The goal is to make routing decisions credible without pretending that an offline fixture can measure the live web.

## Two different things

### Deterministic offline harness

Command:

```bash
nole bench --json
nole bench --evidence-md
```

Purpose:

- Validate routing/fallback contracts.
- Ensure fixture coverage for supported task types.
- Catch route matrix regressions.
- Produce a public-safe Markdown evidence summary.
- Run in CI without secrets or network calls.

The deterministic offline harness does not measure live web quality. It does not measure:

- live web result quality;
- currentness of real provider indexes;
- provider uptime in production;
- actual cost/quota behavior;
- statistically meaningful provider ranking.

The JSON report includes an `evidence` object with methodology, data source, reproduction commands, raw artifact policy and explicit `does_not_measure` caveats. The Markdown summary uses the same metadata and is intended for public PRs/docs.

### Optional live benchmark summaries

Before running any live benchmark, follow `docs/LIVE-BENCHMARK-PLAN.md`
and use `docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md` for the public-safe
summary. Those docs are planning/template artifacts only; they do not run
live provider calls, use provider keys or create evidence by themselves.

Command:

```bash
# Explicit low-limit smoke only. Keep cost policy and provider keys intentional.
nole bench --live --max-live-cases 3 --json
nole bench --live --max-live-cases 3 --evidence-md
```

Purpose:

- Collect low-limit smoke evidence against configured providers.
- Record provider/task success, latency buckets and result counts for this local run.
- Observe extraction success and sanitized error categories.
- Produce sanitized summaries that can inform future route changes.

Rules:

- Live runs are explicit only.
- Live/keyed provider calls require explicit maintainer approval before execution.
- Keep each run low-limit and record the approved maximum case count before it starts.
- Do not run live benchmarks in CI.
- Do not require secrets in CI.
- Do not commit raw provider payloads, headers, private queries, private URLs or credentials.
- Sanitize before sharing.
- Do not overstate the results; a smoke summary is not a scientific paper or a provider-ranking claim.
- Cost policy still applies. A live run can consume quota or incur provider-account cost when the selected policy and provider dashboard allow it.

The separate `--live --comprehensive` mode is more invasive: it bypasses the route matrix, policy and quota ledger so each provider can be compared directly on every capability-permitting fixture. It still never prints key values or raw payloads, and a TinyFish instance with no `TINYFISH_API_KEY` rejects locally before any provider call. Run it only after the explicit keyed/live approval gates in `docs/LIVE-BENCHMARK-PLAN.md`:

```bash
nole bench --live --comprehensive --max-comprehensive-cases 3 --json
```

Comprehensive live fixtures extend (but do not mutate) the deterministic offline set with localization/freshness search options plus static, JavaScript-heavy, redirect, error-path and structured-content extraction targets. TinyFish probes use a conservative two-second inter-call spacing floor. These are contract/smoke observations, not ranking evidence.

## Evidence fields to record

A sanitized live summary should include:

- fixture/scenario version;
- mode (`offline` deterministic fixture eval or `live` smoke summary);
- methodology and data source;
- provider name;
- task or selected intents;
- success/failure count;
- selected provider count;
- result count range;
- latency bucket;
- extraction success count where relevant;
- source/citation notes if manually reviewed;
- sanitized error categories such as `provider_error`, `empty_results`, `quota_blocked`, `premium_blocked_free_first` or `cost_cap_exceeded`;
- date of run;
- whether keys were present, without printing values;
- raw artifact policy.

## Scenario set

The default offline fixture set is versioned and covers generic scenarios for:

| Scenario | Intent/task |
| --- | --- |
| General web search | general |
| Documentation lookup | docs |
| Code examples / release notes | code |
| Pricing and limits | pricing |
| People/company lookup | people |
| Academic/papers | academic |
| News/freshness | news |
| Social/community discussion | social |
| Fact checking | factcheck |
| Semantic discovery | semantic |
| URL extraction | extract |
| General multilingual search | general/docs/pricing/factcheck variants |

Adding or removing a supported task type should update both the fixture set and tests in `internal/bench`.

## Route matrix policy

Route matrix changes require evidence. Do not change provider route ordering unless the PR includes sanitized evidence or a clear deterministic-contract reason.

Acceptable evidence sources:

- sanitized live benchmark summary;
- documented provider capability change;
- regression test showing current route violates fallback/capability/cost-policy contract;
- user-approved provider policy change.

Not sufficient by itself:

- a provider is popular;
- a provider is paid;
- a provider has a key configured;
- a single anecdotal result without fixture context;
- an offline fixture score presented as live web quality.

## Public-safe artifact structure

Use `docs/ROUTE-EVIDENCE.md` for the current deterministic route evidence summary, or a dated file under a future evidence directory for explicit live smoke summaries. Keep shared artifacts summary-only:

```markdown
# Route evidence summary YYYY-MM-DD

Fixture version: ...
Mode: offline | live
Artifact kind: deterministic_fixture_eval | live_smoke_summary
Private data: none included
Keys: presence/status only, no values
Network required: true | false
Secrets required: true | false

## Methodology
...

## Does not measure
- ...

## Raw artifact policy
...

| task | provider | cases | success | result range | latency bucket | notes |
| --- | --- | --- | --- | --- | --- | --- |
```

Raw JSON provider responses belong in local scratch space only and should be deleted or ignored. Do not paste them into PRs, issues, docs or chat.

## CI expectation

CI should run deterministic `nole bench --json`, `nole bench --evidence-md`, and `scripts/check-benchmark-claims.sh`. CI should not run `--live` because live mode requires network access and explicit quota/cost decisions, and it may use configured provider secrets.
