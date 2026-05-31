# Nólë v0.3.1 release notes

Nólë v0.3.1 is a hardening release driven by a comprehensive adversarial audit
of v0.3.0. The audit found no critical or high-severity bug; this release closes
the medium/low defense-in-depth and correctness gaps it surfaced. There are no
breaking changes and no new required configuration.

## Added

- Response-body size caps across every provider: `providerhttp.ReadAllLimited`
  / `DecodeJSONLimited` (16 MiB search, 64 MiB extract) wrap each HTTP response
  read, and the local Scrapling subprocess stdout/stderr are bounded
  (64 MiB / 64 KiB). A hostile or misconfigured endpoint can no longer OOM the
  process with an unbounded body. Errors are redaction-safe.
- CI now runs `go test -race ./...` (a dedicated job) so the concurrency-heavy
  cache, quota ledger, MCP session tracker and the new request-coalescing path
  are race-checked on every change.
- `govulncheck` now runs in the tag-triggered release workflow, so the exact
  published commit is vulnerability-scanned (not only PRs/pushes to `main`).

## Changed

- Concurrent identical search/extract requests are coalesced
  (`golang.org/x/sync/singleflight` via `DoChan`, keyed by the cache key): N
  simultaneous identical queries collapse to one upstream fetch and one quota
  debit, so a burst on the `serve` surface cannot multiply free-tier debits.
  Each caller observes only its own cancellation, and the shared fetch runs on
  a context detached from any single caller so a leaving client cannot fail its
  peers. Distinct queries still run fully in parallel.
- The provider fallback loop short-circuits on a cancelled/disconnected request,
  surfacing `context.Canceled` immediately instead of probing every remaining
  provider.
- Search `limit` is clamped centrally to `[1,20]` (and Brave/Tavily request
  counts to their API maximum of 20), so an over-large limit no longer forces a
  guaranteed provider `422`.
- Provider retry backoff adds equal jitter (`math/rand/v2`) to the exponential
  delay; `Retry-After` handling is unchanged and stays exact.
- `nole serve` shuts down gracefully on SIGINT/SIGTERM (drains in-flight
  requests via `http.Server.Shutdown`), warns on a non-loopback bind (the
  endpoints are unauthenticated and expose BYOK keys/quota), accurately
  describes the REST API at `/api/*` in its help/error text, and gates the
  read-only endpoints to GET/HEAD.

## Fixed

- `research`: extract content is truncated on a rune boundary, not a byte
  boundary, preventing mid-UTF-8 mojibake in non-ASCII results.
- `ddgs`: result `href` values now have `&amp;` decoded.
- `safeerr`: `Set-Cookie`/`Cookie` tokens and userinfo credentials in
  non-`http(s)` scheme URLs are now redacted.
- Docs: `AGENTS.md` states the Go 1.25+ toolchain requirement (matching
  `go.mod`); the previously-missing `[0.2.3]` CHANGELOG section is restored.

## Security

- SSRF preflight blocks reserved ranges Go's `net.IP.IsPrivate()` misses
  (CGNAT `100.64.0.0/10`, `0.0.0.0/8`, `192.0.0.0/24`, benchmark
  `198.18.0.0/15`, IPv6 `64:ff9b::/96` and `2001:db8::/32`) and rejects
  ambiguous all-numeric/octal/hex hostnames (e.g. `0177.0.0.1`) that pass Go's
  resolver but resolve to loopback under a libc/`inet_aton` backend
  (parser-differential SSRF).
- Bounded provider/subprocess reads close an unbounded-read OOM/DoS vector.
- `github.com/buger/jsonparser` bumped `v1.1.1 -> v1.1.2`, clearing the DoS
  advisory `GO-2026-4514` it carried as an indirect dependency.

## Known follow-up

- Local Scrapling extract still follows HTTP redirects past the one-time SSRF
  preflight; the preflight hardening above narrows the reachable target space,
  and per-hop redirect re-validation is tracked as a separate, test-backed
  change.

## Verified

- `go build ./...`, `go vet ./...`
- `go test -race ./...`
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh` (docs framing, benchmark claims, integration evidence,
  `go run . doctor`, `go run . doctor --mcp`, `go run . bench --json`,
  `go run . bench --evidence-md`, `go run . providers --json`)
