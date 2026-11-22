package scrapling

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/core"
)

const testProxy = "http://fixture-user:fixture-password@127.0.0.1:8080"
const ambientProxyFixture = "ambient-fixture"

func seedAmbientProxy(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(name, ambientProxyFixture)
		t.Setenv(strings.ToLower(name), ambientProxyFixture)
	}
}

// Reuse the prerequisite's offline Python package, executing the real script.
func proxyFixture(t *testing.T, expectedProxy string) Provider {
	t.Helper()
	p := syntheticScrapling(t, map[string]any{})
	t.Setenv("NOLE_TEST_PROXY_SHA256", fmt.Sprintf("%x", sha256.Sum256([]byte(expectedProxy))))
	module := `import hashlib, json, os, sys
from types import SimpleNamespace

import_record = os.environ.get('NOLE_TEST_IMPORT_RECORD')
if import_record:
    watched = {'nole_scrapling_proxy', 'firecrawl_api_key', 'brave_api_key',
               'brave_search_api_key', 'tavily_api_key', 'tinyfish_api_key'}
    names = sorted({key.lower() for key in os.environ if key.lower() in watched})
    with open(import_record, 'a') as f:
        f.write(json.dumps(names) + '\n')

class Page(SimpleNamespace):
    def css(self, selector):
        return []
class Fetcher:
    @staticmethod
    def get(url, **kwargs):
        proxy = kwargs.get('proxy')
        env = {}
        for key, value in os.environ.items():
            name = key.lower()
            if name in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy'):
                env[name] = env.get(name, True) and value == 'ambient-fixture'
        record = {
            'proxy_present': 'proxy' in kwargs,
            'proxy_matches': proxy is not None and hashlib.sha256(proxy.encode()).hexdigest() == os.environ['NOLE_TEST_PROXY_SHA256'],
            'proxy_in_argv': bool(proxy and any(proxy in arg for arg in sys.argv)),
            'redirects_disabled': kwargs.get('follow_redirects') is False,
            'env': env,
        }
        with open(os.environ['NOLE_TEST_FETCH_COUNT'], 'a') as f:
            f.write(json.dumps(record) + '\n')
        mode = os.environ.get('NOLE_TEST_PROXY_MODE', '')
        if mode == 'exception':
            raise RuntimeError(kwargs['proxy'])
        if mode == 'unsupported':
            raise TypeError(kwargs['proxy'])
        if mode == 'malformed_redirect':
            return Page(status=302, headers={'Location': kwargs['proxy'] + '/%zz'})
        if mode == 'malformed_final':
            return Page(status=200, text='fixture', url=kwargs['proxy'] + '/%zz')
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
	ProxyPresent      bool            `json:"proxy_present"`
	ProxyMatches      bool            `json:"proxy_matches"`
	ProxyInArgv       bool            `json:"proxy_in_argv"`
	RedirectsDisabled bool            `json:"redirects_disabled"`
	Env               map[string]bool `json:"env"`
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

func TestScraplingProxyFixtureRecordsNoValues(t *testing.T) {
	for _, proxy := range []string{"", testProxy} {
		t.Setenv("NOLE_SCRAPLING_PROXY", proxy)
		seedAmbientProxy(t)
		p := proxyFixture(t, proxy)
		if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(os.Getenv("NOLE_TEST_FETCH_COUNT"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), ambientProxyFixture) ||
			strings.Contains(string(data), "fixture-user") || strings.Contains(string(data), "fixture-password") {
			t.Fatal("fixture wrote a proxy value")
		}
	}
}

func TestScraplingChildEnvironmentsExcludeProviderCredentials(t *testing.T) {
	for _, tt := range []struct{ name, proxy string }{{"proxy configured", testProxy}, {"proxy absent", ""}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("nole_scrapling_proxy", testProxy)
			t.Setenv("NOLE_SCRAPLING_PROXY", tt.proxy)
			for _, name := range []string{"FIRECRAWL_API_KEY", "brave_api_key", "BRAVE_SEARCH_API_KEY", "tavily_api_key", "TinyFish_Api_Key"} {
				t.Setenv(name, "synthetic-provider-key")
			}
			p := proxyFixture(t, tt.proxy)
			for _, operation := range []string{"extract", "status"} {
				t.Run(operation, func(t *testing.T) {
					record := filepath.Join(t.TempDir(), "import-env")
					t.Setenv("NOLE_TEST_IMPORT_RECORD", record)
					if operation == "extract" {
						if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA}); err != nil {
							t.Fatal(err)
						}
					} else if !p.Status(context.Background()).Available {
						t.Fatal("expected successful status check")
					}
					data, err := os.ReadFile(record)
					if err != nil {
						t.Fatal(err)
					}
					var names []string
					if err := json.Unmarshal(data, &names); err != nil {
						t.Fatal(err)
					}
					if len(names) != 0 {
						t.Errorf("child inherited filtered variable names: %v", names)
					}
				})
			}
		})
	}
}

func TestScraplingProxyPropagationAndPrecedence(t *testing.T) {
	for _, scheme := range []string{"http", "https", "socks4", "socks4a", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			proxy := scheme + "://fixture-user:fixture-password@127.0.0.1:8080"
			t.Setenv("NOLE_SCRAPLING_PROXY", proxy)
			seedAmbientProxy(t)
			p := proxyFixture(t, proxy)
			resp, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err != nil {
				t.Fatal(err)
			}
			calls := proxyCalls(t)
			if len(calls) != 1 {
				t.Fatalf("got %d fetches", len(calls))
			}
			if !calls[0].ProxyMatches || !calls[0].RedirectsDisabled {
				t.Fatal("proxy/redirect arguments were not preserved")
			}
			if len(calls[0].Env) != 0 {
				t.Fatal("explicit proxy inherited ambient proxy/bypass variables")
			}
			if calls[0].ProxyInArgv {
				t.Fatal("proxy was present in child arguments")
			}
			assertProxyPrivate(t, resp)
			if os.Getenv("NO_PROXY") != ambientProxyFixture {
				t.Fatal("parent environment changed")
			}
		})
	}
}

func TestScraplingProxyAbsentPreservesEnvironment(t *testing.T) {
	t.Setenv("NOLE_SCRAPLING_PROXY", "")
	seedAmbientProxy(t)
	p := proxyFixture(t, "")
	if _, err := p.Extract(context.Background(), core.ExtractRequest{URL: pubA}); err != nil {
		t.Fatal(err)
	}
	calls := proxyCalls(t)
	if len(calls) != 1 {
		t.Fatal("expected one fetch")
	}
	if calls[0].ProxyPresent {
		t.Fatal("absent setting must omit proxy argument")
	}
	if !maps.Equal(calls[0].Env, map[string]bool{"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true}) {
		t.Fatal("absent setting changed inherited behavior")
	}
	// With nothing set, status text must stay exactly as in v1.11.1.
	if reason := p.Status(context.Background()).Reason; reason != "local Python package test" {
		t.Fatalf("unset proxy changed status reason: %q", reason)
	}
}

func TestScraplingStatusVersionIsBounded(t *testing.T) {
	p := proxyFixture(t, "")
	pkg := filepath.Join(os.Getenv("PYTHONPATH"), "scrapling", "__init__.py")
	for _, tc := range []struct{ version, want string }{
		{"0.3.7", "local Python package 0.3.7"},
		{"", "local Python package unknown"},
		{"x" + strings.Repeat("y", 40), "local Python package unknown"},
		{"1.0 http://10.0.0.1/secret", "local Python package unknown"},
	} {
		if err := os.WriteFile(pkg, []byte("__version__ = "+strconv.Quote(tc.version)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := p.Status(context.Background()).Reason; got != tc.want {
			t.Fatalf("version %q: got %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestScraplingProxyInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"fixture-password", "ftp://fixture-user:fixture-password@127.0.0.1:8080", "http://", "http://127.0.0.1:bad", "http://127.0.0.1:65536", "http://127.0.0.1:0", "http://127.0.0.1:8080/path", "http://127.0.0.1:8080?fixture-password", "http://127.0.0.1:8080#fixture-password"} {
		t.Run("invalid", func(t *testing.T) {
			t.Setenv("NOLE_SCRAPLING_PROXY", value)
			p := proxyFixture(t, "")
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
			p := proxyFixture(t, testProxy)
			wrapped := &countedBlockProvider{Provider: p, name: "scrapling"}
			resp, err := blockTestService(t, wrapped).Extract(context.Background(), core.ExtractRequest{URL: pubA})
			if err == nil {
				t.Fatal("expected failure")
			}
			assertProxyPrivate(t, err.Error())
			assertProxyPrivate(t, resp)
			calls := proxyCalls(t)
			if len(calls) != 1 || !calls[0].ProxyMatches {
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
			t.Setenv("NOLE_TEST_DIAGNOSTIC", testProxy)
			script := "#!/bin/sh\nprintf '%s' \"$NOLE_TEST_DIAGNOSTIC\"\n"
			if mode == "stderr" {
				script = "#!/bin/sh\nprintf '%s' \"$NOLE_TEST_DIAGNOSTIC\" >&2\nexit 1\n"
			}
			if mode == "decode" {
				script = "#!/bin/sh\nprintf '{\"content\":{},\"%s\":true}' \"$NOLE_TEST_DIAGNOSTIC\"\n"
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
	p := &countedBlockProvider{Provider: proxyFixture(t, testProxy), name: "scrapling"}
	if _, err := blockTestService(t, p).Extract(context.Background(), core.ExtractRequest{URL: private}); err == nil {
		t.Fatal("private target accepted")
	}
	if p.calls != 0 || len(proxyCalls(t)) != 0 {
		t.Fatal("private target fetched")
	}
}
