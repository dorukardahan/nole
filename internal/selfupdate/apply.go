package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// This file implements `nole self-update`'s apply path. It mirrors
// scripts/install.sh's contract EXACTLY so the two installers share one mental
// model: SHA256 is the mandatory integrity floor (verified in-process), and a
// GitHub build-provenance attestation is an ADDITIVE, best-effort second gate
// done by SHELLING OUT to `gh attestation verify` when a usable gh is present.
// We deliberately do NOT vendor sigstore-go (it would add ~70 transitive deps,
// ~15-26 MB, and force a Go 1.25 toolchain for a feature that runs only at
// upgrade time — exactly the heavy dependency the gateway exists to refuse).
//
// The self-replace is hand-rolled (no self-update library): stage a NEW file in
// the target's own directory and atomically rename it over the running binary.
// Writing a new inode (never truncating/overwriting in place) is what keeps it
// Apple-Silicon-safe — overwriting a mapped, signed Mach-O in place SIGKILLs the
// next exec.

const (
	// SignedSince is the first release whose assets carry a build-provenance
	// attestation. For a target >= this, a reachable-but-unverified attestation is
	// fail-closed (tampering); older targets soft-skip. Keep in sync with
	// scripts/install.sh's SIGNED_SINCE.
	SignedSince = "v0.10.0"
	// GhMinVersion is the minimum gh free of CVE-2026-48501 (gh attestation leaked
	// the host token to TUF mirrors before 2.93.0). Older gh is treated as "no
	// verifier" so we never invoke a token-leaking binary. Matches install.sh.
	GhMinVersion = "2.93.0"

	defaultDownloadURL = "https://github.com"
	defaultRepo        = "dorukardahan/nole"
)

// VerifyMode mirrors NOLE_INSTALL_VERIFY: auto (verify if a usable gh is present,
// else soft-skip), require (any soft-skip is a hard error), off (skip the
// attestation gate; SHA256 stays mandatory).
type VerifyMode string

const (
	VerifyAuto    VerifyMode = "auto"
	VerifyRequire VerifyMode = "require"
	VerifyOff     VerifyMode = "off"
)

// ParseVerifyMode validates a mode string, rejecting unknown values early (a
// typo like "required" must NOT silently degrade to auto). An empty string maps
// to the auto default.
func ParseVerifyMode(s string) (VerifyMode, error) {
	switch strings.TrimSpace(s) {
	case "", string(VerifyAuto):
		return VerifyAuto, nil
	case string(VerifyRequire):
		return VerifyRequire, nil
	case string(VerifyOff):
		return VerifyOff, nil
	default:
		return "", fmt.Errorf("invalid verify mode %q (expected one of: auto, require, off)", s)
	}
}

// Options configures Apply. The zero value resolves all defaults from the
// environment (NOLE_RELEASES_API, NOLE_INSTALL_DOWNLOAD_URL, NOLE_INSTALL_REPO)
// and os.Executable(); tests override the unexported fields.
type Options struct {
	Current    string     // running version (version.Version)
	Target     string     // explicit tag to install; "" => latest published
	Mode       VerifyMode // attestation verification policy
	CheckOnly  bool       // report only; download/replace nothing
	Out        io.Writer  // progress sink (interactive CLI; stdout is fine here)
	HTTPClient *http.Client

	// test seams (package-internal):
	apiBaseURL      string // default: NOLE_RELEASES_API or https://api.github.com
	downloadBaseURL string // default: NOLE_INSTALL_DOWNLOAD_URL or https://github.com
	repo            string // default: NOLE_INSTALL_REPO or dorukardahan/nole
	exePath         string // default: os.Executable()
}

// ApplyResult reports what Apply did.
type ApplyResult struct {
	Action         string // "up-to-date" | "check-only" | "updated"
	From           string
	To             string
	AttestVerified bool
	AttestSkip     string // reason the attestation gate soft-skipped, if any
}

func (o *Options) resolve() {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 60 * time.Second} // large enough for a binary download
	}
	if o.apiBaseURL == "" {
		// NOLE_RELEASES_API, then the installer's NOLE_INSTALL_API_URL, then the
		// public API — so a mirror/GHE install resolves "latest" from the same API
		// base install.sh used.
		o.apiBaseURL = envAPIBase()
	}
	if o.downloadBaseURL == "" {
		o.downloadBaseURL = strings.TrimSpace(os.Getenv("NOLE_INSTALL_DOWNLOAD_URL"))
		if o.downloadBaseURL == "" {
			o.downloadBaseURL = defaultDownloadURL
		}
	}
	if o.repo == "" {
		o.repo = strings.TrimSpace(os.Getenv("NOLE_INSTALL_REPO"))
		if o.repo == "" {
			o.repo = defaultRepo
		}
	}
	if o.Mode == "" {
		o.Mode = VerifyAuto
	}
}

func (o *Options) logf(format string, a ...any) { fmt.Fprintf(o.Out, format+"\n", a...) }

// AssetName returns the release asset for the running platform, matching
// scripts/check-release-builds.sh (nole-<os>-<arch>, .exe on Windows).
func AssetName() (string, error) {
	var goos string
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		goos = runtime.GOOS
	default:
		return "", fmt.Errorf("unsupported OS %q", runtime.GOOS)
	}
	var arch string
	switch runtime.GOARCH {
	case "amd64", "arm64":
		arch = runtime.GOARCH
	default:
		return "", fmt.Errorf("unsupported architecture %q", runtime.GOARCH)
	}
	name := fmt.Sprintf("nole-%s-%s", goos, arch)
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

// Apply runs the self-update: resolve target -> (check-only?) -> download +
// verify SHA256 (mandatory) -> attestation gate (additive) -> atomic self-replace.
// It returns a non-nil error on any hard failure (SHA256 mismatch, attestation
// fail-closed, replace failure); the existing binary is never disturbed unless
// the new one is fully verified and staged.
func Apply(ctx context.Context, opts Options) (ApplyResult, error) {
	opts.resolve()

	// 1. Resolve the target version.
	target := strings.TrimSpace(opts.Target)
	res := ApplyResult{From: opts.Current}
	if target == "" {
		chk := checkLatest(ctx, opts.Current, opts.apiBaseURL, opts.repo, opts.HTTPClient)
		if !chk.Checked {
			return res, fmt.Errorf("could not determine the latest release (offline or GitHub unavailable)")
		}
		target = chk.Latest
		// Only short-circuit "up to date" when the running version is comparable
		// (a dev build is never "up to date"; it should still be able to install
		// the latest with an explicit --version, but a bare self-update on a dev
		// build proceeds to install the latest release).
		if chk.Comparable && !chk.Stale {
			res.Action, res.To = "up-to-date", target
			opts.logf("nole %s is already up to date", opts.Current)
			return res, nil
		}
	}
	res.To = target

	if opts.CheckOnly {
		res.Action = "check-only"
		opts.logf("a newer release is available: %s (current: %s)", target, opts.Current)
		opts.logf("run 'nole self-update' to install it")
		return res, nil
	}

	asset, err := AssetName()
	if err != nil {
		return res, err
	}

	// 2. Download asset + SHA256SUMS into a temp dir.
	tmp, err := os.MkdirTemp("", "nole-self-update-")
	if err != nil {
		return res, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	base := strings.TrimRight(opts.downloadBaseURL, "/") + "/" + opts.repo + "/releases/download/" + target
	opts.logf("downloading %s %s", asset, target)
	assetBytes, err := download(ctx, opts.HTTPClient, base+"/"+asset)
	if err != nil {
		return res, fmt.Errorf("download %s: %w", asset, err)
	}
	sumsBytes, err := download(ctx, opts.HTTPClient, base+"/SHA256SUMS")
	if err != nil {
		return res, fmt.Errorf("download SHA256SUMS: %w", err)
	}

	// 3. MANDATORY SHA256 floor (in-process). Fail closed on any mismatch.
	if err := verifyChecksum(assetBytes, sumsBytes, asset); err != nil {
		return res, err
	}
	opts.logf("checksum verified")

	// Write the verified bytes to the temp dir so the attestation gate can run
	// `gh attestation verify` against a real file path.
	stagedTmp := filepath.Join(tmp, asset)
	if err := os.WriteFile(stagedTmp, assetBytes, 0o755); err != nil {
		return res, fmt.Errorf("write temp binary: %w", err)
	}

	// 4. ADDITIVE attestation gate (best-effort; SHA256 already passed).
	skip, err := verifyAttestation(ctx, stagedTmp, opts.repo, target, opts.Mode)
	if err != nil {
		return res, err
	}
	if skip == "" {
		res.AttestVerified = true
		opts.logf("attestation verified (build provenance, %s)", opts.repo)
	} else {
		res.AttestSkip = skip
		opts.logf("attestation check skipped: %s (SHA256 already verified)", skip)
	}

	// 5. Atomic self-replace.
	exe := opts.exePath
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return res, fmt.Errorf("locate running binary: %w", err)
		}
		if resolved, lerr := filepath.EvalSymlinks(exe); lerr == nil {
			exe = resolved
		}
	}
	if err := installBinary(assetBytes, exe); err != nil {
		return res, err
	}
	res.Action = "updated"
	opts.logf("updated to %s — restart nole to use the new version", target)
	return res, nil
}

// download GETs url and returns the body, bounded and anonymous (no auth header,
// matching CheckLatest). A non-200 is an error.
func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 256 MiB ceiling: a nole binary is a few tens of MB; this bounds a hostile
	// or misconfigured endpoint without truncating a legitimate asset.
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// verifyChecksum confirms sha256(asset) matches the asset's line in a SHA256SUMS
// file ("<hex>  <name>", two spaces). Fail-closed on a missing/mismatched entry.
func verifyChecksum(asset, sums []byte, name string) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimRight(line, "\r")
		// Format: 64-hex, two spaces, filename.
		if strings.HasSuffix(line, "  "+name) {
			want = strings.TrimSpace(line[:len(line)-len(name)])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksum verification FAILED: SHA256SUMS has no entry for %s — refusing to install", name)
	}
	sum := sha256.Sum256(asset)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(strings.TrimSpace(want), got) {
		return fmt.Errorf("checksum verification FAILED for %s — refusing to install", name)
	}
	return nil
}

// verifyAttestation is the Go port of install.sh's attest_verify three-way
// taxonomy. It returns ("", nil) when the attestation verified, (reason, nil)
// when it soft-skipped, and (_, err) when it fails closed.
func verifyAttestation(ctx context.Context, file, repo, version string, mode VerifyMode) (skip string, err error) {
	if mode == VerifyOff {
		return "attestation check disabled (--verify off)", nil
	}

	// Capability probe: gh present, has `attestation verify`, and >= GhMinVersion.
	reason := ""
	if !lookGhFunc() {
		reason = "gh not installed"
	} else if !ghHasAttestationVerify(ctx) {
		reason = "installed gh lacks 'attestation verify'"
	} else if !ghVersionOK(ctx) {
		reason = "gh < " + GhMinVersion + " (CVE-2026-48501)"
	}
	if reason != "" {
		if mode == VerifyRequire {
			return "", fmt.Errorf("--verify require but no usable attestation verifier: %s (install GitHub CLI gh >= %s)", reason, GhMinVersion)
		}
		return reason, nil
	}

	// Verify, hardened to the exact release-workflow signer identity. We pass NO
	// token; gh uses whatever auth the host carries. An anonymous host hits the
	// public-repo auth limit (cli/cli #11803) and lands in the unreachable
	// soft-skip branch below.
	out, code := runGh(ctx, "attestation", "verify", file,
		"--repo", repo,
		"--signer-workflow", repo+"/.github/workflows/release.yml")
	if code == 0 {
		return "", nil
	}

	// Could-not-verify (offline/anonymous) vs reachable-but-unverified. The set is
	// CONSERVATIVE: a missed pattern falls through to fail-closed-on-signed, never
	// soft-skips a genuine failure.
	if isUnreachable(out) {
		if mode == VerifyRequire {
			return "", fmt.Errorf("--verify require: could not reach/authenticate to the attestation API to verify %s — %s", filepath.Base(file), trim(out))
		}
		return "attestation API unreachable/unauthenticated", nil
	}
	// Reachable, did not verify.
	if isCleanRelease(version) {
		if releaseAtLeast(version, SignedSince) {
			return "", fmt.Errorf("attestation verification FAILED for %s (%s) — refusing to install (possible tampering; use --verify off to override): %s", filepath.Base(file), version, trim(out))
		}
		// clean release below the cutover -> pre-signing
	} else if looksLikeReleaseTag(version) {
		return "", fmt.Errorf("attestation verification FAILED for %s: malformed release tag %q could not be confirmed pre-signing — refusing to install (use --verify off to override): %s", filepath.Base(file), version, trim(out))
	}
	if mode == VerifyRequire {
		return "", fmt.Errorf("--verify require but %s (%s) has no verifiable attestation (predates signing or is not a release tag)", filepath.Base(file), version)
	}
	return "no verifiable attestation for " + version + " (pre-signing release)", nil
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// isUnreachable reports whether gh's output indicates a network/auth failure
// (can't-verify) rather than a real verification failure. Mirrors install.sh.
func isUnreachable(out string) bool {
	for _, p := range []string{
		"HTTP 401", "HTTP 403", "authentication", "Unauthorized", "log in", "gh auth",
		"rate limit", "connection refused", "no such host", "i/o timeout", "deadline exceeded",
		"dial tcp", "lookup ", "network is unreachable", "no route to host", "TLS handshake",
		"server misbehaving",
	} {
		if strings.Contains(out, p) {
			return true
		}
	}
	return false
}

// isCleanRelease reports whether v strips to exactly three numeric segments.
func isCleanRelease(v string) bool {
	_, ok := parseRelease(v)
	return ok
}

// looksLikeReleaseTag reports whether v is SHAPED like a release tag (optional
// 'v' then a digit) — distinguishes a malformed release tag ("v0.10") from a
// genuine non-release ref ("dev"). Mirrors install.sh's looks_like_release_tag.
func looksLikeReleaseTag(v string) bool {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

// releaseAtLeast reports whether release-shaped a >= release-shaped b.
func releaseAtLeast(a, b string) bool {
	av, aok := parseRelease(a)
	bv, bok := parseRelease(b)
	if !aok || !bok {
		return false
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return true
}

// --- gh shell-out helpers ---

// lookGhFunc reports whether a `gh` binary is on PATH; overridable in tests.
var lookGhFunc = func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// runGhFunc is the gh runner; overridable in tests, defaults to real exec.
var runGhFunc = func(ctx context.Context, args ...string) (string, int) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	return string(out), -1
}

func runGh(ctx context.Context, args ...string) (string, int) { return runGhFunc(ctx, args...) }

func ghHasAttestationVerify(ctx context.Context) bool {
	_, code := runGh(ctx, "attestation", "verify", "--help")
	return code == 0
}

func ghVersionOK(ctx context.Context) bool {
	out, code := runGh(ctx, "--version")
	if code != 0 {
		return false
	}
	// First line: "gh version 2.93.0 (2026-04-01)".
	line := out
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	for _, f := range strings.Fields(line) {
		if _, ok := parseRelease(f); ok {
			return releaseAtLeast(f, GhMinVersion)
		}
	}
	return false
}
