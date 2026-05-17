package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newSetupCommand() *cobra.Command {
	var all bool
	var claude bool
	var cursor bool
	var codex bool
	var opencode bool
	var windsurf bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure AI agents to use nole as MCP server",
		Long:  "Writes MCP server configuration files for supported AI coding agents.\nSupports: --claude, --cursor, --codex, --opencode, --windsurf, or --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				claude, cursor, codex, opencode, windsurf = true, true, true, true, true
			}
			if !claude && !cursor && !codex && !opencode && !windsurf {
				return fmt.Errorf("specify at least one agent: --claude, --cursor, --codex, --opencode, --windsurf, or --all")
			}

			binary, err := os.Executable()
			if err != nil {
				binary = "nole"
			}

			// Resolve to absolute path
			absBinary, err := filepath.Abs(binary)
			if err != nil {
				absBinary = binary
			}

			configured := 0

			if claude {
				if err := writeClaudeConfig(absBinary); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "claude: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "claude: configured")
					configured++
				}
			}
			if cursor {
				if err := writeCursorConfig(absBinary); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "cursor: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "cursor: configured")
					configured++
				}
			}
			if codex {
				if err := writeCodexConfig(absBinary); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "codex: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "codex: configured")
					configured++
				}
			}
			if opencode {
				if err := writeOpenCodeConfig(absBinary); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "opencode: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "opencode: configured")
					configured++
				}
			}
			if windsurf {
				if err := writeWindsurfConfig(absBinary); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "windsurf: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "windsurf: configured")
					configured++
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n%d agent(s) configured. Set env vars before starting:\n", configured)
			fmt.Fprintln(cmd.OutOrStdout(), "  export JINA_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export FIRECRAWL_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export BRAVE_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export TAVILY_API_KEY=...")
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "configure all supported agents")
	cmd.Flags().BoolVar(&claude, "claude", false, "configure Claude Code")
	cmd.Flags().BoolVar(&cursor, "cursor", false, "configure Cursor")
	cmd.Flags().BoolVar(&codex, "codex", false, "configure Codex CLI")
	cmd.Flags().BoolVar(&opencode, "opencode", false, "configure OpenCode")
	cmd.Flags().BoolVar(&windsurf, "windsurf", false, "configure Windsurf")
	return cmd
}

// mcpConfig is the common MCP server config format used by Claude Code and Cursor.
type mcpConfig struct {
	McpServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func writeClaudeConfig(binary string) error {
	// Claude Code: .mcp.json in project root or ~/.claude/mcp.json
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude", "mcp.json")
	return writeMCPJSONConfig(path, binary)
}

func writeCursorConfig(binary string) error {
	// Cursor: ~/.cursor/mcp.json
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	return writeMCPJSONConfig(path, binary)
}

func writeWindsurfConfig(binary string) error {
	// Windsurf: ~/.codeium/windsurf/mcp_config.json
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	return writeMCPJSONConfig(path, binary)
}

func writeMCPJSONConfig(path, binary string) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers := map[string]mcpServerEntry{}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("parse existing mcpServers: %w", err)
		}
	}
	servers["nole"] = noleMCPServerEntry(binary)
	encoded, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal mcpServers: %w", err)
	}
	root["mcpServers"] = encoded

	return writeJSONConfig(path, root)
}

func writeCodexConfig(binary string) error {
	// Codex CLI: ~/.codex/config.toml (TOML format)
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex", "config.toml")
	return writeCodexConfigPath(path, binary)
}

func writeCodexConfigPath(path, binary string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	existing, exists, err := readExistingFile(path)
	if err != nil {
		return err
	}
	if exists {
		if err := writeBackup(path, existing); err != nil {
			return err
		}
	}
	content := mergeCodexMCPServer(string(existing), binary)
	return os.WriteFile(path, []byte(content), 0644)
}

func writeOpenCodeConfig(binary string) error {
	// OpenCode: opencode.json in current directory or home
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "opencode.json")
	return writeOpenCodeConfigPath(path, binary)
}

func writeOpenCodeConfigPath(path, binary string) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers := map[string]mcpServerEntry{}
	if raw, ok := root["mcp"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("parse existing opencode mcp: %w", err)
		}
	}
	servers["nole"] = noleMCPServerEntry(binary)
	encoded, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal opencode mcp: %w", err)
	}
	root["mcp"] = encoded

	return writeJSONConfig(path, root)
}

func writeJSONConfig(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if existing, exists, err := readExistingFile(path); err != nil {
		return err
	} else if exists {
		if err := writeBackup(path, existing); err != nil {
			return err
		}
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return os.WriteFile(path, b, 0644)
}

func noleMCPServerEntry(binary string) mcpServerEntry {
	return mcpServerEntry{Command: binary, Args: []string{"mcp"}}
}

func readJSONObject(path string) (map[string]json.RawMessage, error) {
	existing, exists, err := readExistingFile(path)
	if err != nil {
		return nil, err
	}
	if !exists || len(strings.TrimSpace(string(existing))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &root); err != nil {
		return nil, fmt.Errorf("parse existing json config: %w", err)
	}
	return root, nil
}

func readExistingFile(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return b, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read existing config: %w", err)
}

func writeBackup(path string, content []byte) error {
	if err := os.WriteFile(path+".bak", content, 0644); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

func mergeCodexMCPServer(existing, binary string) string {
	block := fmt.Sprintf("[mcp_servers.nole]\ncommand = %q\nargs = [\"mcp\"]\n", binary)
	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return "# nole MCP server\n" + block
	}
	start := strings.Index(trimmed, "[mcp_servers.nole]")
	if start == -1 {
		return trimmed + "\n\n# nole MCP server\n" + block
	}
	end := len(trimmed)
	remaining := trimmed[start+len("[mcp_servers.nole]"):]
	if next := strings.Index(remaining, "\n["); next != -1 {
		end = start + len("[mcp_servers.nole]") + next + 1
	}
	return strings.TrimRight(trimmed[:start], "\n") + "\n" + block + strings.TrimLeft(trimmed[end:], "\n") + "\n"
}
