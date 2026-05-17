# Benchmarks and route evidence

Nólë uses benchmark/evidence language carefully. The goal is to make routing decisions credible without pretending an offline fixture can measure the live web.

## Two different things

### Deterministic offline harness

Command:

```bash
nole bench --json
```

Purpose:

- Validate routing/fallback contracts.
- Ensure fixture coverage for supported task types.
- Catch route matrix regressions.
- Run in CI without secrets or network calls.

The deterministic offline harness does not measure live web quality. It does not measure:

- Live web result quality;
- Currentness of real provider indexes.
- Provider uptime in production.
- Actual cost/quota behavior.

### Optional live benchmark summaries

Command:

```bash
nole bench --live --max-live-cases 3 --json
```

Purpose:

- Collect low-limit smoke evidence against configured providers.
- Compare provider/task success, latency ranges and result counts.
- Observe extraction success and error categories.
- Produce sanitized summaries that can inform future route changes.

Rules:

- Live runs are explicit only.
- Do not require secrets in CI.
- Do not commit raw provider payloads, headers or private queries.
- Sanitize before sharing.
- Do not overstate the results; a smoke summary is not a scientific paper.

## Evidence fields to record

A sanitized live summary should include:

- fixture/scenario version;
- provider name;
- task or selected intents;
- success/failure count;
- selected provider count;
- result count range;
- latency range or bucket;
- extraction success count;
- source/citation quality notes;
- sanitized error categories such as `provider_error`, `empty_results`, `quota_blocked`;
- date of run;
- whether keys were present, without printing values.

## Scenario set

Small generic scenarios should cover:

| Scenario | Intent/task |
| --- | --- |
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
| General multilingual search | general |

## Route matrix policy

Do not change provider route ordering unless the PR includes sanitized evidence or a clear deterministic-contract reason.

Acceptable evidence sources:

- sanitized live benchmark summary;
- documented provider capability change;
- regression test showing current route violates fallback/capability contract;
- user-approved provider policy change.

Not sufficient by itself:

- a provider is popular;
- a provider is paid;
- a provider has a key configured;
- a single anecdotal result without fixture context.

## Public-safe artifact structure

Use `docs/ROUTE-EVIDENCE.md` or a dated file under a future evidence directory. Keep it summary-only:

```markdown
# Route evidence summary YYYY-MM-DD

Fixture version: ...
Mode: live smoke summary
Private data: none included
Keys: presence only, no values

| task | provider | cases | success | result range | latency bucket | notes |
| --- | --- | --- | --- | --- | --- | --- |
```

Raw JSON provider responses belong in local scratch space only and should be deleted or ignored.

## CI expectation

CI should run deterministic `nole bench --json`. CI should not run `--live` because live mode requires secrets, network access and quota/cost decisions.
