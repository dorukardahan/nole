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
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("parse existing mcpServers: %w", err)
		}
	}
	nole, err := noleMCPServerRaw(binary)
	if err != nil {
		return err
	}
	servers["nole"] = nole
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
	existing, exists, mode, err := readExistingFileWithMode(path)
	if err != nil {
		return err
	}
	if exists {
		if err := writeBackup(path, existing, mode); err != nil {
			return err
		}
	}
	content := upsertCodexTomlTable(string(existing), "mcp_servers.nole", codexMCPServerBlock(binary))
	return atomicWriteFile(path, []byte(content), configWriteMode(exists, mode))
}

func codexMCPServerBlock(binary string) string {
	launch := fmt.Sprintf(
		`set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec %q mcp`,
		binary,
	)
	return fmt.Sprintf(
		"# nole MCP server\n[mcp_servers.nole]\ncommand = \"/bin/sh\"\nargs = [\"-lc\", %q]\n",
		launch,
	)
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
	return writeOpenCodeConfigPath(path, binary)
}

func writeOpenCodeConfigPath(path, binary string) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcp"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("parse existing opencode mcp: %w", err)
		}
	}
	nole, err := noleMCPServerRaw(binary)
	if err != nil {
		return err
	}
	servers["nole"] = nole
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
	existing, exists, mode, err := readExistingFileWithMode(path)
	if err != nil {
		return err
	}
	if exists {
		if err := writeBackup(path, existing, mode); err != nil {
			return err
		}
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return atomicWriteFile(path, b, configWriteMode(exists, mode))
}

func noleMCPServerEntry(binary string) mcpServerEntry {
	return mcpServerEntry{Command: binary, Args: []string{"mcp"}}
}

func noleMCPServerRaw(binary string) (json.RawMessage, error) {
	b, err := json.Marshal(noleMCPServerEntry(binary))
	if err != nil {
		return nil, fmt.Errorf("marshal nole MCP server: %w", err)
	}
	return json.RawMessage(b), nil
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
	b, exists, _, err := readExistingFileWithMode(path)
	return b, exists, err
}

func readExistingFileWithMode(path string) ([]byte, bool, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, 0, nil
		}
		return nil, false, 0, fmt.Errorf("stat existing config: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, 0, fmt.Errorf("read existing config: %w", err)
	}
	return b, true, info.Mode().Perm(), nil
}

func writeBackup(path string, content []byte, sourceMode os.FileMode) error {
	if err := atomicWriteFile(path+".bak", content, configWriteMode(true, sourceMode)); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

func configWriteMode(exists bool, mode os.FileMode) os.FileMode {
	if exists {
		if perm := mode.Perm(); perm != 0 {
			return perm
		}
	}
	return 0600
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0600
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temp file: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	return nil
}
