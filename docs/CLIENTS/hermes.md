# Hermes Agent client

Status: generic/unverified.

Nólë is a free, local web search router for AI agents and coding CLI tools. Hermes Agent is a priority v0.1 target, but this repository does not yet include a verified Hermes setup writer or a documented local Hermes MCP config test.

Use the generic MCP stdio template until Hermes-specific config is verified.

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

If Hermes uses a YAML/TOML config, map the same command/args into that schema.

## Install Nólë first

```bash
git clone https://github.com/dorukardahan/nole.git
cd nole
go test ./...
go build -o nole .
./nole doctor
./nole doctor --mcp
```

## Provider keys

Hermes launch environments may differ between CLI, gateway and service modes. Keep provider keys in the process environment or a local env file/wrapper. Do not print values.

## Verification checklist for future support

Upgrade status to `verified` only after recording:

- Hermes version/config mode tested;
- config path and schema;
- exact Nólë MCP entry;
- `nole doctor --mcp` result;
- Hermes tool visibility for `search`, `extract`, `provider_status`, `budget_status`;
- one successful low-limit docs search;
- confirmation no provider keys or auth headers appear in logs;
- troubleshooting notes for gateway/service environment inheritance.

## Suggested first prompt

```text
Use Nólë to search for Go net/http Client Timeout documentation. Include one compact Nólë routing insight and cite result URLs.
```

## Troubleshooting

- Use absolute paths if the Hermes process cannot find `nole`.
- Restart the relevant Hermes process after MCP config changes.
- If keys are missing, check the environment of the process that launches MCP tools, not just the interactive shell.
- Do not enable hosted/proxy behavior unless the user explicitly requests it.
