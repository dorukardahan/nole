# Cost, quota, cache and output-quality audit

This audit captures the approval-free v0.1 status for Nólë's cost/quota/cache/output-quality surfaces. It is a docs/readiness artifact only. It does not query provider dashboards, run live provider calls, use provider keys, publish releases or change routing behavior.

## Current status

| Area | v0.1 status | Notes |
| --- | --- | --- |
| Cost policy | implemented | `free-first` default, `cost-capped` and `quality-first` policy modes documented. |
| Provider status | implemented | Safe surfaces report provider availability/cost class/reason without key values. |
| Budget status | implemented | Local policy and estimated spend state are surfaced without provider-dashboard claims. |
| Local quota ledger | implemented | Optional file-backed local ledger for provider names, cost classes, local counters and estimated spend. |
| Cache | implemented | In-process TTL cache for normalized search/extract responses. |
| Output quality | implemented baseline | Compact `routing_insight` plus detailed `route_trace` for debugging. |
| Live provider quality | pending approval | Requires controlled live benchmark execution before making live-quality claims. |

## Cost-policy audit

Nólë's default is no hidden paid spend:

- `free-first` allows keyless/free-tier routes and blocks premium-capable providers.
- `cost-capped` allows premium-capable providers only when local hard cap, local ledger state and explicit per-provider estimates keep the call inside the cap.
- `quality-first` explicitly allows premium-capable providers when the operator accepts provider-account cost risk.

A provider key alone is not approval to spend. Route choices should still be based on task fit, policy, availability and evidence.

## Quota and ledger audit

The optional local ledger is local accounting only. It is not provider-dashboard truth.

The ledger may record:

- provider names;
- cost classes;
- local free-quota counters;
- local estimated spend;
- corruption backups/recovery state.

The ledger must not record:

- provider key values;
- bearer tokens;
- auth headers;
- raw provider payloads;
- private URLs or private queries;
- local transcripts.

If provider dashboards expose reliable quota APIs in the future, add them one provider at a time with tests and public-safe docs. Do not query dashboards in routine readiness checks.

## Cache audit

`NOLE_CACHE_TTL` / `NOLE_CACHE_TTL_SECONDS` enable in-process normalized search/extract caching. Cache status can appear in `route_trace` and compact `routing_insight`.

Cache follow-ups should be driven by observed usage, for example:

- cache hit/miss clarity in long-running MCP sessions;
- cache bypass or purge UX if users need it;
- evidence that cached stale results confuse an agent;
- memory footprint observations under real workloads.

Do not build speculative cache persistence without a concrete use case and secret-safety design.

## Output-quality audit

The normal user-facing output should stay compact:

- one deterministic `routing_insight` line when useful;
- result URLs or extracted content only when the provider response is safe and intended;
- full `route_trace` reserved for JSON/debug/troubleshooting surfaces.

`routing_insight` and errors must not include API keys, auth headers, raw provider payloads, private URLs or private queries.

Output-quality follow-ups should be evidence-driven:

- controlled live benchmark summaries;
- user reports of confusing insight wording;
- agent logs showing overlong or underinformative traces;
- provider errors that need better sanitized categories.

## Safe immediate conclusion

No approval-free code change is required for cost/quota/cache/output quality after PR #19. The correct next step for live provider-quality evidence remains a controlled live benchmark, and that requires explicit approval for live/keyed/provider-account risk.

## Approval-gated next steps

| Item | Approval required |
| --- | --- |
| Run `nole bench --live` | live benchmark approval |
| Include keyed providers | keyed provider approval |
| Include paid-capable providers | paid-capable provider and cost/quota approval |
| Query provider dashboards/APIs for balance | provider-account access approval |
| Commit sanitized live summary | sanitized summary commit approval |
| Change route matrix from live findings | maintainer review plus sanitized evidence |
