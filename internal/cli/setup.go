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

			fmt.Fprintf(cmd.OutOrStdout(), "\n%d agent(s) configured. Set provider keys before starting:\n", configured)
			fmt.Fprintln(cmd.OutOrStdout(), "  export JINA_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export FIRECRAWL_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export BRAVE_API_KEY=... or BRAVE_SEARCH_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  export TAVILY_API_KEY=...")
			fmt.Fprintln(cmd.OutOrStdout(), "  Or store them in ~/.config/nole/.env; Codex setup sources that file without putting secrets in config.toml.")
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
	config := mcpConfig{McpServers: map[string]mcpServerEntry{}}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		_ = json.Unmarshal(existing, &config)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing config: %w", err)
	}
	if config.McpServers == nil {
		config.McpServers = map[string]mcpServerEntry{}
	}
	config.McpServers["nole"] = mcpServerEntry{
		Command: binary,
		Args:    []string{"mcp"},
	}

	return writeJSONConfig(path, config)
}

func writeCodexConfig(binary string) error {
	// Codex CLI: ~/.codex/config.toml (TOML format)
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex", "config.toml")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing config: %w", err)
	}

	launch := fmt.Sprintf(
		`set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec %q mcp`,
		binary,
	)
	block := fmt.Sprintf(
		"# nole MCP server\n[mcp_servers.nole]\ncommand = \"/bin/sh\"\nargs = [\"-lc\", %q]\n",
		launch,
	)
	content := upsertCodexTomlTable(string(existing), "mcp_servers.nole", block)
	return os.WriteFile(path, []byte(content), 0644)
}

func upsertCodexTomlTable(existing string, table string, block string) string {
	lines := strings.Split(existing, "\n")
	var kept []string
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			header := strings.Trim(trimmed, "[]")
			skipping = header == table || strings.HasPrefix(header, table+".")
		}
		if !skipping {
			kept = append(kept, line)
		}
	}

	preserved := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if preserved != "" {
		preserved += "\n\n"
	}
	return preserved + strings.TrimRight(block, "\n") + "\n"
}

func writeOpenCodeConfig(binary string) error {
	// OpenCode: opencode.json in current directory or home
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "opencode.json")

	type openCodeConfig struct {
		MCP map[string]mcpServerEntry `json:"mcp"`
	}

	config := openCodeConfig{
		MCP: map[string]mcpServerEntry{
			"nole": {
				Command: binary,
				Args:    []string{"mcp"},
			},
		},
	}

	return writeJSONConfig(path, config)
}

func writeJSONConfig(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return os.WriteFile(path, b, 0644)
}
