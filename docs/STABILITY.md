# Stability commitment (v1.0.0)

As of **v1.0.0**, Nólë commits to [Semantic Versioning](https://semver.org/) for
the surfaces listed below. The goal is simple: an agent or script that integrates
Nólë at 1.x should keep working across 1.x upgrades without changes.

- **MAJOR** (2.0.0): a breaking change to a stable surface — a removed/renamed
  command, flag, MCP tool, or response field; a changed default that alters
  behaviour; a tightened input contract.
- **MINOR** (1.x.0): additive, backward-compatible — a new command, flag, MCP
  tool, optional response field, or env var.
- **PATCH** (1.0.x): bug fixes and internal changes with no surface impact.

## Stable surfaces (frozen for 1.x)

### CLI commands

These top-level commands and their documented behaviour are stable:

`search`, `classify`, `route-plan`, `extract`, `research`, `bench`, `providers`,
`doctor`, `config`, `mcp`, `serve`, `setup`, `version`, `self-update`.

Their primary flags are stable too (e.g. `--json`, `--task`, `--insight`,
`doctor --mcp` / `--json` / `--check-updates`, `self-update --check-only` /
`--version` / `--verify`). `--json` output of `search`, `extract`, `classify`,
`route-plan`, `providers`, `doctor`, `config dump`, `bench` keeps its existing
fields; new fields may be ADDED (minor), existing ones are not removed/renamed in
1.x. The set is locked by a test (`internal/cli` surface lock) so it cannot drift
silently.

### MCP tools

The MCP tool surface is stable:

- Always advertised: `search`, `research`, `provider_status`, `budget_status`.
- Advertised when an extract-capable provider (Tavily/Firecrawl key, or local
  Scrapling) is configured: `extract`, `search_and_extract`.

Tool names, their required/optional parameters, and the shape of their JSON
results are frozen for 1.x (new optional params/fields may be added). Locked by a
test (`internal/mcpserver` surface lock). **MCP `stdout` carries JSON-RPC only**
(logs go to stderr) — an invariant, not just a convention.

### Configuration (environment variables)

The documented env vars are stable: `NOLE_COST_POLICY`, `NOLE_HARD_CAP_CENTS`,
`NOLE_<PROVIDER>_PAID`, `NOLE_QUOTA_LEDGER_PATH`, `NOLE_CACHE_TTL` /
`NOLE_CACHE_TTL_SECONDS`, `NOLE_LOG`, `NOLE_RELEASES_API`, and the installer family
`NOLE_INSTALL_VERSION` / `_DIR` / `_REPO` / `_API_URL` / `_DOWNLOAD_URL` /
`_VERIFY`. Provider keys (`BRAVE_API_KEY` / `BRAVE_SEARCH_API_KEY` /
`TAVILY_API_KEY` / `FIRECRAWL_API_KEY`) are stable.

### Install + integrity contract

`scripts/install.sh`, `scripts/install.ps1`, and `nole self-update` keep their
contract: **SHA256 is the mandatory integrity floor** (fail-closed on mismatch),
build-provenance attestation is an additive, best-effort gate, and the
`NOLE_INSTALL_VERIFY=auto|require|off` semantics are stable. The release artifact
matrix (`nole-<os>-<arch>` for darwin/linux × amd64/arm64 plus
`nole-windows-<arch>.exe`, with `SHA256SUMS`) is stable.

### Safety invariants (never weakened in 1.x)

- MCP `stdout` is JSON-RPC only; diagnostics go to stderr.
- Secrets are never printed or logged (values shown only as set/unset).
- Cost is fail-closed: no hidden paid spend; a paid provider is never selected
  merely because a key exists — only under an explicit policy + cap.

## NOT covered (may change in a minor or patch)

These are intentionally **not** frozen — integrate against them only with that in
mind:

- **HTTP/REST (`nole serve`)** — experimental; the route shapes and behaviour may
  change as it gains the hardening CLI/MCP already have.
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

The stable CLI-command and MCP-tool sets are pinned by surface-lock tests
(`TestStableCLICommandSurface`, `TestStableMCPToolSurface*`). They fail on any
add/remove/rename, forcing a conscious decision (and a matching version bump +
this doc update) rather than silent drift.
