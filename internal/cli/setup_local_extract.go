package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const scraplingInstallSpec = "scrapling[fetchers]"

type localExtractOptions struct {
	VenvPath string
	Python   string
}

type localExtractResult struct {
	VenvPath   string
	PythonPath string
	EnvPath    string
}

func setupLocalExtract(opts localExtractOptions) (localExtractResult, error) {
	venvPath, err := resolveLocalExtractVenvPath(opts.VenvPath)
	if err != nil {
		return localExtractResult{}, err
	}
	python, err := resolveSetupPython(opts.Python)
	if err != nil {
		return localExtractResult{}, err
	}
	if err := ensurePythonVersion(python); err != nil {
		return localExtractResult{}, err
	}

	venvPython := venvPythonPath(venvPath)
	if _, err := os.Stat(venvPython); err != nil {
		if !os.IsNotExist(err) {
			return localExtractResult{}, fmt.Errorf("stat local extract python: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(venvPath), 0755); err != nil {
			return localExtractResult{}, fmt.Errorf("create local extract parent dir: %w", err)
		}
		if err := runSetupCommand(python, "-m", "venv", venvPath); err != nil {
			return localExtractResult{}, fmt.Errorf("create local extract venv: %w", err)
		}
	}

	if err := verifyScraplingFetcher(venvPython); err != nil {
		if err := runSetupCommand(venvPython, "-m", "pip", "install", "--disable-pip-version-check", scraplingInstallSpec); err != nil {
			return localExtractResult{}, fmt.Errorf("install %s: %w", scraplingInstallSpec, err)
		}
		if err := verifyScraplingFetcher(venvPython); err != nil {
			return localExtractResult{}, fmt.Errorf("verify local extract runtime: %w", err)
		}
	}

	envPath, err := defaultNoleEnvPath()
	if err != nil {
		return localExtractResult{}, err
	}
	if err := writeNoleEnvValue(envPath, "NOLE_SCRAPLING_PYTHON", venvPython); err != nil {
		return localExtractResult{}, err
	}
	return localExtractResult{VenvPath: venvPath, PythonPath: venvPython, EnvPath: envPath}, nil
}

func resolveLocalExtractVenvPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		home, err := resolveHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".local", "share", "nole", "scrapling-venv")
	} else {
		var err error
		path, err = expandHomePath(path)
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("--local-extract-venv must be an absolute path, got %q", raw)
	}
	return filepath.Clean(path), nil
}

func resolveSetupPython(raw string) (string, error) {
	if python := strings.TrimSpace(raw); python != "" {
		return python, nil
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Python 3.10+ not found; install Python or pass --python /absolute/path/to/python3")
}

func ensurePythonVersion(python string) error {
	if err := runSetupCommand(python, "-c", `import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)`); err != nil {
		return fmt.Errorf("%s is not Python 3.10+: %w", python, err)
	}
	return nil
}

func verifyScraplingFetcher(python string) error {
	return runSetupCommand(python, "-c", `import scrapling; from scrapling.fetchers import Fetcher; print(getattr(scrapling, "__version__", "unknown"))`)
}

func runSetupCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%v: %s", err, msg)
		}
		return err
	}
	return nil
}

func venvPythonPath(venvPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "python.exe")
	}
	return filepath.Join(venvPath, "bin", "python")
}

func defaultNoleEnvPath() (string, error) {
	home, err := resolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "nole", ".env"), nil
}

func defaultMCPWrapperPath() (string, error) {
	home, err := resolveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "nole-mcp"), nil
}

func writeNoleEnvValue(path, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("env key is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	existing, exists, mode, err := readExistingFileWithMode(path)
	if err != nil {
		return err
	}
	content := upsertShellEnvAssignment(string(existing), key, value)
	return atomicWriteFile(path, []byte(content), configWriteMode(exists, mode))
}

func upsertShellEnvAssignment(existing, key, value string) string {
	line := key + "=" + shellQuote(value)
	lines := strings.Split(existing, "\n")
	replaced := false
	for i, current := range lines {
		trimmed := strings.TrimSpace(current)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, "export "+key+"=") {
			lines[i] = line
			replaced = true
		}
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if !replaced {
		if strings.TrimSpace(out) != "" {
			out += "\n"
		}
		out += line
	}
	return out + "\n"
}

func writeMCPWrapper(path, binary string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("wrapper path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("wrapper path must be absolute, got %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create wrapper dir: %w", err)
	}
	content := fmt.Sprintf(`#!/bin/sh
set -a
if [ -f "$HOME/.config/nole/.env" ]; then
  . "$HOME/.config/nole/.env"
fi
set +a

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
`, shellQuote(binary))
	return atomicWriteFile(path, []byte(content), 0700)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func expandHomePath(path string) (string, error) {
	if path == "~" {
		return resolveHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := resolveHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
