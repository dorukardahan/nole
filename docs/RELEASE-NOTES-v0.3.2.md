# Nólë v0.3.2 release notes

Nólë v0.3.2 is a focused security release that closes the last open item from
the v0.3.0 audit: redirect-based SSRF on the opt-in local Scrapling extractor.
No breaking changes, no new configuration.

## Security

- **Local Scrapling extract no longer follows HTTP redirects past the SSRF
  preflight.** Previously `Service.Extract` validated only the initial URL and
  then the local fetcher followed redirects with no per-hop check, so a public
  URL that `302`-redirected to `169.254.169.254` (cloud metadata / IAM) or an
  internal host would be fetched from the user's own machine. Now:
  - the Scrapling fetch runs with redirect-following disabled and, on a `3xx`,
    surfaces the resolved absolute `Location` instead of content;
  - Go drives the redirect walk and re-validates every redirect target with
    `safenet.ValidateURL` **before** it is fetched (bounded to 5 hops), with a
    final-URL backstop for a build that ignores the no-follow request;
  - if a Scrapling build does not support disabling redirects, the extract
    **fails closed** (a clear "upgrade scrapling" error) rather than fetching
    with redirects enabled.

  Bounded to the opt-in local-extract path (`NOLE_SCRAPLING_PYTHON`). The
  redirect-disabled `status`/`Location` contract is verified live against
  Scrapling 0.4.8; the Go redirect-validation loop is unit-tested (follow a
  public redirect; block a redirect to metadata/private pre-fetch;
  too-many-redirects; final-URL backstop).

## Verified

- `go build ./...`, `go vet ./...`
- `go test -race ./...`
- `./scripts/secret-scan.sh`
- `./scripts/audit.sh` (docs framing, benchmark claims, integration evidence,
  `go run . doctor`, `go run . doctor --mcp`, `go run . bench --json`,
  `go run . bench --evidence-md`, `go run . providers --json`)
- Codex review: one P1 (fail-closed) addressed; second pass clean.
