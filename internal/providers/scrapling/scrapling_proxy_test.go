package scrapling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

const testProxy = "http://fixture-user:fixture-password@127.0.0.1:8080"

// Reuse the prerequisite's offline Python package, executing the real script.
func proxyFixture(t *testing.T) Provider {
	t.Helper()
	p := syntheticScrapling(t, map[string]any{})
	module := `import json, os, sys
from types import SimpleNamespace
class Page(SimpleNamespace):
    def css(self, selector):
        return []
class Fetcher:
    @staticmethod
    def get(url, **kwargs):
        record = {'kwargs': kwargs, 'env': {k: v for k, v in os.environ.items() if k.lower() in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy')}, 'argv': sys.argv}
        with open(os.environ['NOLE_TEST_FETCH_COUNT'], 'a') as f:
            f.write(json.dumps(record) + '\n')
        mode = os.environ.get('NOLE_TEST_PROXY_MODE', '')
        if mode == 'exception':
            raise RuntimeError(os.environ['NOLE_SCRAPLING_PROXY'])
        if mode == 'unsupported':
            raise TypeError(os.environ['NOLE_SCRAPLING_PROXY'])
        if mode == 'malformed_redirect':
            return Page(status=302, headers={'Location': os.environ['NOLE_SCRAPLING_PROXY'] + '/%zz'})
        if mode == 'malformed_final':
            return Page(status=200, text='fixture', url=os.environ['NOLE_SCRAPLING_PROXY'] + '/%zz')
        if mode == 'redirect':
            return Page(status=302, headers={'Location': 'http://10.0.0.1/private'})
        return Page(status=407 if mode == 'auth' else 200, text='Useful fixture page.', url=url, meta={'proxy': kwargs.get('proxy')})
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PYTHONPATH"), "scrapling", "fetchers.py"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

type proxyCall struct {
	Kwargs map[string]any    `json:"kwargs"`
	Env    map[string]string `json:"env"`
	Argv   []string          `json:"argv"`
}

func proxyCalls(t *testing.T) []proxyCall {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("NOLE_TEST_FETCH_COUNT"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []proxyCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call proxyCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func assertProxyPrivate(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{testProxy, "fixture-user", "fixture-password", "127.0.0.1:8080"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("public output contains proxy configuration")
		}
	}
}

func TestScraplingProxyPropagationAndPrecedence(t *testing.T) {
	for _, scheme := range []string{"http", "https", "socks4", "socks4a", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			proxy := scheme + "://fixture-user:fixture-password@127.0.0.1:8080"
			t.Setenv("NOLE_SCRAPLING_PROXY", proxy)
			for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
				t.Setenv(name, "ambient-fixture")
			}
			p := proxyFixture(t)
			resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err != nil {
				t.Fatal(err)
			}
			calls := proxyCalls(t)
			if len(calls) != 1 {
				t.Fatalf("got %d fetches", len(calls))
			}
			if calls[0].Kwargs["proxy"] != proxy || calls[0].Kwargs["follow_redirects"] != false {
				t.Fatal("proxy/redirect arguments were not preserved")
			}
			if len(calls[0].Env) != 0 {
				t.Fatal("explicit proxy inherited ambient proxy/bypass variables")
			}
			assertProxyPrivate(t, calls[0].Argv)
			assertProxyPrivate(t, resp)
			if os.Getenv("NO_PROXY") != "ambient-fixture" {
				t.Fatal("parent environment changed")
			}
		})
	}
}

func TestScraplingProxyAbsentPreservesEnvironment(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_PROXY", "")
	t.Setenv("http_proxy", "http://ambient.invalid:8000")
	t.Setenv("NO_PROXY", "*")
	p := proxyFixture(t)
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA}); err != nil {
		t.Fatal(err)
	}
	calls := proxyCalls(t)
	if len(calls) != 1 {
		t.Fatal("expected one fetch")
	}
	if _, ok := calls[0].Kwargs["proxy"]; ok {
		t.Fatal("absent setting must omit proxy argument")
	}
	if calls[0].Env["http_proxy"] != "http://ambient.invalid:8000" || calls[0].Env["NO_PROXY"] != "*" {
		t.Fatal("absent setting changed inherited behavior")
	}
	if !strings.Contains(p.Status(context.Background()).Reason, "proxy: not configured") {
		t.Fatal("missing proxy absence status")
	}
}

func TestScraplingProxyInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"fixture-password", "ftp://fixture-user:fixture-password@127.0.0.1:8080", "http://", "http://127.0.0.1:bad", "http://127.0.0.1:65536", "http://127.0.0.1:0", "http://127.0.0.1:8080/path", "http://127.0.0.1:8080?fixture-password", "http://127.0.0.1:8080#fixture-password"} {
		t.Run("invalid", func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_PROXY", value)
			p := proxyFixture(t)
			_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err == nil || !strings.Contains(err.Error(), "invalid NOLE_SCRAPLING_PROXY") {
				t.Fatal("expected fixed proxy configuration failure")
			}
			assertProxyPrivate(t, err.Error())
			status := p.Status(context.Background())
			if status.Available {
				t.Fatal("invalid proxy reported available")
			}
			assertProxyPrivate(t, status)
			if len(proxyCalls(t)) != 0 {
				t.Fatal("invalid proxy triggered fetch")
			}
		})
	}
}

func TestScraplingProxyFailuresStayPrivateAndDoNotRetry(t *testing.T) {
	for _, mode := range []string{"exception", "unsupported", "auth", "redirect", "malformed_redirect", "malformed_final"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_PROXY", testProxy)
			t.Setenv("NOLE_TEST_PROXY_MODE", mode)
			p := proxyFixture(t)
			wrapped := &countedBlockProvider{Provider: p, name: "scrapling"}
			resp, err := blockTestService(t, wrapped).Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err == nil {
				t.Fatal("expected failure")
			}
			assertProxyPrivate(t, err.Error())
			assertProxyPrivate(t, resp)
			calls := proxyCalls(t)
			if len(calls) != 1 || calls[0].Kwargs["proxy"] != testProxy {
				t.Fatal("failure dropped proxy or retried")
			}
			if mode == "auth" && !strings.Contains(err.Error(), "HTTP 407") {
				t.Fatal("lost HTTP classification")
			}
			if mode == "redirect" && !strings.Contains(err.Error(), "blocked URL") {
				t.Fatal("private redirect not blocked")
			}
		})
	}
}

func TestScraplingProxyRuntimeDiagnostics(t *testing.T) {
	for _, mode := range []string{"stderr", "stdout", "decode"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_PROXY", testProxy)
			script := "#!/bin/sh\nprintf '%s' \"$NOLE_SCRAPLING_PROXY\"\n"
			if mode == "stderr" {
				script = "#!/bin/sh\nprintf '%s' \"$NOLE_SCRAPLING_PROXY\" >&2\nexit 1\n"
			}
			if mode == "decode" {
				script = "#!/bin/sh\nprintf '{\"content\":{},\"%s\":true}' \"$NOLE_SCRAPLING_PROXY\"\n"
			}
			p := New(WithPython(writeFakePython(t, script)))
			status := p.Status(context.Background())
			assertProxyPrivate(t, status)
			if !strings.Contains(status.Reason, "proxy: configured") {
				t.Fatal("proxy presence not reported")
			}
			_, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err == nil {
				t.Fatal("expected runtime/decode failure")
			}
			assertProxyPrivate(t, err.Error())
			// The real status method's failure reason can become a skipped route reason.
			registry := core.NewRegistry()
			if err := registry.Register(p); err != nil {
				t.Fatal(err)
			}
			ledger := core.NewMemoryQuotaLedger()
			ledger.Set(core.QuotaEntry{Provider: "scrapling", CostClass: core.CostClassKeylessFree, KeylessFree: true})
			service := core.NewService(registry, ledger, core.RouteMatrix{core.TaskExtract: []string{"scrapling"}})
			resp, err := service.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err == nil {
				t.Fatal("expected service failure")
			}
			assertProxyPrivate(t, err.Error())
			assertProxyPrivate(t, resp)
		})
	}
}

func TestScraplingProxyDoesNotPermitPrivateTarget(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_PROXY", testProxy)
	p := &countedBlockProvider{Provider: proxyFixture(t), name: "scrapling"}
	if _, err := blockTestService(t, p).Extract(context.Background(), core.ExtractRequest{URL: private}); err == nil {
		t.Fatal("private target accepted")
	}
	if p.calls != 0 || len(proxyCalls(t)) != 0 {
		t.Fatal("private target fetched")
	}
}
