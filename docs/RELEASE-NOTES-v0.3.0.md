# Nólë v0.3.0 release notes

Nólë v0.3.0 adds native MCP setup writers for Gemini CLI and Grok CLI, a `nole
version` command, and a set of correctness/stability fixes to the provider
retry path, snippet handling, cache and quota ledger.

## Added

- `nole setup --gemini` writer for Gemini CLI (`google-gemini/gemini-cli`):
  merges a `nole` entry into `~/.gemini/settings.json`'s `mcpServers` object,
  preserving unknown root keys and sibling servers, with `.bak` backup and
  preserved permissions. Config shape verified from primary source; status is
  `repo-tested` (the real client was not launched in this environment). See
  `docs/CLIENTS/gemini.md`.
- `nole setup --grok` writer for Grok CLI (`superagent-ai/grok-cli`): upserts a
  `nole` entry into the `mcp.servers` array (keyed by an `id` field) in
  `~/.grok/user-settings.json`, preserving other servers, unknown per-entry
  fields, user `label`/`enabled`, and unknown root keys. Config shape verified
  from primary source; status is `repo-tested`. See `docs/CLIENTS/grok.md`.
- `nole version` command, which prints the binary's version, commit, and build
  date. Release builds now stamp `Commit` and `Date` via `ldflags` alongside
  the existing `Version`.
- `NOLE_CACHE_MAX_ENTRIES` to cap the in-process search/extract cache size
  (default `1024`).

## Changed

- Provider HTTP retries now cover transport-level failures (connection reset,
  DNS blip, dropped keep-alive) and HTTP `408`, instead of returning on the
  first transport error; a dead context is still not retried.
- The in-process cache is now bounded (FIFO eviction of the oldest entry past
  the cap) so a long-lived MCP server cannot grow it without limit.
- Snippet/content truncation is now rune-safe (`core.TruncateRunes`),
  preventing mid-UTF-8 mojibake in non-ASCII results.

## Fixed

- DDGS result snippets are now anchored to the correct result by byte offset; a
  skipped ad row no longer shifts every subsequent organic snippet.
- A future-dated quota `PeriodStart` (clock skew / copied ledger) now
  self-heals instead of stranding a provider as permanently exhausted.

## Verified

- `go test ./...`
- `go vet ./...`
- `go run . doctor`
- `go run . doctor --mcp`
- `go run . bench --json`
