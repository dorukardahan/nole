# Cursor client

Status: verified (GUI MCP path + chat-agent tool dispatch).

Nólë is a local, free-first/BYOK web search and page extraction router for AI agents and coding CLI tools. Cursor-style MCP clients can launch Nólë through `nole mcp`. Live verification on macOS Cursor 3.4.20 is recorded in `docs/CLIENTS/LIVE-VERIFICATION.md` (2026-05-20 follow-up run).

## Setup

Nólë includes a Cursor setup flag and repo coverage for the shared MCP JSON merge helper. On a fresh host, build Nólë and write the Cursor MCP entry:

```bash
go test ./...
go build -o nole .
mkdir -p ~/.local/bin
cp ./nole ~/.local/bin/nole
export PATH="$HOME/.local/bin:$PATH"
command -v nole
nole setup --cursor --local-extract
nole doctor --mcp
```

If Cursor does not inherit PATH, use the generic config template with an absolute binary path. If Cursor also does not inherit the shell environment that owns provider keys, run the writer with the wrapper flag to point the MCP entry at an env-sourcing wrapper:

```bash
nole setup --cursor --mcp-wrapper /absolute/path/to/nole-mcp
```

The wrapper template lives in `docs/PROVIDER-KEYS.md`.

## Generic config template

```json
{
  "mcpServers": {
    "nole": {
      "command": "/absolute/path/to/nole",
      "args": ["mcp"]
    }
  }
}
```

## Verification checklist

To verify Cursor on a new host (or to re-verify after a Cursor upgrade), record:

- Cursor version and config path (`~/.cursor/mcp.json`);
- the Nólë MCP entry shape after setup (wrapper-direct: `command_basename=nole-mcp`, 0 args, 0 env keys);
- that the Nólë tools `search`, `provider_status`, `budget_status`, plus `extract` and `search_and_extract` (advertised out of the box via the keyless httpfetch backstop; a Tavily/Firecrawl key or local Scrapling upgrades extract fidelity, not its availability), are visible in Cursor's MCP panel or dispatchable by name through its chat agent (`provider_status` is a no-network sanity check; one `limit=1` `free-first` DDGS `search` is the live smoke);
- that any unrelated MCP servers already present in the user's Cursor config are preserved unchanged by the writer;
- that no secrets, bearer tokens, auth headers, raw provider payloads or local user paths appear in Cursor's MCP entry, in chat output, or in any committed artifact;
- that provider keys are visible to the Cursor-launched process only via the `~/.local/bin/nole-mcp` wrapper sourcing `~/.config/nole/.env` at launch.

For the macOS 3.4.20 evidence already recorded, see `docs/CLIENTS/LIVE-VERIFICATION.md`.
