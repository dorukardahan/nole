//go:build unix

package scrapling

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in local-only evidence. This uses the actual installed Scrapling/Patchright
// and Chromium, but no public websites, Cloudflare challenges, or provider quota.
func TestRealBrowserNetworkAndCleanup(t *testing.T) {
	python := os.Getenv("NOLE_TEST_REAL_BROWSER_PYTHON")
	if python == "" {
		t.Skip("set NOLE_TEST_REAL_BROWSER_PYTHON for local Chromium integration")
	}
	var trapHits, rebindLookups atomic.Int64
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { trapHits.Add(1); w.Write([]byte("private trap")) }))
	defer trap.Close()
	completed := make(chan struct{}, 3)
	probeScript := func(kind string) string {
		return fmt.Sprintf(`const targets=[%q,%q,"http://169.254.169.254/","http://[::1]/","http://rebind.example/"];
async function check(){
 for(const url of targets){try{await fetch(url,{signal:AbortSignal.timeout(1000)})}catch(e){}}
 await new Promise(resolve=>{let ws=new WebSocket(%q);ws.onerror=resolve;ws.onopen=()=>{ws.close();resolve()};setTimeout(resolve,1000)});
 await fetch('/done');
}
`, "http://"+kind+"-trap.example/", trap.URL, strings.Replace(trap.URL, "http://", "ws://", 1))
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "rebind.example" {
			http.Redirect(w, r, "http://rebind.example/next", http.StatusFound)
			return
		}
		switch r.URL.Path {
		case "/worker.js":
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, probeScript("worker")+`check();`)
		case "/shared.js":
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, probeScript("shared")+`onconnect=()=>check();`)
		case "/done":
			completed <- struct{}{}
			fmt.Fprint(w, "done")
		case "/hold.js":
			w.Header().Set("Content-Type", "application/javascript")
			for i := 0; i < 3; i++ {
				select {
				case <-completed:
				case <-time.After(6 * time.Second):
					t.Error("browser probe did not complete")
					return
				}
			}
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body>Rendered public article.<script>new Worker('/worker.js');const sw=new SharedWorker('/shared.js');sw.port.start();`+probeScript("page")+`check();</script><script src="/hold.js"></script></body></html>`)
		}
	}))
	defer site.Close()
	var publicDials, forbiddenDials atomic.Int64
	var lookupMu sync.Mutex
	privateLookups := make(map[string]int)
	d := browserDialer{lookup: func(_ context.Context, host string) ([]net.IP, error) {
		if host == "page-trap.example" || host == "worker-trap.example" || host == "shared-trap.example" {
			lookupMu.Lock()
			privateLookups[host]++
			lookupMu.Unlock()
		}
		if host == "public.example" || host == "rebind.example" && rebindLookups.Add(1) == 1 {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}, dial: func(ctx context.Context, n, address string) (net.Conn, error) {
		if address != "8.8.8.8:80" {
			forbiddenDials.Add(1)
			return nil, fmt.Errorf("unexpected dial")
		}
		publicDials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(site.URL, "http://"))
	}}
	p := New(WithPython(python))
	p.mode = "stealthy"
	p.browserProxy = func(ctx context.Context, _ string) (string, func(), error) { return listenBrowserProxy(ctx, d) }
	var profile string
	original := p
	p.browserLaunch = func(ctx context.Context, dir, proxy string) (string, func(), error) {
		profile = dir
		if executable := os.Getenv("NOLE_TEST_REAL_BROWSER_EXECUTABLE"); executable != "" {
			return original.launchBrowserExecutable(ctx, dir, proxy, executable)
		}
		return original.launchBrowser(ctx, dir, proxy)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := p.runBrowser(ctx, "http://public.example/", "text")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Rendered public article.") {
		t.Fatalf("missing rendered article, failure=%s", result.Failure)
	}
	if rebindLookups.Load() < 2 {
		t.Fatal("browser did not exercise DNS change across redirect")
	}
	if publicDials.Load() == 0 {
		t.Fatal("browser never reached controlled public page")
	}
	if forbiddenDials.Load() != 0 {
		t.Fatal("private target reached socket dialer")
	}
	if trapHits.Load() != 0 {
		t.Fatalf("private trap reached %d times", trapHits.Load())
	}
	// Zero trap hits alone could mean Chromium blocked or never issued a
	// request. Each execution context must actually reach the guard's resolver,
	// which returns a forbidden address that must never reach the dialer.
	lookupMu.Lock()
	for _, host := range []string{"page-trap.example", "worker-trap.example", "shared-trap.example"} {
		if privateLookups[host] == 0 {
			t.Errorf("guard did not receive private-address probe from %s", host)
		}
	}
	lookupMu.Unlock()
	assertBrowserGone(t, profile)
	t.Log("Actual Chromium: page, Worker, SharedWorker HTTP/WS probes completed; private trap hits=0; profile and browser removed")
}

func assertBrowserGone(t *testing.T, profile string) {
	t.Helper()
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("profile survived cleanup")
	}
	out, err := exec.Command("ps", "-axo", "command=").Output()
	if err != nil {
		t.Fatal("cannot inspect owned process cleanup")
	}
	if strings.Contains(string(out), "--user-data-dir="+profile) {
		t.Fatal("owned Chromium survived cleanup")
	}
}

// This dependency contract does not launch a browser. Scrapling's ordinary
// ProxyDict drops bypass; the documented context override must preserve it.
func TestInstalledBrowserRuntimeOptions(t *testing.T) {
	python := os.Getenv("NOLE_TEST_REAL_BROWSER_PYTHON")
	if python == "" {
		t.Skip("optional Scrapling runtime required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := browserRuntimeScript + `
from scrapling.fetchers import RemoteSession
for enabled in (False, True):
    opts = _browser_options(1000, 'http://nole:synthetic@127.0.0.1:12345', None, BrowserGuard(), enabled)
    session = RemoteSession(cdp_url='http://127.0.0.1:23456', new_context=True, **opts)
    assert session._context_options['proxy']['bypass'] == '<-loopback>'
    assert session._context_options['service_workers'] == 'block'
    assert session._context_options['ignore_https_errors'] is False
    assert session._config.solve_cloudflare is enabled
`
	if err := exec.CommandContext(ctx, python, "-B", "-c", script).Run(); err != nil {
		t.Fatal("installed runtime did not preserve security/solver options")
	}
}
