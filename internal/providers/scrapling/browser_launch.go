package scrapling

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Nole launches Chromium itself. Patchright only connects to this owned process;
// it cannot put a newly launched browser outside Nole's cleanup process group.
func (p Provider) launchBrowser(ctx context.Context, profile, proxy string) (string, func(), error) {
	if p.browserLaunch != nil {
		return p.browserLaunch(ctx, profile, proxy)
	}
	cmd := exec.CommandContext(ctx, p.python, "-c", `from patchright.sync_api import sync_playwright
with sync_playwright() as p:
    print(p.chromium.executable_path)
`)
	cmd.Env = p.subprocessEnv()
	output := newCappedBuffer(4096)
	cmd.Stdout = output
	cmd.Stderr = newCappedBuffer(maxStderrBytes)
	if runBrowserCommand(cmd) != nil {
		return "", nil, errors.New("scrapling: browser executable unavailable")
	}
	executable := strings.TrimSpace(output.String())
	if !filepath.IsAbs(executable) {
		return "", nil, errors.New("scrapling: browser executable unavailable")
	}
	return p.launchBrowserExecutable(ctx, profile, proxy, executable)
}

func (p Provider) launchBrowserExecutable(ctx context.Context, profile, proxy, executable string) (string, func(), error) {
	u, err := url.Parse(proxy)
	if err != nil {
		return "", nil, errBrowserNetwork
	}
	args := []string{
		"--headless=new", "--no-first-run", "--no-default-browser-check",
		"--disable-crashpad-for-testing", "--disable-breakpad", "--disable-crash-reporter",
		"--use-mock-keychain", "--password-store=basic",
		"--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
		"--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--disable-features=WebTransport,DirectSockets", "--proxy-bypass-list=<-loopback>",
		"--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1",
		"--proxy-server=http://" + u.Host,
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
		"--user-data-dir=" + profile, "about:blank",
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = p.subprocessEnv()
	cmd.Stdout = newCappedBuffer(maxStderrBytes)
	cmd.Stderr = newCappedBuffer(maxStderrBytes)
	stop, done, err := startOwnedBrowser(cmd)
	if err != nil {
		return "", nil, errors.New("scrapling: browser launch failed")
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			port, rest, complete := strings.Cut(string(data), "\n")
			if number, err := strconv.Atoi(port); err == nil && complete && strings.TrimSpace(rest) != "" && number > 0 && number <= 65535 {
				return "http://" + net.JoinHostPort("127.0.0.1", port), stop, nil
			}
		}
		select {
		case <-ctx.Done():
			stop()
			return "", nil, errors.New("scrapling: browser startup deadline exceeded")
		case <-done:
			stop()
			return "", nil, errors.New("scrapling: browser exited before readiness")
		case <-tick.C:
		}
	}
}
