package scrapling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// One browser session per Nole process, including no-navigation readiness
// probes. Acquisition is covered by the caller's deadline, even in coalesced work.
var browserSlot = make(chan struct{}, 1)

func (p *Provider) configureBrowser() {
	p.mode = strings.TrimSpace(os.Getenv("NOLE_SCRAPLING_MODE"))
	if p.mode == "" {
		p.mode = "fetcher"
	}
	if p.mode != "fetcher" && p.mode != "auto" && p.mode != "stealthy" {
		p.configErr = errors.New("scrapling: invalid NOLE_SCRAPLING_MODE")
	}
	p.browserTimeout = 60 * time.Second
	if p.mode == "auto" || p.mode == "stealthy" {
		p.timeout = 90 * time.Second
	}
	for key, dest := range map[string]*time.Duration{
		"NOLE_SCRAPLING_TIMEOUT":         &p.timeout,
		"NOLE_SCRAPLING_BROWSER_TIMEOUT": &p.browserTimeout,
	} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			duration, err := time.ParseDuration(value)
			if err != nil || duration <= 0 {
				p.configErr = errors.New("scrapling: invalid " + key)
			} else {
				*dest = duration
			}
		}
	}
}

func remainingMilliseconds(ctx context.Context, limit time.Duration) int64 {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < limit {
		limit = time.Until(deadline)
	}
	return max(1, limit.Milliseconds())
}

func browserSession(ctx context.Context) (string, func(), error) {
	if !browserProcessSupported {
		return "", nil, errors.New("scrapling: browser process cleanup unavailable on this platform")
	}
	select {
	case browserSlot <- struct{}{}:
	case <-ctx.Done():
		return "", nil, errors.New("scrapling: browser deadline exceeded while waiting")
	}
	profile, err := os.MkdirTemp("", "nole-browser-")
	if err != nil {
		<-browserSlot
		return "", nil, errors.New("scrapling: cannot create isolated browser profile")
	}
	return profile, func() {
		_ = os.RemoveAll(profile)
		<-browserSlot
	}, nil
}

func (p Provider) runBrowser(ctx context.Context, url, format string) (hopOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, p.browserTimeout)
	defer cancel()
	profile, cleanup, err := browserSession(ctx)
	if err != nil {
		return hopOutput{}, err
	}
	defer cleanup()
	return p.execMode(ctx, url, format, "stealthy", profile)
}

func (p Provider) browserReadiness(ctx context.Context) (bool, string) {
	profile, cleanup, err := browserSession(ctx)
	if err != nil {
		return false, "browser unavailable: process support or readiness deadline"
	}
	defer cleanup()
	payload, _ := json.Marshal(map[string]any{"proxy": p.proxy, "profile": profile})
	cmd := exec.CommandContext(ctx, p.python, "-c", browserRuntimeScript+browserStatusScript)
	cmd.Env = p.subprocessEnv()
	cmd.Stdin = bytes.NewReader(payload)
	stdout, stderr := newCappedBuffer(maxStderrBytes), newCappedBuffer(maxStderrBytes)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if runBrowserCommand(cmd) != nil {
		return false, "browser unavailable: local launch or readiness deadline"
	}
	var out struct {
		Ready            bool `json:"ready"`
		ProxyUnsupported bool `json:"proxy_unsupported"`
	}
	if json.Unmarshal(stdout.Bytes(), &out) != nil {
		return false, "browser unavailable: invalid readiness response"
	}
	if out.ProxyUnsupported {
		return false, "browser unavailable: configured proxy is unsupported by browser mode"
	}
	if !out.Ready {
		return false, "browser unavailable: install optional Scrapling browser components or check local runtime"
	}
	return true, "browser ready (local launch verified)"
}

const browserStatusScript = `
req = json.load(sys.stdin)
try:
    check_browser(profile=req['profile'], proxy=req.get('proxy'))
    print(json.dumps({'ready': True}))
except BrowserProxyError:
    print(json.dumps({'ready': False, 'proxy_unsupported': True}))
except BaseException:
    print(json.dumps({'ready': False}))
`
