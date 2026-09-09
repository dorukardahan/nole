# Platform and provider alignment research

Status: implemented in PR #126; exact-head CI and local tooling disposition are tracked at delivery. Research cutoff: 2026-09-09 (Asia/Singapore).
This record distinguishes source inspection, offline behavior tests and real-client evidence. Current-stable end-to-end MCP evidence is available for Codex; other client limits are explicit below.

## Frozen baseline and targets

Nólë local main and remote main were clean/equal at `c9314b4d1705ea0b0cc93aa43b5ba71cb7a0a3ea` (2026-08-17T12:52:46Z). Latest release: [v1.10.0](https://github.com/dorukardahan/nole/releases/tag/v1.10.0), published 2026-08-17T13:00:48Z. Installed binary reports v1.10.0 / c9314b4. The last product change is `a57881e` (experimental TinyFish, PR #111); the two later commits are documentation. Work uses a dedicated branch from this baseline.

| Client | Local installed | Previous Nólë evidence | Frozen official stable | Published UTC | Exact commit |
| --- | --- | --- | --- | --- | --- |
| Hermes | Not on local PATH | v0.19.0 / v2026.7.20, checked 2026-07-21 | [v0.21.1 / v2026.9.7](https://github.com/NousResearch/hermes-agent/releases/tag/v2026.9.7) | 2026-09-07T22:17:01Z | 2237be355906fbe6065ce1815711eee52b2d646e |
| OpenClaw | Not on local PATH | 2026.7.1 bridge check, 2026-07-17; 2026.5.18 full MCP evidence | [2026.9.3](https://github.com/openclaw/openclaw/releases/tag/v2026.9.3) | 2026-09-08T14:15:53Z | 1391f7cd2d40ab5bbcf2f5f831d3a64f520e72d7 |
| Claude Code | 2.1.266 | Historical CLI-manager evidence, exact version not recorded in that receipt | [2.1.266](https://github.com/anthropics/claude-code/releases/tag/v2.1.266) | 2026-09-08T23:55:14Z | 347b38e4a733d95b2f00690a4ca58ac1544f8a1c (public changelog repository, not CLI source) |
| Codex CLI | 0.153.4 | Historical CLI-manager evidence, exact version not recorded in that receipt | [0.153.4](https://github.com/openai/codex/releases/tag/rust-v0.153.4) | 2026-09-04T23:25:48Z | 3d2ee51ca2d5db578f328aa75e20aa22c0197c9a |

GitHub latest-release metadata marks all four targets non-prerelease. Targets are frozen. The single closure check on 2026-09-09 returned the same four stable tags; no newer stable target or baseline-main drift was found.

## Baseline evidence

Before source changes, Go 1.26.6 darwin/arm64: `go test ./...`, binary build, `doctor --mcp`, `bench --json`, and `providers --json` all exit 0. Tests and runtime commands used an explicit child environment, disposable HOME, disabled env-file loading and in-memory quota accounting. All six native MCP tools are visible. This is Nólë's own real subprocess stdio test, not proof of any external client. Representative public CLI and native Claude/Codex setup checks are recorded below.

## Coverage ledger

All source/API inspections below were made on 2026-09-09. The interval starts at the baseline commit timestamp; current contract checks also identify older gaps without assigning them a new introduction date.

| Surface / source | Code inventory and current contract | Decision / verification / residual risk |
| --- | --- | --- |
| [Brave](https://api-dashboard.search.brave.com/app/documentation/web-search/get-started) | `internal/providers/brave`: keyed web/news, filters, count/offset, normalized citations; direct HTTP, no companion MCP dependency | No change: existing web count <=20, news <=50 and request filters remain applicable. LLM Context is a deferred alternative, without comparative Nólë evidence. Keyed live calls not run |
| [Tavily](https://docs.tavily.com/documentation/api-reference/endpoint/search) | `internal/providers/tavily`: keyed search/extract, general/news topics, advanced research depth | Correct country enum and forward explicit language boost; HTTP contract tests. Strict language filtering, auto-parameters, fast/ultra-fast and include-domain boost deferred: different semantics/cost or new surface, no measured need |
| [Firecrawl](https://docs.firecrawl.dev/api-reference/endpoint/search) | `internal/providers/firecrawl`: direct keyless/BYOK, academic Research Index, separate OpenClaw bridge | Existing keyless public search/extract works before/after. Retain endpoints, limits, extraction and academic metadata. Update published rate metadata only. Developer Index/vendor quality claims insufficient to change routing |
| [TinyFish Search](https://docs.tinyfish.ai/search-api/reference), [Fetch](https://docs.tinyfish.ai/fetch-api/reference) | `internal/providers/tinyfish`: experimental keyed search/fetch; not promoted into primary defaults. Search and Fetch are credit-free but key-gated; Search page starts at 0 with a documented 30 requests/minute default, Fetch has a 150 URLs/minute default and long timeout | Preserve new documented sanitized error classes. Research date-filter exclusion and existing 150s client timeout remain appropriate. Selectors/highlights/conditional fetch and richer research metadata deferred absent demonstrated need. Existing PR #125 covers route wording; no duplicate change |
| [DDG HTML](https://duckduckgo.com/duckduckgo-help-pages/features/non-javascript) | `internal/providers/ddgs`: native Go HTML adapter, no Python ddgs dependency; empty and challenge/rate errors distinct | No change. Official non-JavaScript surface persists; third-party SDK changes do not apply. Anti-bot availability is still external and not guaranteed |
| [MediaWiki API](https://www.mediawiki.org/wiki/API:Etiquette) | `internal/providers/wikipedia`: keyless Action API, identifying User-Agent, maxlag=5, HTTP-200 API errors recognized | No change; existing normalized titles/URLs and error handling retained. Docs inspected, not a new live availability guarantee |
| [arXiv API](https://info.arxiv.org/help/api/user-manual.html) | `internal/providers/arxiv`: keyless Atom search, pagination, process-serial three-second throttle | No change. Terms require aggregate operator pacing across machines; current implementation is process-local. Fleet coordination remains outside this local router task; do not claim global enforcement |
| [Scrapling v0.4.15](https://github.com/D4Vinci/Scrapling/releases/tag/v0.4.15) | `internal/providers/scrapling`: optional local Python fetcher, callable markdown-or-text fallback; installed 0.4.8 | No dependency bump. New `Response.markdown` needs optional markdownify via rag/ai extras; existing fetchers-only setup can fall back to text. New upstream MCP server/spider changes do not affect this subprocess path. New-version runtime unverified |
| httpfetch | `internal/providers/httpfetch`: built-in Go HTTP/HTML extraction backstop, bounded bodies, public-address/redirect checks, cancellation | No provider API or account to migrate. Existing offline HTTP/body/cancellation/error tests pass; no new parser/framework/dependency |
| Mock | `internal/providers/mock`: offline test/benchmark provider | Not an external provider and not live evidence |

No endpoint, routing default, quota debit, retry policy, account, dependency, license distribution or output schema was changed. Direct adapters do not import the vendor SDKs. The provider's own remote behavior and account quotas remain outside local guarantees.

## Client contract coverage

| Target / pinned source | Relevant interval and current behavior | Decision and validation limit |
| --- | --- | --- |
| [Hermes MCP transport](https://github.com/NousResearch/hermes-agent/blob/2237be355906fbe6065ce1815711eee52b2d646e/tools/mcp_tool_transport.py) | SDK 2.x negotiation: auto attempts initialize first; falls back only on modern-only errors, not timeouts. MCP env allowlisting, tool policy and lifecycle remain client-owned | Retain stdio/wrapper, disabled unused resources/prompts and YAML merge. Existing Hermes writer tests preserve comments, sibling servers and Nólë tool policy. Source checked, stable runtime not exercised |
| [OpenClaw MCP](https://github.com/openclaw/openclaw/blob/1391f7cd2d40ab5bbcf2f5f831d3a64f520e72d7/docs/tools/mcp.md) / [Firecrawl manifest](https://github.com/openclaw/openclaw/blob/1391f7cd2d40ab5bbcf2f5f831d3a64f520e72d7/extensions/firecrawl/openclaw.plugin.json) | Native MCP discovery, tool schemas/policy, longer-lived sessions; stable Firecrawl now advertises firecrawl-free | Existing capability detection supports full bridge. Correct old stable fetch-only docs; preserve dedicated-wrapper opt-in and native policy. Do not use native `mcp set` to overwrite an existing policy-bearing entry blindly. No real gateway/tool invocation in this run |
| [Claude changelog](https://github.com/anthropics/claude-code/blob/347b38e4a733d95b2f00690a4ca58ac1544f8a1c/CHANGELOG.md) | Recent discovery-before-initialize, config race and HTTP/SSE fixes are upstream. Nólë uses stdio and the native manager at user scope | No setup writer change. Real stable native add/get and six-tool discovery pass. No authenticated/model-driven tool call; no claim of private CLI source access |
| [Codex MCP protocol](https://github.com/openai/codex/blob/3d2ee51ca2d5db578f328aa75e20aa22c0197c9a/codex-rs/app-server-protocol/src/protocol/v2/mcp.rs) | TOML command/args/env, stdio, status discovery and explicit app-server tool dispatch remain supported | Real stable setup, discovery, provider_status and search pass before/after. No model inference. Sibling configuration preserved. Existing permission/timeout/cancellation policy is retained |

Nólë continues to provide native CLI commands and six MCP stdio tools. It does not become an HTTP MCP host, replace clients' native web tools, or claim to bypass their sandbox/approval policy. Existing subprocess tests cover protocol-clean stdout and cancellation. OpenClaw's Firecrawl bridge is a separate explicit integration, not a general HTTP MCP transport.

The available local runtimes were Node 22.23.1 and Python 3.14.7. Frozen OpenClaw requires newer Node (24.16+ or supported 26.x); frozen Hermes declares Python >=3.11,<3.14. Neither client executable was on PATH. No replacement runtime or client was installed for this task. Thus their stable native round-trips remain a delivery limitation, not fixture-based verification. Live VPS installations were not used or modified.

## Initial findings (2026-09-09)

| Finding / source | Current behavior and affected path | Decision, benefit and risk | Evidence |
| --- | --- | --- | --- |
| [Tavily country contract](https://docs.tavily.com/documentation/api-reference/endpoint/search#body-country) | `internal/providers/tavily/tavily.go` forwards ISO codes, although upstream requires country names and only supports country with general topic | Required correction: map documented country names; omit unsupported boosts for news/factcheck or unknown codes. Keeps the public two-letter option and task routing. Pre-existing gap; no claim it was introduced after baseline | HTTP contract fixture initially fails 6 cases; after correction all Tavily tests pass |
| [Tavily August changelog](https://docs.tavily.com/changelog) language boosting | Explicit `search_lang` previously ignored by Tavily | Useful bounded improvement: send `language` only when supplied. No strict filtering, auto-parameter selection, depth or default routing change. The SDK addition merged after baseline on 2026-08-24; exact API enablement day is not established | Same HTTP contract test checks language forwarding and absent defaults; all Tavily tests pass |
| [TinyFish Fetch errors](https://docs.tinyfish.ai/fetch-api/reference) | `login_required` and `content_too_large` collapsed to `provider_error` | Useful correction: preserve these two documented, payload-free classes. Unknown error text remains redacted; no login, credential request or retry expansion | Both per-URL HTTP-200 cases fail before, pass after; complete TinyFish tests pass |
| [OpenClaw stable Firecrawl manifest](https://github.com/openclaw/openclaw/blob/1391f7cd2d40ab5bbcf2f5f831d3a64f520e72d7/extensions/firecrawl/openclaw.plugin.json) | Stable now advertises `firecrawl-free`; Nólë already selects full mode when advertised | Runtime no-op; correct stale fetch-only documentation based on pinned manifest. Keep explicit dedicated-wrapper opt-in and host policy | Pinned source inspection; not a real host call yet |
| [Hermes transport negotiation](https://github.com/NousResearch/hermes-agent/blob/2237be355906fbe6065ce1815711eee52b2d646e/tools/mcp_tool_transport.py) | SDK 2.x adds stateless protocol, but default `auto` tries initialize first and only falls back on modern-only errors | No forced Nólë protocol rewrite: old stdio handshake remains supported. Preserve timeouts/cancellation; no compatibility claim from source alone | Pinned source inspected; real-client test not run |
| [Tavily official announcement](https://x.com/tavilyai/status/2093353586356851125), 2026-08-28 | NanoClaw-specific keyless integration advertised | Deferred: does not establish generic direct API keyless access; retain Nólë BYOK requirement | Official account linked by Tavily docs; read using tweet CLI interval search |

Official X provenance captured from provider-owned pages: Tavily docs → `tavilyai`; TinyFish docs → `Tiny_Fish`; Firecrawl docs → `firecrawl`; Brave API page → `bravesearchapi`. Additional provenance: official Scrapling release links `Scrapling_dev`; DuckDuckGo documentation links `duckduckgo`. Targeted interval searches found no relevant Brave or DuckDuckGo matches; verified API-specific X provenance for MediaWiki/arXiv was not established. Search results are discovery evidence, not an exhaustive archive or proof of API availability.

The first new Tavily test attempt had a test-helper compilation error (incorrect option names); corrected before observing the six actual behavioral failures. It is not counted as a regression reproduction. TinyFish had two actual behavioral failures. Post-change `go test ./internal/providers/tavily ./internal/providers/tinyfish -count=1` exits 0.

## Real baseline checks (isolated, 2026-09-09)

- Native CLI search: `Go net/http Client Timeout documentation`, docs task, limit 1, free-first, no keys, cache disabled: exit 0, one result through keyless Firecrawl. Extract `https://go.dev/doc/`: exit 0, keyless Firecrawl, 16,962 content characters and content-safety receipt. Configured-local Scrapling was absent in this disposable environment and skipped normally. These two samples establish availability only, not a quality/speed benchmark. The search receipt is per result; a top-level `content_safety` presence check is not meaningful for that response.
- Claude Code 2.1.266: Nólë setup instructions applied by the native `claude mcp add ... -s user`; `mcp get nole` reports connected. The real headless SDK control protocol (`initialize`, `mcp_status`) reports all six Nólë tools connected. No user message/model inference sent. Tool dispatch through Claude has not been verified. Process closed with exit 0.
- Codex 0.153.4: `nole setup --codex` output is read by native `codex mcp get nole --json`; a preseeded disabled sibling server survives. Real `codex app-server` accepts initialize, lists all six Nólë tools, starts an ephemeral thread and directly dispatches `provider_status` and the same limit-1 public search via `mcpServer/tool/call`. Both return successful non-error tool content. No model turn/inference was requested. Process closed with exit 0.
- Local Scrapling runtime is 0.4.8; official latest is [v0.4.15](https://github.com/D4Vinci/Scrapling/releases/tag/v0.4.15), published 2026-08-23. Nólë's Python extraction already invokes callable `markdown` attributes and falls back for older versions. New upstream MCP-server breaking changes do not target this subprocess adapter. New-version extraction was not run; no local upgrade performed.

## Additional source findings

- [Firecrawl pricing](https://www.firecrawl.dev/pricing), effective 2026-09-04, still states 1,000 monthly free credits and two credits per ten search results. Existing 250-call conservative floor remains unchanged. Published free Search rate now says 10 requests/minute; Nólë descriptive metadata was corrected from 5 to 10. Keyless starter limits are separate. Failed scrape without a result is uncharged; an error-status page returned as a result can still cost one credit. Do not equate every failed extraction with free usage.
- [Tavily pricing](https://docs.tavily.com/documentation/api-credits): advanced search costs two credits; extraction charges one/two credits per five successful URLs for basic/advanced. Existing 500-call floor remains conservative due to advanced search, but the misleading per-call extraction wording was corrected.
- [Brave pricing](https://brave.com/search/api/) still advertises $5 per 1,000 Search requests and $5 monthly credits. Keep the current 1,000-call estimate and paid opt-in. The official page also offers LLM Context; a switch would alter normalized results and requires comparative evidence, so it is deferred.
- [arXiv API terms](https://info.arxiv.org/help/api/tou.html) still require one serial request per three seconds across all machines under the operator. Existing process-local throttling is useful but does not coordinate separate processes/hosts; do not claim fleet-wide enforcement.
- [MediaWiki etiquette](https://www.mediawiki.org/wiki/API:Etiquette) still requests identifying User-Agent, caching and backpressure. Nólë already sends a version/contact User-Agent and maxlag=5 and distinguishes HTTP-200 API errors from valid empty results. No adapter change selected.
- [DuckDuckGo non-JavaScript documentation](https://duckduckgo.com/duckduckgo-help-pages/features/non-javascript) retains HTML/lite surfaces. Nólë uses the HTML endpoint directly; changes to the third-party Python ddgs package do not automatically apply. Anti-bot availability is not guaranteed.
- Official X interval searches found Firecrawl's keyless announcement ([2026-08-29](https://x.com/firecrawl/status/2093504782321188890)) and [pricing announcement](https://x.com/firecrawl/status/2095908277842284661); existing keyless baseline worked. Vendor benchmark claims were not adopted as Nólë evidence.
- TinyFish official [Search announcement](https://x.com/Tiny_Fish/status/2096025553979703394) aligns with its documented credit-free Search. Access still requires an eligible key; no default route promotion. The bounded interval search returned recent results and is not exhaustive.
- Brave's provider-owned page links `bravesearchapi`; the requested interval search returned no matches. That is missing announcement evidence, not proof that nothing changed.

The Claude SDK control contract was inspected in official `anthropics/claude-agent-sdk-python` at `6bbd3093147c2fadcd4b868599b8fb6d9db3d523`; no local SDK installed/upgraded and no claim to Claude Code private source access. Codex direct MCP protocol was inspected at the frozen CLI tag.

## SDK and announcement provenance

Official SDK/source inspection supplements API documentation; no SDK dependency was added:

- [Tavily Python language addition, PR #187](https://github.com/tavily-ai/tavily-python/pull/187), merged 2026-08-24T15:47:45Z (`821b6ca9abf2d9887c6cdca94e588590c6223507`), confirms the language opportunity after baseline. Inspected SDK main `d10ab8e20d4d86922a549711dea8b19fa1f42b46`; API contract, not SDK implementation, controls Nólë's HTTP request.
- Firecrawl canonical repository is [firecrawl/firecrawl](https://github.com/firecrawl/firecrawl) (old owner redirects), inspected `e847d994264f115fe672c4d40bfd2e70329c9243`. Latest GitHub server release v2.11.0 predates the interval; hosted API pricing/features can change independently. SDK country support and usage-count fixes do not require adopting that SDK. Server AGPL code is not bundled.
- [Brave companion MCP](https://github.com/brave/brave-search-mcp-server), inspected `fca821db68a4bc99d5f5b30396fac222b5100b69`: SDK dependency/argument maintenance is separate from Nólë's direct web/news adapter. No source import or license change.
- TinyFish official docs link [tinyfish-io/tinyfish-cookbook](https://github.com/tinyfish-io/tinyfish-cookbook), inspected `8615317f6db58ae776dd53817ac30668c1db5ef8`; this is examples, not proof of proprietary backend implementation.
- Scrapling's [markdown announcement](https://x.com/Scrapling_dev/status/2094578183760752870) and [spider announcement](https://x.com/Scrapling_dev/status/2093011694721097854) were checked against v0.4.15 source. Optional dependencies and existing fallback explain the no-upgrade decision.

X searches used the local read-only tweet CLI with the baseline/cutoff interval and accounts linked from official pages/releases. Bounded search is not an exhaustive announcement archive. No-result searches and missing official-account provenance remain explicit evidence gaps. API/docs, not announcements or cached snippets, establish availability and price statements.

## Candidate verification and comparison

The candidate passed `./scripts/audit.sh` (exit 0), including full Go tests/vet, docs/version/benchmark guards, own real stdio doctor, offline benchmark and integration-evidence check; a standalone build also passed. PowerShell installer parse and Homebrew formula style passed. Only the optional Clawpatch check was skipped. No assertions or quality gates were weakened.

| Check | Baseline | Candidate | Meaning |
| --- | --- | --- | --- |
| Tavily country/language HTTP contract | Six behavioral cases fail | All pass | Documented request shape; no keyed production API call |
| TinyFish new per-URL errors | Two cases become provider_error | Both preserve safe classes | Better diagnostics without upstream payload leakage |
| Public docs search, same query, limit 1, cache off | Firecrawl, 1 result, exit 0 | Firecrawl, 1 result, exit 0 | Keyless availability retained; not a quality benchmark |
| Public go.dev/doc extraction, cache off | Firecrawl, 16,962 chars, safety receipt | Firecrawl, 16,962 chars, safety receipt | Representative extraction preserved; no general fidelity claim |
| Codex 0.153.4 native app-server | Six tools; provider_status + search succeed | Same | Real client tool dispatch, no model turn |
| Claude Code 2.1.266 native manager/control | Connected, six tools | Same | Discovery only; tool invocation unverified |
| Own CLI/setup/MCP/fallback regression suite | Pass | Pass | Includes sibling/unknown-field/mode preservation, wrapper spaces, idempotence, stderr/stdout isolation and cancellation; offline fixtures are not vendor/client live evidence |

The native probes use disposable HOME/config, absolute candidate binary paths, disabled env-file loading and explicit child-environment allowlists. Codex uses initialize, mcpServerStatus/list, ephemeral thread/start and mcpServer/tool/call; Claude uses native mcp add and headless control initialize/mcp_status without an inference message. Both subprocesses exit 0 after input closure. The sample search was `Go net/http Client Timeout documentation`, task docs, limit 1; extraction was `https://go.dev/doc/`. No new account or paid benchmark was used. Tavily/TinyFish production calls remain untested.

One local validation side effect must be disclosed: Homebrew's `brew style` step automatically installed/cleaned its global Ruby helper gems despite disposable HOME and HOMEBREW_NO_AUTO_UPDATE. The step passed, but this exceeded the intended no-live-install-change boundary. The official installed Homebrew code calls bundle install/clean independently of auto-update. No exact pre-run bundle backup exists; automatic rollback was not attempted. User disposition is pending. No Nólë or target-client global upgrade, live application config write, deployment, merge or release occurred.

## Local review notes

`git diff --check` and all documentation guards pass. Direct review covered changed code with neighboring request/topic/depth logic, the complete country map against the documented enum, TinyFish's existing unknown-code redaction test, and unchanged sibling-provider option handling. Old metering strings remain only in historical v0.7.1 release notes. Codex review caught two missed OpenClaw stable/fetch-only descriptions; the setup summary, provider guidance and neighboring source comment now describe installed capabilities, and the dated old-host receipt explicitly says then-stable. Focused OpenClaw/setup regressions pass.

The required tracked-only filename-only heuristic secret scan ran inside workspace-no-secrets for both the clean baseline and staged candidate. Both returned the identical 23-file review-required list; no new filename was flagged. Changed lines and new files were reviewed directly and contain no credential values. This is a bounded heuristic/diff review, not proof that the entire repository contains no secrets. The repository's public-safety CI remains a separate required check. No local govulncheck binary was available; the configured CI job will provide that result.

## Remaining delivery checks

- Direct diff/secret review and commit completed; [PR #126](https://github.com/dorukardahan/nole/pull/126) is open.
- Exact-head CI and review tracked on PR #126. Codex integration is active: its first review identified stale OpenClaw setup guidance, addressed in a follow-up. Final-head review must be checked after that push.
- Closure stable-release/main check completed without target changes.
- Record user disposition of the Homebrew helper side effect. Remaining real-client/provider gaps above must remain visible in the PR.
