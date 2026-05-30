package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// launchSpec describes how a client should be told to invoke Nólë's MCP server.
// When wrapper is empty the client launches the bare binary with the "mcp"
// subcommand. When wrapper is set the client launches the wrapper directly with
// no extra args; the wrapper is responsible for sourcing ~/.config/nole/.env and
// exec'ing the binary. The wrapper template lives in docs/PROVIDER-KEYS.md.
type launchSpec struct {
	Binary  string
	Wrapper string
}

func (l launchSpec) command() string {
	if l.Wrapper != "" {
		return l.Wrapper
	}
	return l.Binary
}

func (l launchSpec) args() []string {
	if l.Wrapper != "" {
		return []string{}
	}
	return []string{"mcp"}
}

func newSetupCommand() *cobra.Command {
	var all bool
	var claude bool
	var cursor bool
	var codex bool
	var opencode bool
	var windsurf bool
	var kimi bool
	var hermes bool
	var localExtract bool
	var localExtractVenv string
	var localExtractPython string
	var wrapper string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure AI agents to use nole as MCP server",
		Long: "Writes MCP server configuration files for supported AI coding agents.\n" +
			"Supports: --claude, --cursor, --codex, --opencode, --kimi, --windsurf, --hermes, or --all.\n" +
			"Use --local-extract to install an isolated Scrapling runtime and write NOLE_SCRAPLING_PYTHON.\n" +
			"Use --mcp-wrapper /absolute/path/to/nole-mcp to register an env-sourcing wrapper instead of the bare binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				claude, cursor, codex, opencode, kimi, windsurf, hermes = true, true, true, true, true, true, true
			}
			if !claude && !cursor && !codex && !opencode && !kimi && !windsurf && !hermes && !localExtract {
				return fmt.Errorf("specify at least one agent or --local-extract: --claude, --cursor, --codex, --opencode, --kimi, --windsurf, --hermes, --local-extract, or --all")
			}

			binary, err := os.Executable()
			if err != nil {
				binary = "nole"
			}
			absBinary, err := filepath.Abs(binary)
			if err != nil {
				absBinary = binary
			}

			spec := launchSpec{Binary: absBinary, Wrapper: strings.TrimSpace(wrapper)}
			if spec.Wrapper != "" && !filepath.IsAbs(spec.Wrapper) {
				return fmt.Errorf("--mcp-wrapper must be an absolute path, got %q", spec.Wrapper)
			}

			out := cmd.OutOrStdout()
			errOut := cmd.OutOrStderr()
			configured := 0

			if localExtract {
				fmt.Fprintln(out, "local-extract: preparing isolated Scrapling runtime (first run may take a few minutes)")
				result, err := setupLocalExtract(localExtractOptions{
					VenvPath: localExtractVenv,
					Python:   localExtractPython,
				})
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "local-extract: configured Scrapling runtime at %s\n", result.PythonPath)
				fmt.Fprintf(out, "local-extract: wrote %s\n", result.EnvPath)
				if spec.Wrapper == "" {
					wrapperPath, err := defaultMCPWrapperPath()
					if err != nil {
						return err
					}
					spec.Wrapper = wrapperPath
				}
				if err := writeMCPWrapper(spec.Wrapper, spec.Binary); err != nil {
					return err
				}
				fmt.Fprintf(out, "local-extract: wrote env-sourcing MCP wrapper at %s\n", spec.Wrapper)
			}

			if claude {
				// Claude is intentionally instruction-only — the writer does
				// not modify any file. Print the command but do not count it
				// as a configured client in the summary line, so users do not
				// mistake the printout for a finished setup.
				if err := printClaudeInstructions(out, spec); err != nil {
					fmt.Fprintf(errOut, "claude: %v\n", err)
				}
			}
			if cursor {
				if err := writeCursorConfig(spec); err != nil {
					fmt.Fprintf(errOut, "cursor: %v\n", err)
				} else {
					fmt.Fprintln(out, "cursor: configured")
					configured++
				}
			}
			if codex {
				if err := writeCodexConfig(spec); err != nil {
					fmt.Fprintf(errOut, "codex: %v\n", err)
				} else {
					fmt.Fprintln(out, "codex: configured")
					configured++
				}
			}
			if opencode {
				if err := writeOpenCodeConfig(spec); err != nil {
					fmt.Fprintf(errOut, "opencode: %v\n", err)
				} else {
					fmt.Fprintln(out, "opencode: configured")
					configured++
				}
			}
			if kimi {
				if err := writeKimiConfig(spec); err != nil {
					fmt.Fprintf(errOut, "kimi: %v\n", err)
				} else {
					fmt.Fprintln(out, "kimi: configured")
					configured++
				}
			}
			if windsurf {
				if err := writeWindsurfConfig(spec); err != nil {
					fmt.Fprintf(errOut, "windsurf: %v\n", err)
				} else {
					fmt.Fprintln(out, "windsurf: configured")
					configured++
				}
			}
			if hermes {
				if err := writeHermesConfig(spec); err != nil {
					fmt.Fprintf(errOut, "hermes: %v\n", err)
				} else {
					fmt.Fprintln(out, "hermes: configured")
					configured++
				}
			}

			fmt.Fprintf(out, "\n%d agent(s) configured. Set provider keys before starting:\n", configured)
			fmt.Fprintln(out, "  export FIRECRAWL_API_KEY=...")
			fmt.Fprintln(out, "  export BRAVE_API_KEY=... or BRAVE_SEARCH_API_KEY=...")
			fmt.Fprintln(out, "  export TAVILY_API_KEY=...")
			fmt.Fprintln(out, "  Or store them in ~/.config/nole/.env; Nólë commands load it, and Codex setup plus the nole-mcp wrapper source it for MCP clients without putting secrets in client configs.")
			if !localExtract {
				fmt.Fprintln(out, "  Optional local extraction fallback:")
				fmt.Fprintln(out, "    nole setup --local-extract")
			}
			if spec.Wrapper == "" {
				fmt.Fprintln(out, "  For non-Codex clients that do not inherit shell env, point them at an env-sourcing wrapper:")
				fmt.Fprintln(out, "    nole setup --opencode --mcp-wrapper /absolute/path/to/nole-mcp")
				fmt.Fprintln(out, "  Wrapper template: docs/PROVIDER-KEYS.md")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "configure all supported agents")
	cmd.Flags().BoolVar(&claude, "claude", false, "print Claude Code setup instructions")
	cmd.Flags().BoolVar(&cursor, "cursor", false, "configure Cursor")
	cmd.Flags().BoolVar(&codex, "codex", false, "configure Codex CLI")
	cmd.Flags().BoolVar(&opencode, "opencode", false, "configure OpenCode")
	cmd.Flags().BoolVar(&kimi, "kimi", false, "configure Kimi CLI")
	cmd.Flags().BoolVar(&windsurf, "windsurf", false, "configure Windsurf")
	cmd.Flags().BoolVar(&hermes, "hermes", false, "configure Hermes Agent")
	cmd.Flags().BoolVar(&localExtract, "local-extract", false, "install an isolated local Scrapling extract runtime and write NOLE_SCRAPLING_PYTHON")
	cmd.Flags().StringVar(&localExtractVenv, "local-extract-venv", "", "absolute path for the local extract Python virtual environment (default: ~/.local/share/nole/scrapling-venv)")
	cmd.Flags().StringVar(&localExtractPython, "python", "", "Python 3.10+ executable to use for creating the local extract virtual environment (default: auto-detect python3/python)")
	cmd.Flags().StringVar(&wrapper, "mcp-wrapper", "", "absolute path to an env-sourcing MCP wrapper (e.g. ~/.local/bin/nole-mcp). Applies to non-Codex writers and changes the Codex launch line to call the wrapper directly.")
	return cmd
}

// printClaudeInstructions does not write any file. Claude Code's installed
// release manages user-scope MCP servers through `claude mcp add` and reads
// from its own evolving config schema; writing to a stale path on its behalf
// would silently mislead users. Instead we print the exact command to run.
func printClaudeInstructions(out io.Writer, spec launchSpec) error {
	target := spec.command()
	fmt.Fprintln(out, "claude: instructions (no file written)")
	fmt.Fprintln(out, "  Claude Code manages user-scope MCP servers via its CLI.")
	fmt.Fprintln(out, "  Run the following to register Nólë:")
	if len(spec.args()) == 0 {
		fmt.Fprintf(out, "    claude mcp add nole -s user -- %s\n", target)
	} else {
		fmt.Fprintf(out, "    claude mcp add nole -s user -- %s %s\n", target, strings.Join(spec.args(), " "))
	}
	fmt.Fprintln(out, "  Then verify:")
	fmt.Fprintln(out, "    claude mcp list")
	fmt.Fprintln(out, "    claude mcp get nole")
	if spec.Wrapper == "" {
		fmt.Fprintln(out, "  If Claude Code does not inherit your shell env, prefer the wrapper form:")
		fmt.Fprintln(out, "    nole setup --claude --mcp-wrapper /absolute/path/to/nole-mcp")
	}
	return nil
}

// mcpConfig is the common MCP server config format used by Cursor and Windsurf.
type mcpConfig struct {
	McpServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func writeCursorConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".cursor", "mcp.json")
	return writeMCPJSONConfig(path, spec)
}

func writeHermesConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".hermes", "config.yaml")
	return writeHermesConfigPath(path, spec)
}

func writeHermesConfigPath(path string, spec launchSpec) error {
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
	doc, root, err := parseHermesConfigYAML(existing)
	if err != nil {
		return err
	}
	if err := upsertHermesNoleServer(root, spec); err != nil {
		return err
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal hermes config yaml: %w", err)
	}
	return atomicWriteFile(path, out, configWriteMode(exists, mode))
}

func parseHermesConfigYAML(existing []byte) (*yaml.Node, *yaml.Node, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	if len(strings.TrimSpace(string(existing))) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{root}
		return doc, root, nil
	}
	if err := yaml.Unmarshal(existing, doc); err != nil {
		return nil, nil, fmt.Errorf("parse existing hermes config yaml: %w", err)
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, nil, fmt.Errorf("parse existing hermes config yaml: root document is invalid")
	}
	if len(doc.Content) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = []*yaml.Node{root}
		return doc, root, nil
	}
	root := doc.Content[0]
	if isYAMLNullNode(root) {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content[0] = root
		return doc, root, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse existing hermes config yaml: root must be a mapping")
	}
	return doc, root, nil
}

func isYAMLNullNode(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

func upsertHermesNoleServer(root *yaml.Node, spec launchSpec) error {
	servers, ok := yamlMappingLookup(root, "mcp_servers")
	if !ok {
		servers = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlMappingUpsert(root, "mcp_servers", servers)
	}
	if servers.Kind != yaml.MappingNode {
		return fmt.Errorf("existing hermes config mcp_servers must be a mapping")
	}

	nole, ok := yamlMappingLookup(servers, "nole")
	if !ok {
		nole = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlMappingUpsert(servers, "nole", nole)
	}
	if nole.Kind != yaml.MappingNode {
		return fmt.Errorf("existing hermes config mcp_servers.nole must be a mapping")
	}

	yamlMappingUpsert(nole, "command", yamlStringNode(spec.command()))
	yamlMappingUpsert(nole, "args", yamlStringSequenceNode(spec.args()))
	yamlMappingUpsertIfMissing(nole, "timeout", yamlIntNode(120))
	yamlMappingUpsertIfMissing(nole, "connect_timeout", yamlIntNode(60))
	if err := ensureHermesNoleToolPolicy(nole); err != nil {
		return err
	}
	return nil
}

func ensureHermesNoleToolPolicy(nole *yaml.Node) error {
	tools, ok := yamlMappingLookup(nole, "tools")
	if !ok {
		tools = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		yamlMappingUpsert(nole, "tools", tools)
	}
	if tools.Kind != yaml.MappingNode {
		return fmt.Errorf("existing hermes config mcp_servers.nole.tools must be a mapping")
	}
	yamlMappingUpsertIfMissing(tools, "resources", yamlBoolNode(false))
	yamlMappingUpsertIfMissing(tools, "prompts", yamlBoolNode(false))
	return nil
}

func yamlMappingLookup(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], true
		}
	}
	return nil, false
}

func yamlMappingUpsert(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			existing := mapping.Content[i+1]
			value.HeadComment = existing.HeadComment
			value.LineComment = existing.LineComment
			value.FootComment = existing.FootComment
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, yamlStringNode(key), value)
}

func yamlMappingUpsertIfMissing(mapping *yaml.Node, key string, value *yaml.Node) {
	if _, ok := yamlMappingLookup(mapping, key); ok {
		return
	}
	yamlMappingUpsert(mapping, key, value)
}

func yamlStringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlIntNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
}

func yamlBoolNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprintf("%t", value)}
}

func yamlStringSequenceNode(values []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if len(values) == 0 {
		node.Style = yaml.FlowStyle
		return node
	}
	for _, value := range values {
		node.Content = append(node.Content, yamlStringNode(value))
	}
	return node
}

func writeWindsurfConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
	return writeMCPJSONConfig(path, spec)
}

// resolveHomeDir returns os.UserHomeDir() with a more actionable error message
// when neither HOME nor /etc/passwd yields a directory. Falling back silently
// to "" would have writers create relative paths like ".cursor/mcp.json" in the
// current working directory, which is almost never what the user wants.
func resolveHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w (set HOME or run in a context with a known user home)", err)
	}
	if home == "" {
		return "", fmt.Errorf("resolve home dir: empty result (set HOME or run in a context with a known user home)")
	}
	return home, nil
}

func writeMCPJSONConfig(path string, spec launchSpec) error {
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
	nole, err := noleMCPServerRaw(spec)
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

func writeCodexConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".codex", "config.toml")
	return writeCodexConfigPath(path, spec)
}

func writeCodexConfigPath(path string, spec launchSpec) error {
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
	content := upsertCodexTomlTable(string(existing), "mcp_servers.nole", codexMCPServerBlock(spec))
	return atomicWriteFile(path, []byte(content), configWriteMode(exists, mode))
}

func codexMCPServerBlock(spec launchSpec) string {
	if spec.Wrapper != "" {
		// The wrapper sources ~/.config/nole/.env and exec's nole mcp itself,
		// so Codex can call it directly without the inline shell.
		return fmt.Sprintf(
			"# nole MCP server\n[mcp_servers.nole]\ncommand = %q\nargs = []\n",
			spec.Wrapper,
		)
	}
	launch := fmt.Sprintf(
		`set -a; [ -f "$HOME/.config/nole/.env" ] && . "$HOME/.config/nole/.env"; set +a; exec %q mcp`,
		spec.Binary,
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
			willSkip := header == table || strings.HasPrefix(header, table+".")
			if willSkip {
				// Drop any contiguous comment lines (and a single trailing
				// blank line) we just kept above this header. Without this,
				// the marker comment that `codexMCPServerBlock` emits would
				// accumulate one copy per re-run.
				for len(kept) > 0 && strings.HasPrefix(strings.TrimSpace(kept[len(kept)-1]), "#") {
					kept = kept[:len(kept)-1]
				}
				for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
					kept = kept[:len(kept)-1]
				}
			}
			skipping = willSkip
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

// writeOpenCodeConfig writes Nólë's MCP entry to OpenCode's native user-scope
// config path, using the schema the installed OpenCode release reads:
//
//	{
//	  "mcp": {
//	    "nole": {
//	      "type": "local",
//	      "command": ["/absolute/path/to/nole", "mcp"],
//	      "enabled": true,
//	      "environment": {}
//	    }
//	  }
//	}
func writeOpenCodeConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	return writeOpenCodeConfigPath(path, spec)
}

func writeOpenCodeConfigPath(path string, spec launchSpec) error {
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
	nole, err := openCodeEntryRaw(spec)
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

type openCodeEntry struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Enabled     bool              `json:"enabled"`
	Environment map[string]string `json:"environment"`
}

func openCodeEntryRaw(spec launchSpec) (json.RawMessage, error) {
	args := spec.args()
	command := make([]string, 0, 1+len(args))
	command = append(command, spec.command())
	command = append(command, args...)
	entry := openCodeEntry{
		Type:        "local",
		Command:     command,
		Enabled:     true,
		Environment: map[string]string{},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal opencode entry: %w", err)
	}
	return json.RawMessage(b), nil
}

// writeKimiConfig writes Nólë's MCP entry to Kimi's user-scope config path,
// matching the shape `kimi mcp add` produces:
//
//	{
//	  "mcpServers": {
//	    "nole": {
//	      "command": "/absolute/path/to/nole-mcp"
//	    }
//	  }
//	}
//
// When --mcp-wrapper is unset the writer emits {command, args} so the entry
// still launches `nole mcp` directly. Both shapes are valid per Kimi's mcp.json
// reader; the wrapper form matches what the official CLI writes.
func writeKimiConfig(spec launchSpec) error {
	home, err := resolveHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".kimi", "mcp.json")
	return writeKimiConfigPath(path, spec)
}

func writeKimiConfigPath(path string, spec launchSpec) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("parse existing kimi mcpServers: %w", err)
		}
	}
	nole, err := kimiEntryRaw(spec)
	if err != nil {
		return err
	}
	servers["nole"] = nole
	encoded, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal kimi mcpServers: %w", err)
	}
	root["mcpServers"] = encoded
	return writeJSONConfig(path, root)
}

func kimiEntryRaw(spec launchSpec) (json.RawMessage, error) {
	if spec.Wrapper != "" {
		// Match the single-command shape that `kimi mcp add` writes.
		entry := struct {
			Command string `json:"command"`
		}{Command: spec.Wrapper}
		b, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("marshal kimi entry: %w", err)
		}
		return json.RawMessage(b), nil
	}
	entry := mcpServerEntry{Command: spec.Binary, Args: []string{"mcp"}}
	b, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal kimi entry: %w", err)
	}
	return json.RawMessage(b), nil
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

func noleMCPServerEntry(spec launchSpec) mcpServerEntry {
	return mcpServerEntry{Command: spec.command(), Args: spec.args()}
}

func noleMCPServerRaw(spec launchSpec) (json.RawMessage, error) {
	b, err := json.Marshal(noleMCPServerEntry(spec))
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
