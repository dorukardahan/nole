package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandUsesNoleExecutableName(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Use != "nole" {
		t.Fatalf("expected root command use nole, got %q", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "Nólë") {
		t.Fatalf("expected short description to include visible product name Nólë, got %q", cmd.Short)
	}
}

func TestSetupConfigUsesNoleMCPServerName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if _, ok := cfg.McpServers["nole"]; !ok {
		t.Fatalf("expected nole MCP server key, got %#v", cfg.McpServers)
	}
	if _, ok := cfg.McpServers["searchmcp"]; ok {
		t.Fatalf("did not expect legacy searchmcp MCP server key")
	}
}

func TestMCPJSONSetupPreservesExistingServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	existing := `{
  "mcpServers": {
    "github": {
      "command": "gh",
      "args": ["mcp"]
    }
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if err := writeMCPJSONConfig(path, launchSpec{Binary: "/usr/local/bin/nole"}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if _, ok := cfg.McpServers["github"]; !ok {
		t.Fatalf("expected existing github MCP server to be preserved, got %#v", cfg.McpServers)
	}
	if nole := cfg.McpServers["nole"]; nole.Command != "/usr/local/bin/nole" || len(nole.Args) != 1 || nole.Args[0] != "mcp" {
		t.Fatalf("expected nole MCP server to be upserted, got %#v", nole)
	}
}

func TestCodexSetupPreservesExistingConfigAndUpsertsNole(t *testing.T) {
	existing := `model = "gpt-5.5"

[mcp_servers.B12]
command = "/usr/bin/python3"
args = ["server.py"]

[mcp_servers.nole]
command = "/old/nole"
args = ["mcp"]

[mcp_servers.nole.env]
OLD = "value"

[mcp_servers.github]
command = "gh"
`

	updated := upsertCodexTomlTable(existing, "mcp_servers.nole", "# nole MCP server\n[mcp_servers.nole]\ncommand = \"/bin/sh\"\nargs = [\"-lc\", \"exec /new/nole mcp\"]\n")

	if !strings.Contains(updated, `[mcp_servers.B12]`) {
		t.Fatalf("expected existing B12 MCP section to be preserved:\n%s", updated)
	}
	if !strings.Contains(updated, `[mcp_servers.github]`) {
		t.Fatalf("expected existing GitHub MCP section to be preserved:\n%s", updated)
	}
	if strings.Contains(updated, "/old/nole") || strings.Contains(updated, `[mcp_servers.nole.env]`) {
		t.Fatalf("expected old nole sections to be replaced:\n%s", updated)
	}
	if !strings.Contains(updated, `command = "/bin/sh"`) || !strings.Contains(updated, `exec /new/nole mcp`) {
		t.Fatalf("expected new shell-backed nole config:\n%s", updated)
	}
}
