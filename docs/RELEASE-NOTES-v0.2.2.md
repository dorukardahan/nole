# Nólë v0.2.2 release notes

Nólë v0.2.2 is a follow-up hardening release for the local Scrapling setup
flow introduced in v0.2.1.

## Highlights

- `nole setup --local-extract` now prefers stable versioned Python runtimes
  (`python3.13`, `python3.12`, `python3.11`, `python3.10`) before generic
  `python3` or `python`.
- The setup command now prints a short progress line before first-run Scrapling
  dependency installation, so an installing AI agent does not appear idle.

## Why this matters

On machines where `python3` points to a bleeding-edge Python, some browser
fetcher dependencies can take much longer to resolve or download. Preferring an
installed stable Python keeps the GitHub-link install path closer to the
expected flow: install Nólë, prepare local extraction, ask only for BYOK keys
when the user wants provider accounts, then start a fresh agent session.

## Safety notes

Nólë still does not vendor, copy or redistribute Scrapling code. The local
runtime is an isolated user-side venv, and `NOLE_SCRAPLING_PYTHON` is stored in
the user's local `~/.config/nole/.env`.
