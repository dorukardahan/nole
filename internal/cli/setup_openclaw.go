package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	openClawLookPath        = exec.LookPath
	runOpenClawSetupOutput  = runOpenClawCommandOutput
	runOpenClawSetupCommand = func(name string, args ...string) error {
		_, err := runOpenClawSetupOutput(name, args...)
		return err
	}
)

const openClawSetupOutputLimit = 2 << 20

type openClawPluginList struct {
	Plugins []struct {
		ID                   string   `json:"id"`
		Origin               string   `json:"origin"`
		WebSearchProviderIDs []string `json:"webSearchProviderIds"`
		WebFetchProviderIDs  []string `json:"webFetchProviderIds"`
	} `json:"plugins"`
}

type openClawPluginInspect struct {
	Plugin struct {
		ID                     string `json:"id"`
		PackageName            string `json:"packageName"`
		TrustedOfficialInstall bool   `json:"trustedOfficialInstall"`
	} `json:"plugin"`
	Install struct {
		Source       string `json:"source"`
		ResolvedName string `json:"resolvedName"`
		Integrity    string `json:"integrity"`
	} `json:"install"`
}

type openClawSetupOptions struct {
	Binary      string
	CLIPath     string
	WrapperPath string
}

type openClawSetupResult struct {
	CLIPath        string
	WrapperPath    string
	SearchProvider string
	BridgeMode     string
}

func setupOpenClaw(options openClawSetupOptions) (openClawSetupResult, error) {
	cliPath, err := resolveOpenClawCLI(options.CLIPath)
	if err != nil {
		return openClawSetupResult{}, err
	}
	wrapperPath := strings.TrimSpace(options.WrapperPath)
	if wrapperPath == "" {
		wrapperPath, err = defaultOpenClawMCPWrapperPath()
		if err != nil {
			return openClawSetupResult{}, err
		}
	}
	if !filepath.IsAbs(wrapperPath) {
		return openClawSetupResult{}, fmt.Errorf("--openclaw-wrapper must be an absolute path, got %q", wrapperPath)
	}
	searchProvider, err := ensureOpenClawFirecrawlPlugin(cliPath)
	if err != nil {
		return openClawSetupResult{}, err
	}
	bridgeMode := "fetch-only"
	if searchProvider != "" {
		bridgeMode = "full"
	}
	if err := writeOpenClawMCPWrapper(wrapperPath, options.Binary, cliPath, bridgeMode); err != nil {
		return openClawSetupResult{}, err
	}
	commands := [][]string{{"config", "set", "tools.web.fetch.provider", "firecrawl"}}
	if searchProvider != "" {
		commands = append([][]string{{"config", "set", "tools.web.search.provider", searchProvider}}, commands...)
	}
	for _, args := range commands {
		if err := runOpenClawSetupCommand(cliPath, args...); err != nil {
			return openClawSetupResult{}, fmt.Errorf("configure OpenClaw Firecrawl bridge: %w", err)
		}
	}
	entry, err := json.Marshal(map[string]any{
		"command": wrapperPath,
		"args":    []string{},
	})
	if err != nil {
		return openClawSetupResult{}, fmt.Errorf("encode OpenClaw MCP entry: %w", err)
	}
	if err := runOpenClawSetupCommand(cliPath, "mcp", "set", "nole", string(entry)); err != nil {
		return openClawSetupResult{}, fmt.Errorf("register Nólë with OpenClaw: %w", err)
	}
	return openClawSetupResult{CLIPath: cliPath, WrapperPath: wrapperPath, SearchProvider: searchProvider, BridgeMode: bridgeMode}, nil
}

func ensureOpenClawFirecrawlPlugin(cliPath string) (string, error) {
	providers, installed, err := inspectOpenClawFirecrawlPlugin(cliPath)
	if err != nil {
		return "", err
	}
	if !installed {
		if err := runOpenClawSetupCommand(cliPath, "plugins", "install", "@openclaw/firecrawl-plugin", "--pin"); err != nil {
			return "", fmt.Errorf("install official OpenClaw Firecrawl plugin: %w", err)
		}
		providers, installed, err = inspectOpenClawFirecrawlPlugin(cliPath)
		if err != nil {
			return "", err
		}
		if !installed {
			return "", fmt.Errorf("official OpenClaw Firecrawl plugin was not discovered after installation")
		}
	}
	if err := runOpenClawSetupCommand(cliPath, "plugins", "enable", "firecrawl"); err != nil {
		return "", fmt.Errorf("enable OpenClaw Firecrawl plugin: %w", err)
	}
	if openClawContainsString(providers, "firecrawl-free") {
		return "firecrawl-free", nil
	}
	if openClawContainsString(providers, "firecrawl") {
		return "", nil
	}
	return "", fmt.Errorf("OpenClaw Firecrawl plugin exposes no supported web search provider")
}

func inspectOpenClawFirecrawlPlugin(cliPath string) ([]string, bool, error) {
	raw, err := runOpenClawSetupOutput(cliPath, "plugins", "list", "--json")
	if err != nil {
		return nil, false, fmt.Errorf("inspect OpenClaw plugins: %w", err)
	}
	var list openClawPluginList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false, fmt.Errorf("decode OpenClaw plugin registry")
	}
	for _, plugin := range list.Plugins {
		if plugin.ID != "firecrawl" {
			continue
		}
		trusted, err := trustedOpenClawFirecrawlPlugin(cliPath, plugin.Origin)
		if err != nil {
			return nil, true, err
		}
		if !trusted {
			return nil, true, fmt.Errorf("refusing to enable non-official OpenClaw plugin with id firecrawl")
		}
		if !openClawContainsString(plugin.WebFetchProviderIDs, "firecrawl") {
			return nil, true, fmt.Errorf("OpenClaw Firecrawl plugin exposes no supported web fetch provider")
		}
		return plugin.WebSearchProviderIDs, true, nil
	}
	return nil, false, nil
}

func trustedOpenClawFirecrawlPlugin(cliPath, origin string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(origin), "bundled") {
		return true, nil
	}
	raw, err := runOpenClawSetupOutput(cliPath, "plugins", "inspect", "firecrawl", "--json")
	if err != nil {
		return false, fmt.Errorf("inspect OpenClaw Firecrawl plugin provenance: %w", err)
	}
	var inspected openClawPluginInspect
	if err := json.Unmarshal(raw, &inspected); err != nil {
		return false, fmt.Errorf("decode OpenClaw Firecrawl plugin provenance")
	}
	return inspected.Plugin.ID == "firecrawl" &&
		inspected.Plugin.PackageName == "@openclaw/firecrawl-plugin" &&
		inspected.Plugin.TrustedOfficialInstall &&
		inspected.Install.Source == "npm" &&
		inspected.Install.ResolvedName == "@openclaw/firecrawl-plugin" &&
		strings.TrimSpace(inspected.Install.Integrity) != "", nil
}

func openClawContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func resolveOpenClawCLI(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = "openclaw"
	}
	path, err := openClawLookPath(candidate)
	if err != nil {
		return "", fmt.Errorf("find OpenClaw CLI %q: %w", candidate, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve OpenClaw CLI path: %w", err)
	}
	return absolute, nil
}

func defaultOpenClawMCPWrapperPath() (string, error) {
	home, err := resolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "nole-mcp-openclaw"), nil
}

func writeOpenClawMCPWrapper(path, binary, openClawCLI, bridgeMode string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("OpenClaw wrapper path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("OpenClaw wrapper path must be absolute, got %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create OpenClaw wrapper dir: %w", err)
	}
	content := fmt.Sprintf(`#!/bin/sh
set -a
if [ -f "$HOME/.config/nole/.env" ]; then
  . "$HOME/.config/nole/.env"
fi
set +a

export NOLE_CLIENT=%s
export NOLE_OPENCLAW_CLI=%s
export NOLE_OPENCLAW_BRIDGE=%s

NOLE_BIN_DEFAULT=%s
if [ -n "${NOLE_BIN:-}" ] && [ -x "$NOLE_BIN" ]; then
  exec "$NOLE_BIN" mcp
fi
if [ -x "$NOLE_BIN_DEFAULT" ]; then
  exec "$NOLE_BIN_DEFAULT" mcp
fi
if command -v nole >/dev/null 2>&1; then
  exec nole mcp
fi

echo "nole binary not found. Install nole to PATH or set NOLE_BIN." >&2
exit 127
`, shellQuote("openclaw"), shellQuote(openClawCLI), shellQuote(bridgeMode), shellQuote(binary))
	return atomicWriteFile(path, []byte(content), 0700)
}

type cappedSetupWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *cappedSetupWriter) Write(p []byte) (int, error) {
	original := len(p)
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		if original > 0 {
			w.truncated = true
		}
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	_, _ = w.buf.Write(p)
	return original, nil
}

func runOpenClawCommandOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := &cappedSetupWriter{limit: openClawSetupOutputLimit}
	stderr := &cappedSetupWriter{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("OpenClaw command timed out")
		}
		return nil, fmt.Errorf("OpenClaw command failed")
	}
	if stdout.truncated {
		return nil, fmt.Errorf("OpenClaw command output exceeded safety limit")
	}
	return append([]byte(nil), stdout.buf.Bytes()...), nil
}
