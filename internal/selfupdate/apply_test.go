package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is intentionally NOT the canonical repo: the unpinned apply tests
// (Target=="") only resolve "latest" correctly if checkLatest honours the
// overridden repo, so using a non-canonical repo here is the regression guard
// for that (an unpinned self-update on a fork must resolve the fork's latest).
const testRepo = "testowner/testrepo"

// releaseServer serves the latest-tag JSON, the asset bytes, and a SHA256SUMS
// whose hash is sumHex (caller controls it to force match/mismatch). It fails the
// test if any request carries an Authorization header (the path must stay
// anonymous).
func releaseServer(t *testing.T, tag, asset string, body []byte, sumHex string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+testRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/"+testRepo+"/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/"+testRepo+"/releases/download/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", sumHex, asset)
	})
	srv := httptest.NewServer(authGuard(t, mux))
	t.Cleanup(srv.Close)
	return srv
}

func authGuard(t *testing.T, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("self-update must not send an Authorization header; got %q", r.Header.Get("Authorization"))
		}
		h.ServeHTTP(w, r)
	})
}

// setFakeGh installs a fake gh for the duration of the test. present controls the
// PATH probe; version is what `gh --version` reports; helpOK is the exit of
// `attestation verify --help`; verifyOut/verifyExit drive the real verify. A
// non-nil verifyCalled is set true when the REAL verify (not --help) is invoked.
func setFakeGh(t *testing.T, present bool, version string, helpOK bool, verifyOut string, verifyExit int, verifyCalled *bool) {
	t.Helper()
	oldLook, oldRun := lookGhFunc, runGhFunc
	t.Cleanup(func() { lookGhFunc, runGhFunc = oldLook, oldRun })
	lookGhFunc = func() bool { return present }
	runGhFunc = func(ctx context.Context, args ...string) (string, int) {
		if len(args) >= 1 && args[0] == "--version" {
			return "gh version " + version + " (2026-01-01)\nhttps://github.com/cli/cli/releases/latest", 0
		}
		if len(args) >= 2 && args[0] == "attestation" && args[1] == "verify" {
			if len(args) >= 3 && args[2] == "--help" {
				if helpOK {
					return "", 0
				}
				return "unknown command", 1
			}
			if verifyCalled != nil {
				*verifyCalled = true
			}
			return verifyOut, verifyExit
		}
		return "", 0
	}
}

func tempExe(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "nole")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newOpts(srvURL, exe, current, target string, mode VerifyMode) Options {
	return Options{
		Current:         current,
		Target:          target,
		Mode:            mode,
		Out:             &strings.Builder{},
		apiBaseURL:      srvURL,
		downloadBaseURL: srvURL,
		repo:            testRepo,
		exePath:         exe,
		HTTPClient:      http.DefaultClient,
	}
}

func mustAsset(t *testing.T) string {
	t.Helper()
	a, err := AssetName()
	if err != nil {
		t.Skipf("unsupported platform: %v", err)
	}
	return a
}

func TestApply_UpdatesAndVerifies(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW-NOLE-BINARY\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:]))
	exe := tempExe(t, "OLD\n")

	var verified bool
	setFakeGh(t, true, "2.93.0", true, "Verification succeeded", 0, &verified)

	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err != nil {
		t.Fatalf("update should succeed: %v", err)
	}
	if res.Action != "updated" || !res.AttestVerified {
		t.Fatalf("want updated+verified, got %+v", res)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(body) {
		t.Fatalf("binary not replaced: %q", string(got))
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("replaced binary not executable")
	}
	if !verified {
		t.Fatalf("gh attestation verify was not invoked")
	}
	// No staging leftovers.
	entries, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".nole.update") || strings.HasSuffix(e.Name(), ".old") {
			t.Fatalf("leftover staging file: %s", e.Name())
		}
	}
}

func TestApply_UpToDateNoOp(t *testing.T) {
	asset := mustAsset(t)
	srv := releaseServer(t, "v0.11.0", asset, []byte("x"), strings.Repeat("0", 64))
	exe := tempExe(t, "CURRENT\n")
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.11.0", "", VerifyAuto))
	if err != nil {
		t.Fatalf("up-to-date must not error: %v", err)
	}
	if res.Action != "up-to-date" {
		t.Fatalf("want up-to-date, got %+v", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "CURRENT\n" {
		t.Fatalf("binary changed on a no-op: %q", string(got))
	}
}

func TestApply_CheckOnlyDownloadsNothing(t *testing.T) {
	asset := mustAsset(t)
	srv := releaseServer(t, "v0.11.0", asset, []byte("x"), strings.Repeat("0", 64))
	exe := tempExe(t, "CURRENT\n")
	o := newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto)
	o.CheckOnly = true
	res, err := Apply(context.Background(), o)
	if err != nil {
		t.Fatalf("check-only must not error: %v", err)
	}
	if res.Action != "check-only" || res.To != "v0.11.0" {
		t.Fatalf("want check-only -> v0.11.0, got %+v", res)
	}
	if got, _ := os.ReadFile(exe); string(got) != "CURRENT\n" {
		t.Fatalf("check-only must not replace the binary: %q", string(got))
	}
}

func TestApply_ChecksumMismatch_FailsClosed(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("REAL\n")
	srv := releaseServer(t, "v0.11.0", asset, body, strings.Repeat("0", 64)) // wrong hash
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, true, "2.93.0", true, "", 0, nil)
	_, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err == nil || !strings.Contains(err.Error(), "checksum verification FAILED") {
		t.Fatalf("want checksum fail-closed, got err=%v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD\n" {
		t.Fatalf("binary replaced despite checksum mismatch: %q", string(got))
	}
}

func TestApply_AttestationMismatch_FailsClosed(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("TAMPERED\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:])) // SHA256 passes
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, true, "2.93.0", true, "failed to verify certificate identity", 1, nil)
	_, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err == nil || !strings.Contains(err.Error(), "attestation verification FAILED") {
		t.Fatalf("want attestation fail-closed, got err=%v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD\n" {
		t.Fatalf("binary replaced despite attestation mismatch: %q", string(got))
	}
}

func TestApply_VerifierAbsent_SoftSkipInstalls(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:]))
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, false, "", true, "", 0, nil) // gh absent
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err != nil {
		t.Fatalf("absent verifier must soft-skip + install: %v", err)
	}
	if res.Action != "updated" || res.AttestVerified {
		t.Fatalf("want updated + not-verified soft-skip, got %+v", res)
	}
	if !strings.Contains(res.AttestSkip, "gh not installed") {
		t.Fatalf("want gh-absent skip reason, got %q", res.AttestSkip)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(body) {
		t.Fatalf("binary not installed via SHA256 alone")
	}
}

func TestApply_CVEGate_OldGh_SoftSkip(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:]))
	exe := tempExe(t, "OLD\n")
	var verified bool
	setFakeGh(t, true, "2.89.0", true, "", 1, &verified) // pre-CVE-fix gh
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err != nil {
		t.Fatalf("old gh must soft-skip, not fail: %v", err)
	}
	if !strings.Contains(res.AttestSkip, "2.93.0") {
		t.Fatalf("want CVE-gated skip reason, got %q", res.AttestSkip)
	}
	if verified {
		t.Fatalf("the token-leaking verify must NOT be invoked on old gh")
	}
}

func TestApply_RequireMode_VerifierAbsent_Fails(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:]))
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, false, "", true, "", 0, nil)
	_, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyRequire))
	if err == nil || !strings.Contains(err.Error(), "require") {
		t.Fatalf("require mode must fail when no verifier exists, got %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD\n" {
		t.Fatalf("nothing should install in require mode without a verifier")
	}
}

func TestApply_ApiUnreachable_SoftSkip(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v1.0.0", asset, body, hex.EncodeToString(sum[:])) // signed version
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, true, "2.93.0", true, "Get \"https://api.github.com\": dial tcp: lookup api.github.com: no such host", 1, nil)
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyAuto))
	if err != nil {
		t.Fatalf("unreachable API must soft-skip even on a signed version: %v", err)
	}
	if !strings.Contains(res.AttestSkip, "unreachable") {
		t.Fatalf("want unreachable skip, got %q", res.AttestSkip)
	}
}

func TestApply_PinnedOldVersion_NoAttestation_SoftSkip(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("OLD-RELEASE\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.9.0", asset, body, hex.EncodeToString(sum[:])) // below SignedSince
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, true, "2.93.0", true, "no attestation found for subject digest sha256:abc", 1, nil)
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.8.0", "v0.9.0", VerifyAuto))
	if err != nil {
		t.Fatalf("pre-signing pinned version must soft-skip: %v", err)
	}
	if !strings.Contains(res.AttestSkip, "pre-signing") {
		t.Fatalf("want pre-signing skip, got %q", res.AttestSkip)
	}
}

func TestApply_MalformedPinnedTag_FailsClosed(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("X\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11", asset, body, hex.EncodeToString(sum[:])) // version-shaped but malformed
	exe := tempExe(t, "OLD\n")
	setFakeGh(t, true, "2.93.0", true, "no attestation found for subject digest sha256:def", 1, nil)
	_, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "v0.11", VerifyAuto))
	if err == nil || !strings.Contains(err.Error(), "malformed release tag") {
		t.Fatalf("malformed tag must fail closed, got %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD\n" {
		t.Fatalf("binary replaced despite a malformed-tag fail-closed")
	}
}

func TestApply_OffMode_SkipsVerifierEntirely(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("NEW\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v0.11.0", asset, body, hex.EncodeToString(sum[:]))
	exe := tempExe(t, "OLD\n")
	var verified bool
	setFakeGh(t, true, "2.93.0", true, "", 1, &verified) // would fail if called
	res, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "", VerifyOff))
	if err != nil {
		t.Fatalf("off mode must install on SHA256 alone: %v", err)
	}
	if verified {
		t.Fatalf("off mode must not invoke gh attestation verify")
	}
	if !strings.Contains(res.AttestSkip, "disabled") {
		t.Fatalf("want off-mode skip reason, got %q", res.AttestSkip)
	}
}

// gh's raw output (which can carry private URLs / a credential a misbehaving gh
// echoes) must NEVER be surfaced in the fail-closed error — we classify on it
// internally but emit only a sanitized reason (north-star + AGENTS.md). (Codex PR #45.)
func TestApply_DoesNotSurfaceGhOutputInError(t *testing.T) {
	asset := mustAsset(t)
	body := []byte("X\n")
	sum := sha256.Sum256(body)
	srv := releaseServer(t, "v1.0.0", asset, body, hex.EncodeToString(sum[:])) // signed -> fail-closed
	exe := tempExe(t, "OLD\n")
	// Build the credential from parts so this test's own fixture does not trip
	// secret-scan, while remaining Bearer-shaped for safeerr.Redact to catch.
	secretVal := "FAKE" + "TOKEN" + "abc123def456ghi"
	setFakeGh(t, true, "2.93.0", true, "verify failed; Bearer "+secretVal, 1, nil)
	_, err := Apply(context.Background(), newOpts(srv.URL, exe, "0.10.0", "v1.0.0", VerifyAuto))
	if err == nil {
		t.Fatalf("expected a fail-closed error")
	}
	if strings.Contains(err.Error(), secretVal) {
		t.Fatalf("gh output token was NOT redacted in the surfaced error: %v", err)
	}
}

func TestInstallBinary_ReplacesAtomically(t *testing.T) {
	exe := tempExe(t, "ORIGINAL\n")
	if err := installBinary([]byte("REPLACED\n"), exe); err != nil {
		t.Fatalf("installBinary: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "REPLACED\n" {
		t.Fatalf("not replaced: %q", string(got))
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("replaced binary not executable")
	}
	// No staging leftovers.
	entries, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".nole.update") {
			t.Fatalf("leftover staging file: %s", e.Name())
		}
	}
}

// The load-bearing invariant: a failure to stage the new binary must leave the
// existing one byte-for-byte intact. Force the stage write to fail by making the
// install directory unwritable.
func TestInstallBinary_PreservesOriginalOnStageFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	exe := tempExe(t, "ORIGINAL-GOOD\n")
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := installBinary([]byte("NEW\n"), exe)
	if err == nil {
		t.Fatalf("installBinary must fail when staging cannot be written")
	}
	if got, _ := os.ReadFile(exe); string(got) != "ORIGINAL-GOOD\n" {
		t.Fatalf("original binary was disturbed by a failed install: %q", string(got))
	}
}

func TestParseVerifyMode(t *testing.T) {
	for _, ok := range []string{"", "auto", "require", "off"} {
		if _, err := ParseVerifyMode(ok); err != nil {
			t.Errorf("ParseVerifyMode(%q) unexpected err: %v", ok, err)
		}
	}
	for _, bad := range []string{"required", "REQUIRE", "Off", "yes", "1"} {
		if _, err := ParseVerifyMode(bad); err == nil {
			t.Errorf("ParseVerifyMode(%q) should reject", bad)
		}
	}
}

func TestVerifyChecksumTable(t *testing.T) {
	body := []byte("hello")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	if err := verifyChecksum(body, []byte(good+"  nole-x\n"), "nole-x"); err != nil {
		t.Errorf("valid checksum should pass: %v", err)
	}
	if err := verifyChecksum(body, []byte(strings.Repeat("0", 64)+"  nole-x\n"), "nole-x"); err == nil {
		t.Errorf("wrong checksum must fail")
	}
	if err := verifyChecksum(body, []byte(good+"  other\n"), "nole-x"); err == nil {
		t.Errorf("missing entry must fail")
	}
}

func TestGhHostFromAPIBase(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"https://api.github.com":              "",
		"https://api.github.com/":             "",
		"https://github.com":                  "",
		"https://ghe.corp/api/v3":             "ghe.corp",
		"https://git.example.com:8443/api/v3": "git.example.com:8443",
		"https://api.github.com.evil.com":     "api.github.com.evil.com",
	}
	for in, want := range cases {
		if got := ghHostFromAPIBase(in); got != want {
			t.Errorf("ghHostFromAPIBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifierHelpers(t *testing.T) {
	if !isCleanRelease("v0.11.0") || isCleanRelease("v0.11") || isCleanRelease("dev") {
		t.Errorf("isCleanRelease misclassified")
	}
	if !looksLikeReleaseTag("v0.11") || !looksLikeReleaseTag("0.11") || looksLikeReleaseTag("dev") {
		t.Errorf("looksLikeReleaseTag misclassified")
	}
	if !releaseAtLeast("v0.11.0", "v0.10.0") || releaseAtLeast("v0.9.0", "v0.10.0") || !releaseAtLeast("v0.10.0", "v0.10.0") {
		t.Errorf("releaseAtLeast wrong")
	}
	if !isUnreachable("Get x: dial tcp: lookup api.github.com: no such host") || isUnreachable("no attestation found") {
		t.Errorf("isUnreachable misclassified")
	}
}
