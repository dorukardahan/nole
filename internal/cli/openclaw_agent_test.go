package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

func TestDefaultServiceForwardsExplicitOpenClawAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based OpenClaw CLI fixture")
	}
	clearProviderPolicyEnv(t)
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("NOLE_CLIENT", "openclaw")
	t.Setenv("NOLE_OPENCLAW_AGENT_ID", " documentation ")
	dir := t.TempDir()
	capture := filepath.Join(dir, "params.json")
	script := filepath.Join(dir, "openclaw")
	body := "#!/bin/sh\nprintf '%s' \"$5\" > " + shellQuote(capture) + "\nprintf '%s' '{\"ok\":true,\"toolName\":\"web_fetch\",\"output\":{\"details\":{\"url\":\"http://93.184.216.34/\",\"status\":200,\"text\":\"Public documentation\",\"extractor\":\"readability\"}}}'\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOLE_OPENCLAW_CLI", script)
	response, err := defaultService().Extract(context.Background(), core.ExtractRequest{URL: extractTestURL})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "firecrawl" {
		t.Fatalf("provider = %q", response.Provider)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if params["agentId"] != "documentation" {
		t.Fatalf("agentId = %v", params["agentId"])
	}
}
