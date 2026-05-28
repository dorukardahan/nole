# Nólë v0.2.4 release notes

Nólë v0.2.4 tightens Hermes Agent v2026.5.28 / v0.15.0 setup compatibility.

## Changed

- `nole setup --hermes` now writes `tools.resources=false` and
  `tools.prompts=false` for new Nólë MCP entries so Hermes does not add
  resource/prompt utility wrappers to Nólë's native tool surface.
- Hermes client docs now recommend the wrapper/env-file setup path for
  provider keys and local Scrapling because Hermes v0.15 filters stdio MCP
  subprocess environments by default.

## Verified

- `go test ./...`
- `go vet ./...`
- `go run . doctor`
- `go run . doctor --mcp`
- `go run . bench --json`
