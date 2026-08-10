# Stability commitment (v1.0.0)

As of **v1.0.0**, Nólë commits to [Semantic Versioning](https://semver.org/) for
the surfaces listed below. The goal is simple: an agent or script that integrates
Nólë at 1.x should keep working across 1.x upgrades without changes.

- **MAJOR** (2.0.0): a breaking change to a stable surface — a removed/renamed
  command, flag, MCP tool, or response field; a changed default that alters
  behaviour; a tightened input contract.
- **MINOR** (1.x.0): additive, backward-compatible — a new command, flag, MCP
  tool, optional request/response field, or env var.
- **PATCH** (1.0.x): bug fixes and internal changes with no surface impact.

## Stable surfaces (frozen for 1.x)

### CLI commands

These top-level commands and their documented behaviour are stable:

`search`, `classify`, `route-plan`, `extract`, `research`, `bench`, `providers`,
`doctor`, `config`, `mcp`, `serve`, `setup`, `version`, `self-update`.

The **command set** is locked by a test (`internal/cli` surface lock) so it cannot
drift silently. Their primary flags (e.g. `--json`, `--task`, `--insight`,
`doctor --mcp` / `--json` / `--check-updates`, `self-update --check-only` /
`--version` / `--verify`) and the `--json` field names of `search`, `extract`,
`research`, `classify`, `route-plan`, `providers`, `doctor`, `config dump`,
`bench` are a **stability commitment** — additive-only in 1.x (new flags/fields
may be added; existing ones are not removed/renamed) — upheld by the behaviour
test suite and release review rather than a dedicated field-snapshot lock.

The v1.7.0 SearchOptions expansion is an intentional MINOR surface addition:
`nole search` and `nole research` add optional `--country`, `--search-lang`,
`--ui-lang`, `--safesearch`, and `--freshness` flags. These are caller hints,
prevalidated/canonicalized by `core.Service`; existing search/research behavior
is unchanged when they are omitted. Research applies them only to its internal
search passes.

The post-v1.8.1 content-safety expansion is an intentional MINOR response
addition. Search results, extracts, `search_and_extract`, and research
sources/extracts add a deterministic `content_safety` object; no existing field,
input parameter, command, flag, MCP tool, or routing default is removed or renamed.
The object is always present on successful remote-content records so clients do
not have to infer trust from omission. Its `no_indicators` value is explicitly not
a safety verdict.

The post-v1.7.0 provider-usage expansion intentionally adds optional
`providers --live-usage`. It queries provider usage APIs only where Nólë has a
documented quota endpoint or response-header signal, emits sanitized usage fields,
and may reconcile the local quota ledger to provider-reported usage before routed
calls for free-tier BYOK providers. Premium/paid provider account usage is only
queried through the explicit live-usage observability surface. Every provider
status also carries
additive `remote_usage_strategy` / `remote_usage_reason` fields, including
explicit `not_applicable` for keyless/local providers and `disabled_no_key` for
keyed providers without credentials. Plain `providers`, `doctor`, `config dump`,
and `budget_status` remain local-status surfaces unless a live-usage
flag/parameter is explicitly supplied.

### MCP tools

The MCP tool surface is stable:

- Always advertised: `search`, `research`, `provider_status`, `budget_status`.
- Advertised when the registry has an available extract-capable provider:
  `extract`, `search_and_extract`. **Since v1.3.0 the keyless `httpfetch` backstop
  is always registered, so these are advertised out of the box** (zero keys, zero
  setup) — a backward-compatible surface expansion (the tools are only ever ADDED
  to the keyless configuration, never removed). A higher-fidelity / JS-capable
  extract provider (Tavily/Firecrawl key, or local Scrapling) is still preferred
  when configured; `httpfetch` is the last-resort, no-JavaScript fallback.

Tool names AND each tool's parameter set, plus the `task` enum values, are frozen
for 1.x and **locked by tests** (the `internal/mcpserver` surface lock pins the
advertised tool set, each tool's parameters, and the task vocabulary — new optional
params may be added, existing ones are not removed/renamed). The **shape of JSON
results** (field names) is a stability commitment — additive-only in 1.x — upheld
by the behaviour test suite and release review rather than a result-snapshot lock.
**MCP `stdout` carries JSON-RPC only** (logs go to stderr) — an invariant, not just
a convention.

The post-v1.9.0 provider-status expansion intentionally adds top-level
`server_version` to the MCP `provider_status` result. It is populated from the
same build version used by the MCP initialize handshake, so agents can identify
the binary loaded by their MCP subprocess. Unstamped source builds truthfully
report `dev`. The core `nole providers --json` response remains unchanged.

The post-v1.9.0 compact MCP expansion intentionally adds the optional
`nole mcp --compact` launch mode. It advertises exactly one tool,
`web_evidence`, rather than the standard six-tool surface. Its parameters are
`input`, `depth`, `limit`, `country`, `search_lang`, `ui_lang`, `safesearch`,
and `freshness`. Exact public HTTP(S) URLs select extract,
normal text selects search + top-result extraction, and `depth: deep` selects
multi-source research. The default `nole mcp` surface and behavior are
unchanged; compact mode is opt-in and does not drive interactive browsers.
To keep signed/private URLs out of provider requests and MCP transcripts, the
compact surface rejects userinfo, query parameters, and fragments in exact URLs
and in HTTP(S) URLs embedded inside text input.

The v1.7.0 SearchOptions expansion intentionally adds optional MCP params to
`search`, `search_and_extract`, and `research`: `country`, `search_lang`,
`ui_lang`, `safesearch`, and `freshness`. For `research`, they apply to internal
search passes only. The parameter lock was updated with this doc so future MCP
parameter drift remains fail-closed and explicit.

The v1.7.0 research evidence expansion intentionally adds the optional
`evidence_steps` response field to `research` JSON/MCP/REST output. It is compact
observability metadata for search/extract status, source-extraction skips such as
PDF/Reddit sources, and sanitized errors; it is not answer synthesis, provider
ranking, or a full route-trace dump.

The v1.7.0 `search_and_extract` partial-error expansion intentionally adds
optional `routing_insight` to each `extract_errors[]` item. The field is compact,
sanitary extract-route observability for a failed URL; it does not expose full
route traces or raw provider payloads.

The post-v1.7.0 provider-usage expansion intentionally adds optional MCP param
`live_usage` to `provider_status`. When true, Nólë queries supported provider
usage APIs/header metadata, returns sanitized `remote_usage` / advisory
`remote_usage_error`, and syncs the local quota ledger. The tool always reports
the additive strategy/reason fields so agents can see each provider's quota-truth
contract without making unsupported calls. When omitted, live account querying is
not performed.

### Configuration (environment variables)

The committed (stable) env knobs — a stable variable keeps its name + meaning; the
set may grow in 1.x — are:

- **Cost/policy:** `NOLE_COST_POLICY`, `NOLE_HARD_CAP_CENTS`, `NOLE_<PROVIDER>_PAID`,
  `NOLE_<PROVIDER>_ESTIMATED_COST_CENTS`.
- **Cache/ledger:** `NOLE_CACHE_TTL` / `NOLE_CACHE_TTL_SECONDS` /
  `NOLE_CACHE_MAX_ENTRIES`, `NOLE_QUOTA_LEDGER_PATH`.
- **Reliability tuning** (per-provider HTTP retry + circuit breaker):
  `NOLE_RETRY_MAX_ATTEMPTS`, `NOLE_RETRY_BASE_DELAY_MS`, `NOLE_BREAKER_THRESHOLD`,
  `NOLE_BREAKER_COOLDOWN_MS`.
- **Diagnostics/loading:** `NOLE_LOG`, `NOLE_DISABLE_ENV_FILE`,
  `NOLE_SCRAPLING_PYTHON` (written by `nole setup --local-extract`).
- **HTTP serve auth:** `NOLE_SERVE_TOKEN` (bearer token for `nole serve`; required
  for a non-loopback bind, optional for the loopback default).
- **Update/install:** `NOLE_RELEASES_API`, and the installer family
  `NOLE_INSTALL_VERSION` / `_DIR` / `_REPO` / `_API_URL` / `_DOWNLOAD_URL` /
  `_VERIFY`.
- **Provider keys:** `BRAVE_API_KEY` / `BRAVE_SEARCH_API_KEY` / `TAVILY_API_KEY` /
  `FIRECRAWL_API_KEY`.

This is the authoritative list of the committed env surface. `nole config dump`
echoes the subset that are *runtime* config (it omits install-time vars like the
`NOLE_INSTALL_*` family and `NOLE_RELEASES_API`, and shows provider keys as
set/unset only); the standard `XDG_STATE_HOME`, the test-only
`NOLE_MCP_SMOKE_BINARY`, and the build-time `NOLE_BUILD_*` vars (used only by
`scripts/check-release-builds.sh`) are recognized but are NOT part of the committed
product surface.

### Install + integrity contract

`scripts/install.sh`, `scripts/install.ps1`, and `nole self-update` keep their
contract: **SHA256 is the mandatory integrity floor** (fail-closed on mismatch),
build-provenance attestation is an additive, best-effort gate, and the
`NOLE_INSTALL_VERIFY=auto|require|off` semantics are stable. The release artifact
matrix (`nole-<os>-<arch>` for darwin/linux × amd64/arm64 plus
`nole-windows-<arch>.exe`, with `SHA256SUMS`) is stable.

### HTTP/REST (`nole serve`)

**Stable since v1.4.0.** The `serve` command, its `--listen` / `--mcp` flags, and
the HTTP surface are a frozen 1.x contract:

- **Route set** (locked by `TestStableRESTSurface`): `/health`, `/mcp`,
  `/api/search`, `/api/extract`, `/api/search_and_extract`, `/api/research`,
  `/api/providers`, `/api/budget`. Removing/renaming a route is breaking.
- **Request-body fields are additive-only.** The v1.7.0 SearchOptions
  expansion intentionally adds an optional `options` object to `/api/search`,
  `/api/search_and_extract`, and `/api/research` with `country`, `search_lang`,
  `ui_lang`, `safesearch`, and `freshness`. For `/api/research`, options apply to
  the internal search passes only. Invalid option values are caller-controlled
  request errors and return the standard `/api/*` 400 envelope.
- **Error envelope shape** (frozen) — for `/api/*` request-decode (400), auth
  (401), and service errors (402/500): a JSON `{operation, error}` body, plus the
  additive `route` / `routing_insight` / `route_trace` on routed service errors
  (the same `core` + `safeerr` envelope as CLI/MCP, no divergent REST path).
  Method-not-allowed (405) and unknown-route (404) are standard HTTP **plain-text**
  rejections, NOT the JSON envelope; `/health` returns its own `{status, …}` shape;
  `/mcp` speaks JSON-RPC. Parse the JSON envelope on the `/api/*` 400/401/402/500
  paths; treat 404/405 as HTTP-level.
- **Status-code contract:** `200` success, `400` request-decode error, `401`
  missing/invalid bearer token, `402` `NoFreeQuotaError` (free tier exhausted /
  paid blocked), `404` unknown route (plain text), `405` wrong method (plain text),
  `500` other service errors, `503` `/health` not-ready.
- **Auth:** `NOLE_SERVE_TOKEN` sets a bearer token required on every endpoint
  except `/health` (constant-time compared; never logged). A **non-loopback bind
  requires it** — `serve` refuses to start on a non-loopback bind without a token
  (fail closed; it never serves your keys to a network unauthenticated). The
  loopback default (`127.0.0.1`) needs no token.

### Safety invariants (never weakened in 1.x)

- MCP `stdout` is JSON-RPC only; diagnostics go to stderr.
- Secrets are never printed or logged (values shown only as set/unset).
- Cost is fail-closed: no hidden paid spend; a paid provider is never selected
  merely because a key exists — only under an explicit policy + cap.

## NOT covered (may change in a minor or patch)

These are intentionally **not** frozen — integrate against them only with that in
mind:

- **Provider routing order / route matrix** — may change with benchmark or
  real-usage evidence (it does not change the *result contract*, only which
  provider serves a request).
- **`route_trace` internals and the exact `routing_insight` wording** — the
  compact insight stays present; its prose and the debug trace detail may evolve.
- **`NOLE_LOG` line format** — the fact that logs are stderr-only and redacted is
  stable; the exact text/JSON field layout is not.
- **Deterministic benchmark fixtures and numbers** — illustrative, not a contract.
- **Internal Go packages (`internal/...`)** — not a public API; no import
  compatibility is promised.
- **The local quota/cost ledger file format** — local accounting, recovered
  fail-closed on corruption; not an external contract.

## How the freeze is enforced

Surface-lock tests pin the mechanically-enforced surfaces and fail on any
add/remove/rename, forcing a conscious decision (and a matching version bump +
this doc update) rather than silent drift:

- `TestStableCLICommandSurface` (`internal/cli`) — the top-level command set.
- `TestStableMCPToolSurface{WithoutExtract,WithExtract}` (`internal/mcpserver`) —
  the advertised MCP tool set (incl. the extract-gated split).
- `TestStableMCPToolParams` / `TestStableTaskEnum` (`internal/mcpserver`) — each
  MCP tool's parameter names and the `task` enum values.
- `TestStableRESTSurface` (`internal/cli`) — the `nole serve` REST route set, the
  error-envelope field shape, and the status-code contract (incl. the 402 mapping).

CLI flags and `--json` / MCP result field names are not pinned by a snapshot lock;
their additive-only stability is a maintainer commitment enforced by the broader
behaviour test suite and release review.
