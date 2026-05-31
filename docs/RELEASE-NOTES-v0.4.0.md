# Nólë v0.4.0 release notes

Nólë v0.4.0 is a reliability & hardening quality pass that ships the deferred
quality-opportunity backlog from the v0.3.0 audit plus three discovery findings.
No breaking changes, no new required configuration.

## Reliability

- **Per-provider circuit breaker** for the remote API providers (Brave, Tavily,
  Firecrawl). After a configurable number of consecutive failures a provider's
  breaker opens and calls short-circuit immediately — no burned per-call timeout
  and no quota debit — then admit a single half-open probe per cooldown to
  recover. It uses a generation/epoch model so a slow call admitted in a
  previous regime can never be mis-attributed to the recovery probe; it trips on
  5xx / 429 / 408, transport errors, and client timeouts (a hung upstream), and
  never on 4xx client errors or caller cancellation. State is in-memory and
  per-process, so it benefits the long-lived `nole serve` / MCP server; one-shot
  CLI invocations never accumulate enough to trip. The keyless DDGS fallback and
  the local Scrapling extractor are intentionally left unbreakered so the free
  last-resort path is never short-circuited. Tunable via `NOLE_BREAKER_THRESHOLD`
  (default 5) and `NOLE_BREAKER_COOLDOWN_MS` (default 30000).
- **Ctrl-C / SIGTERM now cancels in-flight work** for `search`, `extract`, and
  `research` instead of hard-killing mid-request. A signal-aware root context is
  threaded into the providers; the DNS preflight resolves on that context so a
  slow/wedged resolver is interruptible; `research` surfaces the cancellation
  instead of returning a partial report; and a *second* interrupt force-exits
  during a slow graceful shutdown. `nole mcp` and `nole serve` share the same
  root context with no nested signal handlers.

## Security

- **Embedded-IPv4 SSRF.** The preflight now decodes IPv4 addresses embedded in
  IPv6 transitional forms — IPv4-compatible (`::a.b.c.d`) and 6to4
  (`2002::/16`) — and re-validates the embedded address, closing bypasses where
  a private/metadata IPv4 was smuggled inside a v6 literal past `net.IP`'s
  classifiers. NAT64 keeps its wholesale `64:ff9b::/96` block; network-specific
  prefix NAT64 is left to best-effort to avoid over-blocking legitimate public
  translations.

## Correctness

- Cache eviction is now deterministic when entries share a timestamp (a
  monotonic insertion sequence breaks ties instead of relying on map order).
- The Firecrawl search adapter clamps the result limit to `[1,20]` like Brave
  and Tavily (defense-in-depth for direct construction).
- The `research` report's `providers_used` list is sorted for stable output.

## Tests

- Native fuzz targets: `FuzzValidateURL` (SSRF, fail-closed), `FuzzCleanHTML`
  (DDGS sanitizer), `FuzzDecodeJSONLimited` (bounded readers) — seed corpora run
  in the normal gate.
- Direct REST-handler tests through `buildMux` (method gating, happy paths,
  malformed/oversized body → 400, error-envelope redaction shape), a
  quota persist-failure rollback regression test, breaker state-machine /
  classification / per-call-counting / stale-race tests, cache frozen-clock
  determinism, and CLI cancellation tests.

## Verified

- `go build ./...`, `go vet ./...`
- `go test -race ./...`
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh --ci` (docs framing, benchmark claims, integration
  evidence, `go run . doctor`, `go run . doctor --mcp`, `go run . bench --json`,
  `go run . providers --json`)
- Adversarial review + a 16-finding refute-by-default verification pass, then
  8 Codex review rounds resolved to a clean pass.
