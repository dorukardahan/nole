# OpenClaw client

Status: generic/unverified.

Nólë is a free, local web search router for AI agents and coding CLI tools. OpenClaw is a priority v0.1 target, but this repository does not yet contain a verified OpenClaw setup writer or live client test.

Use the generic MCP stdio template until OpenClaw-specific config is verified.

## Generic MCP template

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

If OpenClaw uses a different schema, adapt the command and args while preserving the same launch behavior: `nole mcp` over stdio.

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

## Verification checklist for future support

Upgrade status to `verified` only after recording:

- OpenClaw version tested;
- config file path and schema;
- exact Nólë config entry;
- `nole doctor --mcp` result;
- OpenClaw MCP tool visibility for `search`, `extract`, `provider_status`, `budget_status`;
- one successful low-limit docs search;
- provider key handling without secret leakage;
- common failure notes.

## Troubleshooting

- Use an absolute path to `nole`.
- Ensure provider keys are available in the OpenClaw launch environment.
- If OpenClaw supports logs, inspect for MCP protocol errors but redact secrets.
- Do not claim verified support until the checklist above is complete.
