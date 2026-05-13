# Nólë Next Steps

Nólë is intentionally staying small for the public-readiness pass. The items below are useful but should be implemented as separate, test-driven changes rather than bundled into the hardening work.

## Reliability

- Add bounded retry with exponential backoff for transient provider failures (`429`, `502`, `503`, `504`) while respecting `Retry-After`.
- Add provider-level observability around selected route, fallback reason, latency, and result count without logging secrets or full request headers.

## Quota and cache

- Add an opt-in TTL cache for search/extract responses to reduce free-tier quota use.
- Add a file-backed quota ledger under the user config directory so monthly/daily free-tier usage survives process restarts.

## Configuration

- Add a small config file for default task, timeout, cache TTL, and route-matrix overrides.
- Keep environment variables as the simplest setup path; never print provider keys in diagnostics.

## CI and release readiness

- Add GitHub Actions for `go test ./...`, `go vet ./...`, and cross-platform builds.
- Add release automation only after the repository is intentionally made public.
