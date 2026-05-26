# Nólë v0.2.1 release notes

Nólë v0.2.1 tightens the GitHub-link install path for AI agents. The main
change is that local Scrapling extraction no longer has to be hand-wired by
the user after install.

## Highlights

- `nole setup --local-extract` creates an isolated Python venv at
  `~/.local/share/nole/scrapling-venv`, installs `scrapling[fetchers]`, writes
  `NOLE_SCRAPLING_PYTHON` to `~/.config/nole/.env` and generates
  `~/.local/bin/nole-mcp`.
- Nólë commands now load `~/.config/nole/.env` automatically without
  overriding values already present in the process environment.
- Client docs now describe `extract` as conditional: it appears when Tavily,
  Firecrawl or local Scrapling is configured.
- Agent install docs now make local extraction part of the standard
  GitHub-link install checklist.

## Legal and safety notes

Nólë does not vendor, copy or redistribute Scrapling code. It optionally
installs the user-side `scrapling[fetchers]` Python package into a local venv.
Scrapling's PyPI metadata lists BSD-3-Clause licensing. Users still need to
respect target website terms, robots.txt and rate limits.
