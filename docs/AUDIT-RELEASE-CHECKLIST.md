# Audit and Release Checklist

Use this checklist before merging, preparing a public release, or installing Nole into an agent runtime.

## Local audit

```bash
scripts/audit.sh
```

Optional Clawpatch smoke, using a local checkout of `openclaw/clawpatch`:

```bash
REQUIRE_CLAWPATCH=1 scripts/audit.sh
```

The Clawpatch smoke writes state outside the repo under `/tmp/nole-clawpatch-smoke-*`.

## Safety checks

The unified audit checks:

- Go formatting, tests and `go vet`;
- docs/product framing guards;
- benchmark and integration evidence guards;
- deterministic doctor, MCP doctor, benchmark and provider-status commands;
- whitespace errors in the working tree or CI diff.

Before changing routing, provider ordering, cost policy, cache behavior or live benchmark claims, update sanitized evidence first.

## Release gate

Nólë publishes GitHub Release binaries from approved semantic version tags. Do
not push release tags, publish packages or deploy hosted endpoints unless the
release checklist explicitly passes and Doruk approves that release step.

Never print API keys, tokens, auth headers, private provider payloads or private URLs while collecting verification evidence.
