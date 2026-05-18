# Kimi client

Status: generic/unverified.

Nólë is a free, local web search router for AI agents and coding CLI tools. Kimi is an important secondary target, but this repository does not yet include a verified Kimi setup writer or live client test.

If the Kimi client supports MCP stdio tools, use the generic Nólë command:

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

If Kimi uses a different tool/plugin schema, adapt the command while preserving the local `nole mcp` stdio behavior.

## Verification checklist for future support

- Kimi client/version tested.
- Config path and schema documented.
- Nólë tools visible: `search`, `extract`, `provider_status`, `budget_status`.
- `nole doctor --mcp` passes.
- One small docs search works.
- Provider keys are not printed or stored in shared config.

Until this checklist is complete, call Kimi support generic/unverified only.
