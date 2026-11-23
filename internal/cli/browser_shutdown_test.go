//go:build unix

package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/dorukardahan/nole/internal/core"
	"github.com/dorukardahan/nole/internal/providers/scrapling"
)

// Real CLI cancellation and OS processes; fake runtime deliberately ignores
// SIGTERM in the owned browser, exercising the forced group cleanup path.
func TestBrowserCLIInterruptJoinsOwnedProcesses(t *testing.T) {
	if os.Getenv("NOLE_TEST_INTERRUPT_HELPER") == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		registry := core.NewRegistry()
		registry.Register(scrapling.New())
		ledger := core.NewMemoryQuotaLedger()
		ledger.Set(core.QuotaEntry{Provider: "scrapling", CostClass: core.CostClassKeylessFree, KeylessFree: true})
		svc := core.NewService(registry, ledger, core.RouteMatrix{core.TaskExtract: {"scrapling"}})
		cmd := newExtractCommandWithService(func() *core.Service { return svc })
		cmd.SetArgs([]string{"https://8.8.8.8/", "--json"})
		_ = cmd.ExecuteContext(ctx)
		return
	}
	python := browserFixturePython(t)
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("scrapling/__init__.py", "", 0600)
	write("scrapling/fetchers.py", `import os,time
from types import SimpleNamespace
class Context:
 def route(self,*args): pass
 def route_web_socket(self,*args): pass
 def close(self): pass
class RemoteSession:
 def __init__(self,**options): self.options=options;self.context=Context();self._context_options=options["additional_args"]
 def __enter__(self): return self
 def __exit__(self,*args): pass
 def fetch(self,url):
  self.options['page_setup'](SimpleNamespace(context=self.context))
  open(os.environ['NOLE_TEST_FETCH_STARTED'],'w').close()
  time.sleep(60)
`, 0600)
	write("patchright/__init__.py", "", 0600)
	write("patchright/sync_api.py", `import os
from types import SimpleNamespace
class sync_playwright:
 def __enter__(self): return SimpleNamespace(chromium=SimpleNamespace(executable_path=os.environ['NOLE_TEST_BROWSER_EXEC']))
 def __exit__(self,*args): pass
`, 0600)
	write("fake-browser", "#!"+python+"\n"+`import os,sys,signal,time,pathlib,subprocess
signal.signal(signal.SIGTERM,signal.SIG_IGN)
profile=next(a.split('=',1)[1] for a in sys.argv if a.startswith('--user-data-dir='))
pathlib.Path(os.environ['NOLE_TEST_PROFILE_RECORD']).write_text(profile)
pathlib.Path(os.environ['NOLE_TEST_BROWSER_RECORD']).write_text(str(os.getpid()))
pathlib.Path(profile,'DevToolsActivePort').write_text('12345\n/devtools/browser/fixture\n')
child=subprocess.Popen([sys.executable,'-c','import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);time.sleep(60)'])
pathlib.Path(os.environ['NOLE_TEST_CHILD_RECORD']).write_text(str(child.pid))
while True: time.sleep(1)
`, 0700)
	started := filepath.Join(dir, "started")
	record := filepath.Join(dir, "profile")
	childRecord := filepath.Join(dir, "child")
	browserRecord := filepath.Join(dir, "browser")
	env := map[string]string{"NOLE_TEST_INTERRUPT_HELPER": "1", "NOLE_SCRAPLING_MODE": "stealthy", "NOLE_SCRAPLING_PYTHON": python, "NOLE_SCRAPLING_PROXY": "", "NOLE_SCRAPLING_SOLVE_CLOUDFLARE": "false", "PYTHONPATH": dir, "PYTHONDONTWRITEBYTECODE": "1", "NOLE_TEST_BROWSER_EXEC": filepath.Join(dir, "fake-browser"), "NOLE_TEST_FETCH_STARTED": started, "NOLE_TEST_PROFILE_RECORD": record, "NOLE_TEST_CHILD_RECORD": childRecord, "NOLE_TEST_BROWSER_RECORD": browserRecord}
	for key, value := range env {
		t.Setenv(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserCLIInterruptJoinsOwnedProcesses$", "-test.timeout=10s")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			child.Process.Signal(os.Interrupt)
			child.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser fetch did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	profileBytes, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		joined = true
		t.Fatalf("CLI helper did not exit cleanly: %v", err)
	}
	joined = true
	if _, err := os.Stat(string(profileBytes)); !os.IsNotExist(err) {
		t.Fatal("Ctrl-C left browser profile behind")
	}
	for _, recordPath := range []string{browserRecord, childRecord} {
		raw, err := os.ReadFile(recordPath)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			err = syscall.Kill(pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("owned process survived shutdown (signal check: %v)", err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	t.Log("CLI SIGINT waits for detached shared work; owned browser ignoring SIGTERM is killed and profile removed")
}
