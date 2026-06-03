# Release checklist

Nólë is a public repository, but this document does not publish a GitHub
Release, create tags, upload assets, publish packages or deploy endpoints.
Release tags, package publication, hosted deployments and paid-provider spend
each require explicit maintainer approval.

Use this checklist before creating a release tag or publishing package channels.

## Decision record

Fill this section in the release PR or local release note before any public action.

| Decision | Required value before action |
| --- | --- |
| Release owner | named maintainer |
| Target version | explicit semantic version, for example `v0.1.0` |
| Public repository visibility | confirmed public |
| Release tag approved | yes/no |
| GitHub Release publication approved | yes/no |
| Release asset upload approved | yes/no; handled by tag workflow |
| Package publication approved | yes/no per channel |
| Deploy/public endpoint approved | yes/no |
| Live benchmark evidence included | optional; yes/no |
| Known limitations accepted | yes/no |

If an approval is `no`, do not perform that action. Complete only local prep and document the blocked step.

## Release-ready vs published

Release-ready means:

- local and CI gates pass without secrets;
- `doctor`, `doctor --mcp`, provider status, deterministic benchmark and public-safety checks are green;
- priority named-client verification is recorded truthfully;
- docs explain BYOK, free-first default behavior and provider overage cautions;
- build/checksum dry-runs pass.

Published means at least one explicit public action has happened, such as:

- a release tag was created;
- a GitHub Release was published;
- release assets were uploaded;
- a package was published to a registry;
- a public deployment or endpoint was exposed.

Do not use release-ready evidence to imply a GitHub Release, package or
deployment has been published.

## Pre-release gate

Before any release PR or direct release-prep commit is merged, verify:

```bash
./scripts/check-docs-framing.sh
./scripts/check-benchmark-claims.sh
./scripts/check-integration-evidence.sh
go test ./...
go vet ./...
go run . doctor
go run . doctor --mcp
go run . providers --json
go run . bench --json
go run . bench --evidence-md
./scripts/verify-integration-evidence.sh > /tmp/nole-integration-verification.md
./scripts/check-integration-evidence.sh /tmp/nole-integration-verification.md
./scripts/check-release-builds.sh
./scripts/secret-scan.sh
git diff --check
```

These commands are non-publishing. They must not require provider secrets and must not run live benchmarks.

## Public-safety checklist

Confirm the release branch and generated notes contain no:

- provider key values;
- bearer tokens;
- auth headers;
- API-key JSON fields;
- `.env` contents;
- raw provider payloads;
- private URLs or private queries;
- local transcripts or chat logs;
- provider dashboard screenshots;
- machine-specific runtime logs.

Allowed wording is presence/status only, for example `present`, `absent`, `disabled-no-key`, `premium_blocked_free_first` or `unknown quota`.

## Repository visibility checklist

Changing repository visibility is separate from creating a release. Nólë is
currently public; keep this section for future visibility changes or forks.

Before making a private repository public:

1. Run all gates in this document.
2. Review docs and tests for private paths, private queries, hidden transcripts and raw payloads.
3. Confirm `docs/CLIENTS/*` verification labels are evidence-backed.
4. Confirm benchmark docs do not make provider-ranking claims.
5. Confirm release notes describe what v0.1 does and does not claim.
6. Record explicit maintainer approval for `TARGET_REPO_VISIBILITY=public`.
7. Change visibility only after approval, then verify visibility through GitHub.

If approval is missing or ambiguous, keep the repository private.

## Release tag and GitHub Release checklist

Only after explicit approval:

1. Confirm local `main` is clean and synced.
2. Confirm latest `main` CI is green.
3. Confirm the chosen version and tag name.
4. Confirm the tag is new. Do not move or reuse an existing tag.
5. Create and push the tag only if tag approval is yes.
6. Let `.github/workflows/release.yml` build assets, generate checksums and
   publish the GitHub Release.
7. Verify the published release, assets and `SHA256SUMS`.

Do not combine tag creation, release publication and asset upload unless each action is explicitly approved.

## Package publication checklist

Package channels are optional and separately approved.

| Channel | Status for v0.1 prep | Approval required before publish |
| --- | --- | --- |
| GitHub Release binaries | preferred first public artifact channel | yes |
| Homebrew | future channel; formula can be drafted after release assets exist | yes |
| Scoop | future channel; manifest can be drafted after release assets exist | yes |
| npm wrapper | future convenience wrapper; not core v0.1 | yes |
| Docker | future optional image; avoid implying hosted SaaS | yes |

A package manifest PR may be drafted without publishing only if it uses placeholders or local checks and cannot upload assets.

## Live benchmark evidence

A live benchmark is not required for v0.1 release readiness. It can support
public claims only after a separate explicit approval and a sanitized summary.

Follow:

- `docs/LIVE-BENCHMARK-PLAN.md`
- `docs/LIVE-BENCHMARK-SUMMARY-TEMPLATE.md`

Do not run `nole bench --live` as part of this checklist unless live/keyed provider calls and any paid-capable provider participation were approved.

## Known v0.1 limitations to disclose

- Nólë is local/BYOK-first, not a hosted search SaaS.
- Default policy is `free-first`; premium-capable providers require explicit policy and account-risk acceptance.
- Local quota/cost ledger is local accounting, not provider-dashboard truth.
- Deterministic benchmark results do not measure live web quality.
- Optional live summaries are low-limit observations, not statistically significant provider rankings.
- Generic MCP clients remain generic/unverified until a named runtime is tested.
- HTTP/REST (`nole serve`) is stable as of v1.4.0 (bearer-token auth via `NOLE_SERVE_TOKEN`, required for a non-loopback bind; route/envelope/status surface-locked). MCP stdio remains the primary agent path; REST is for shared/remote setups.

## Rollback and cleanup notes

If a public action is accidentally performed:

1. Stop additional publication steps.
2. Revert visibility, release, asset or package publication where GitHub/registry policy allows.
3. Rotate any credential that might have been exposed.
4. Open a cleanup PR if docs or generated artifacts need correction.
5. Record only sanitized facts in the incident note.
