// Package selfupdate performs a fail-soft, advisory check for a newer published
// Nólë release. It is the one place in the CLI that makes an outbound network
// call, and it is built to be invisible when it cannot help:
//
//   - It NEVER returns an error and NEVER writes to stdout or stderr. Staleness
//     is advisory; the caller decides whether to print anything.
//   - Any failure (offline, DNS error, timeout, non-200, malformed body, bad
//     version strings) yields Result{Checked:false}, and the caller stays SILENT.
//   - It sends NO Authorization header — this is an anonymous check of a public
//     releases endpoint.
//   - A short timeout bounds it so `doctor --check-updates` never hangs.
//
// The releases endpoint is overridable via NOLE_RELEASES_API (used by tests to
// point at an httptest server; it is config, not a secret).
package selfupdate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com"
	releasePath    = "/repos/dorukardahan/nole/releases/latest"
	requestTimeout = 3 * time.Second
	maxBodyBytes   = 1 << 20
)

// Result is the outcome of a staleness check. Checked is false whenever the
// check could not complete — callers MUST stay silent in that case. Stale and
// Comparable are only meaningful when Checked is true. Comparable is false when
// Current is not a release-shaped version (e.g. a "dev" / `go run` build), so
// callers can avoid falsely reporting such a build as "up to date".
type Result struct {
	Current    string
	Latest     string
	Stale      bool
	Checked    bool
	Comparable bool
}

// CheckLatest reports whether `current` is behind the latest published release.
// It never errors, never writes output, and resolves the endpoint from
// NOLE_RELEASES_API (default: the public GitHub API).
func CheckLatest(ctx context.Context, current string) Result {
	base := strings.TrimSpace(os.Getenv("NOLE_RELEASES_API"))
	if base == "" {
		base = defaultBaseURL
	}
	return checkLatest(ctx, current, base, &http.Client{Timeout: requestTimeout})
}

// checkLatest is the injectable core: tests pass an httptest base URL + client so
// they never touch the network.
func checkLatest(ctx context.Context, current, baseURL string, client *http.Client) Result {
	res := Result{Current: current}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + releasePath
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return res // Checked stays false → caller is silent.
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Deliberately NO Authorization header: anonymous check of a public endpoint.

	resp, err := client.Do(req)
	if err != nil {
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return res
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&payload); err != nil {
		return res
	}
	latest := strings.TrimSpace(payload.TagName)
	if latest == "" {
		return res
	}

	res.Latest = latest
	res.Checked = true
	// Comparable iff BOTH versions are release-shaped; a dev/non-release current
	// is not behind anything (Stale stays false) but is also NOT "up to date".
	_, okCur := parseRelease(current)
	_, okLatest := parseRelease(latest)
	res.Comparable = okCur && okLatest
	res.Stale = isOlder(current, latest)
	return res
}

// isOlder reports whether current < latest by per-segment NUMERIC comparison
// (so 0.10.0 > 0.9.0, which a lexical compare would get wrong). Fail-soft: if
// EITHER version is not strictly release-shaped after stripping a leading 'v'
// and any -prerelease/+build metadata, it returns false (no nag) — covering
// "dev", "dev-build-check", and any garbage.
func isOlder(current, latest string) bool {
	c, okc := parseRelease(current)
	l, okl := parseRelease(latest)
	if !okc || !okl {
		return false
	}
	for i := 0; i < 3; i++ {
		if c[i] != l[i] {
			return c[i] < l[i]
		}
	}
	return false
}

// parseRelease parses a strictly release-shaped version into [major,minor,patch].
// Strips one leading 'v' and any +build / -prerelease metadata, then requires
// exactly three non-negative numeric segments. Returns ok=false otherwise, so a
// non-release version never triggers a stale warning.
func parseRelease(v string) ([3]int, bool) {
	var out [3]int
	s := strings.TrimSpace(v)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
