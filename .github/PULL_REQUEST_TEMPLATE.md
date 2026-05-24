## Motivation

What problem does this PR solve? Link to the issue or audit finding that prompted it.

## Changes

- [ ] Change 1 (file:line)
- [ ] Change 2 (file:line)

## Test plan

How did you verify this works? Include any of the following that apply:

- [ ] `go test ./... -short -count=1 -timeout 180s` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `gofmt -l .` returns no output
- [ ] `./scripts/audit.sh` (full local gate) exits 0
- [ ] `./scripts/secret-scan.sh` exits 0
- [ ] `govulncheck ./...` reports 0 reachable stdlib findings (if toolchain or networking code changed)
- [ ] Manual smoke against at least one client (specify which, and what command was run)

## Audit-gate status

Paste the verbatim summary lines from the gate commands you ran:

```
<paste exit codes / summary lines here>
```

## Scope and side effects

- [ ] No new dependencies introduced.
- [ ] No secrets, real API keys, or absolute home paths in the diff.
- [ ] No widening of provider-route ordering without sanitized evidence.
- [ ] No marketing or superlative claims about providers or benchmarks.
- [ ] If this touches MCP stdout, the protocol stream remains clean (logs to stderr only).

## Reviewer notes

Anything the reviewer should look at first, or known follow-ups intentionally not included in this PR.
