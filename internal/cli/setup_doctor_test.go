package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMCPJSONConfigMergesExistingServersAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	existing := `{"mcpServers":{"other":{"command":"other","args":["serve"]}}}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPJSONConfig(path, "/usr/local/bin/nole"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg mcpConfig
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(b))
	}
	if _, ok := cfg.McpServers["other"]; !ok {
		t.Fatalf("existing server was not preserved: %#v", cfg.McpServers)
	}
	if got := cfg.McpServers["nole"].Command; got != "/usr/local/bin/nole" {
		t.Fatalf("nole command = %q", got)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if string(backup) != existing {
		t.Fatalf("backup content changed: %s", string(backup))
	}
}

func TestWriteOpenCodeConfigMergesExistingMCPAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	existing := `{"mcp":{"other":{"command":"other","args":["serve"]}},"theme":"dark"}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeOpenCodeConfigPath(path, "/usr/local/bin/nole"); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	var cfg struct {
		MCP   map[string]mcpServerEntry `json:"mcp"`
		Theme string                    `json:"theme"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(b))
	}
	if cfg.Theme != "dark" {
		t.Fatalf("existing non-MCP fields not preserved: %#v", cfg)
	}
	if _, ok := cfg.MCP["other"]; !ok {
		t.Fatalf("existing opencode MCP server was not preserved: %#v", cfg.MCP)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestWriteCodexConfigMergesNoleSectionAndCreatesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	existing := "model = \"gpt-5\"\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfigPath(path, "/usr/local/bin/nole"); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"model = \"gpt-5\"", "[mcp_servers.other]", "[mcp_servers.nole]", "command = \"/usr/local/bin/nole\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged codex config missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}

func TestDoctorCommandMCPFlagReportsStdioSmoke(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor", "--mcp"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor --mcp failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "mcp:") || !strings.Contains(text, "stdout") {
		t.Fatalf("doctor --mcp should report MCP stdout health, got:\n%s", text)
	}
}

func TestMCPStdioSmokeDoesNotWriteStartupNoiseToStdout(t *testing.T) {
	result := checkMCPStdioSmoke(context.Background())
	if !result.OK {
		t.Fatalf("expected mcp stdio smoke ok, got: %#v", result)
	}
	if result.StdoutBytes != 0 {
		t.Fatalf("expected no startup stdout bytes, got %d", result.StdoutBytes)
	}
}
