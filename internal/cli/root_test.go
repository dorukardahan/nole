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
	if err := writeMCPJSONConfig(path, "/usr/local/bin/nole"); err != nil {
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
