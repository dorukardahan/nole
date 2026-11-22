package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/mock"
	"github.com/dorukardahan/nole/internal/providers/scrapling"
)

// Exercise the real Scrapling provider and embedded readiness script. Only the
// optional Python library/browser is replaced; provider status is never stubbed.
func TestBrowserStatusRoutineAndDiagnosticSurfaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("browser mode requires Unix process groups")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required")
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "scrapling")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "__init__.py"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	module := `import os
from types import SimpleNamespace
class Fetcher: pass
class StealthyFetcher: pass
class CDP:
    def on(self, event, callback): pass
    def send(self, method, params): pass
class Context:
    def new_page(self): return SimpleNamespace(context=self)
    def new_cdp_session(self, page): return CDP()
    def route(self, pattern, callback): pass
    def route_web_socket(self, pattern, callback): pass
class StealthySession:
    def __init__(self, **options): self.context=Context()
    def __enter__(self):
        with open(os.environ['NOLE_TEST_READINESS_CALLS'],'a') as f: f.write('launch\n')
        return self
    def __exit__(self, *args): pass
`
	if err := os.WriteFile(filepath.Join(pkg, "fetchers.py"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "calls")
	t.Setenv("PYTHONPATH", dir)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("NOLE_TEST_READINESS_CALLS", calls)
	t.Setenv("NOLE_SCRAPLING_PYTHON", python)
	t.Setenv("NOLE_SCRAPLING_MODE", "auto")
	t.Setenv("NOLE_SCRAPLING_PROXY", "")
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	for _, key := range []string{"BRAVE_API_KEY", "BRAVE_SEARCH_API_KEY", "FIRECRAWL_API_KEY", "TAVILY_API_KEY", "TINYFISH_API_KEY"} {
		t.Setenv(key, "")
	}
	registry := core.NewRegistry()
	ledger := core.NewMemoryQuotaLedger()
	for _, p := range []core.Provider{scrapling.New(), mock.New("mock")} {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
		ledger.Set(core.QuotaEntry{Provider: p.Name(), CostClass: core.CostClassKeylessFree, KeylessFree: true})
	}
	svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskGeneral: {"mock"}, core.TaskExtract: {"scrapling"}})
	h := &httpHandler{svc: svc, mcp: buildMCPServer(svc)}
	mux := h.buildMux()
	cliCall := func(args ...string) {
		cmd := NewRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, r.Code)
		}
	}
	cases := []struct {
		name string
		deep bool
		run  func()
	}{
		{"routine status", false, func() {
			for _, status := range svc.ProviderStatus(context.Background()).Providers {
				if status.Name == "scrapling" && !strings.Contains(status.Reason, "mode: auto") {
					t.Fatal("routine status mislabeled configured auto mode")
				}
			}
		}},
		{"health", false, func() { get("/health") }},
		{"search tip", false, func() {
			r := postMCP(t, h, "", mcpSearchBody(t, "fixture"))
			if decodeSetupTip(t, r.Body.Bytes()) == nil {
				t.Fatal("setup tip not exercised")
			}
		}},
		{"config dump", false, func() { cliCall("config", "dump", "--json") }},
		{"doctor", true, func() { cliCall("doctor") }},
		{"doctor json", true, func() { cliCall("doctor", "--json") }},
		{"providers", true, func() { cliCall("providers", "--json") }},
		{"api providers", true, func() { get("/api/providers") }},
		{"mcp provider status", true, func() {
			r := postMCP(t, h, "", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"provider_status","arguments":{}}}`))
			if !strings.Contains(r.Body.String(), "browser ready") {
				t.Fatal("MCP browser readiness missing")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(calls, nil, 0600); err != nil {
				t.Fatal(err)
			}
			tc.run()
			got, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.deep {
				want = "launch\n"
			}
			if string(got) != want {
				t.Fatalf("browser launches=%q want=%q", got, want)
			}
		})
	}
}
