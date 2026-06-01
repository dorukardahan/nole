package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assetName mirrors scripts/install.sh's uname-based OS/arch -> asset mapping for
// the host the test runs on. Returns ok=false on platforms the bash installer
// does not target.
func assetName(t *testing.T) (string, bool) {
	t.Helper()
	var os, arch string
	switch runtime.GOOS {
	case "linux":
		os = "linux"
	case "darwin":
		os = "darwin"
	default:
		return "", false
	}
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", false
	}
	return fmt.Sprintf("nole-%s-%s", os, arch), true
}

func haveAny(names ...string) bool {
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return true
		}
	}
	return false
}

// fakeReleaseServer serves the latest-tag JSON, the asset bytes, and a
// SHA256SUMS whose hash is `sumHex` (caller controls it to force match/mismatch).
func fakeReleaseServer(repo, tag, asset string, body []byte, sumHex string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/"+repo+"/releases/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/"+repo+"/releases/download/"+tag+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		// Two-space separator + bare asset name, matching sha256sum/shasum output.
		_, _ = fmt.Fprintf(w, "%s  %s\n", sumHex, asset)
	})
	return httptest.NewServer(mux)
}

// runInstallerEnv runs install.sh against the fake server with the standard
// override env, plus any extraEnv appended last (so a later duplicate key wins).
func runInstallerEnv(t *testing.T, srvURL, repo, installDir string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "scripts/install.sh")
	env := append(os.Environ(),
		"NOLE_INSTALL_REPO="+repo,
		"NOLE_INSTALL_API_URL="+srvURL,
		"NOLE_INSTALL_DOWNLOAD_URL="+srvURL,
		"NOLE_INSTALL_DIR="+installDir,
		"NOLE_INSTALL_VERSION=", // exercise latest-tag resolution
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runInstaller is the SHA256-focused harness used by the checksum tests. It
// disables the additive attestation gate (NOLE_INSTALL_VERIFY=off) so those
// tests never invoke the host's real `gh` or touch the network — they assert the
// mandatory checksum floor in isolation. Attestation behavior has its own tests
// below that inject a fake `gh`.
func runInstaller(t *testing.T, srvURL, repo, installDir string) (string, error) {
	t.Helper()
	return runInstallerEnv(t, srvURL, repo, installDir, "NOLE_INSTALL_VERIFY=off")
}

// fakeGhScript is a stand-in for the GitHub CLI. install.sh's attest_verify probes
// `gh --version` and `gh attestation verify --help`, then runs
// `gh attestation verify <file> --repo ... --signer-workflow ...`. This fake is
// fully driven by env vars so each test controls the outcome WITHOUT the real gh
// or any network:
//
//	NOLE_FAKE_GH_VERSION        version string reported by `gh --version`
//	NOLE_FAKE_GH_HELP_EXIT      exit code of `attestation verify --help`
//	NOLE_FAKE_GH_VERIFY_EXIT    exit code of the real `attestation verify <file>`
//	NOLE_FAKE_GH_VERIFY_OUTPUT  stdout/stderr text of the real verify (classified by install.sh)
//	NOLE_FAKE_GH_SENTINEL       if set, the fake touches this path ONLY when the
//	                            real verify (not --help) is invoked — lets a test
//	                            assert the verify was/ was not actually called.
const fakeGhScript = `#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  echo "gh version ${NOLE_FAKE_GH_VERSION:-2.93.0} (2026-04-01)"
  echo "https://github.com/cli/cli/releases/latest"
  exit 0
fi
if [ "${1:-}" = "attestation" ] && [ "${2:-}" = "verify" ]; then
  if [ "${3:-}" = "--help" ]; then
    exit "${NOLE_FAKE_GH_HELP_EXIT:-0}"
  fi
  if [ -n "${NOLE_FAKE_GH_SENTINEL:-}" ]; then : > "$NOLE_FAKE_GH_SENTINEL"; fi
  printf '%s\n' "${NOLE_FAKE_GH_VERIFY_OUTPUT:-sigstore.dev: Verification succeeded}"
  exit "${NOLE_FAKE_GH_VERIFY_EXIT:-0}"
fi
exit 0
`

// withFakeGh writes the fake gh into a fresh dir and returns a PATH= env entry
// that prepends that dir (shadowing any real gh) while keeping the rest of PATH
// (so curl/wget/sha256sum/shasum still resolve).
func withFakeGh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte(fakeGhScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// validSumServer is the common setup for the attestation tests: a fake release
// server for `tag` whose SHA256SUMS matches `body` (so the mandatory floor always
// passes and the test exercises ONLY the additive attestation gate).
func validSumServer(t *testing.T, tag string, body []byte) (*httptest.Server, string) {
	t.Helper()
	asset, ok := assetName(t)
	if !ok {
		t.Skipf("installer does not target %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	sum := sha256.Sum256(body)
	srv := fakeReleaseServer("testowner/testrepo", tag, asset, body, hex.EncodeToString(sum[:]))
	return srv, asset
}

func skipUnlessInstallerToolsPresent(t *testing.T) {
	t.Helper()
	if !haveAny("bash") {
		t.Skip("bash not available")
	}
	if !haveAny("curl", "wget") || !haveAny("sha256sum", "shasum") {
		t.Skip("installer needs curl/wget and sha256sum/shasum")
	}
}

func TestInstallScriptVerifiesAndInstalls(t *testing.T) {
	if !haveAny("bash") {
		t.Skip("bash not available")
	}
	if !haveAny("curl", "wget") || !haveAny("sha256sum", "shasum") {
		t.Skip("installer needs curl/wget and sha256sum/shasum")
	}
	asset, ok := assetName(t)
	if !ok {
		t.Skipf("installer does not target %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	repo, tag := "testowner/testrepo", "v0.9.0"
	body := []byte("FAKE-NOLE-BINARY-payload-for-install-test\n")
	sum := sha256.Sum256(body)
	srv := fakeReleaseServer(repo, tag, asset, body, hex.EncodeToString(sum[:]))
	defer srv.Close()

	installDir := t.TempDir()
	// Pre-existing binary: the install must atomically replace it (not error).
	if err := os.WriteFile(filepath.Join(installDir, "nole"), []byte("OLD-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstaller(t, srv.URL, repo, installDir)
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(installDir, "nole"))
	if err != nil {
		t.Fatalf("binary not installed: %v\n%s", err, out)
	}
	if string(got) != string(body) {
		t.Fatalf("installed content mismatch")
	}
	info, _ := os.Stat(filepath.Join(installDir, "nole"))
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %v", info.Mode())
	}
	if !strings.Contains(out, "checksum verified") || !strings.Contains(out, "ZERO keys") {
		t.Fatalf("expected verification + keyless message in output:\n%s", out)
	}
}

func TestInstallScriptFailsClosedOnChecksumMismatch(t *testing.T) {
	if !haveAny("bash") {
		t.Skip("bash not available")
	}
	if !haveAny("curl", "wget") || !haveAny("sha256sum", "shasum") {
		t.Skip("installer needs curl/wget and sha256sum/shasum")
	}
	asset, ok := assetName(t)
	if !ok {
		t.Skipf("installer does not target %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	repo, tag := "testowner/testrepo", "v0.9.0"
	body := []byte("REAL-payload\n")
	// Wrong checksum (64 zeros) -> verification must fail and nothing installs.
	srv := fakeReleaseServer(repo, tag, asset, body, strings.Repeat("0", 64))
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstaller(t, srv.URL, repo, installDir)
	if err == nil {
		t.Fatalf("installer must fail closed on checksum mismatch; output:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); !os.IsNotExist(statErr) {
		t.Fatalf("a binary was installed despite a checksum mismatch (statErr=%v)", statErr)
	}
}

// An install failure AFTER the checksum passes (here: an unwritable install dir)
// must NOT destroy an existing binary — the stage+atomic-rename design replaces
// the old binary only when the new one is fully in place. (Codex review PR #42.)
func TestInstallScriptPreservesExistingBinaryOnPostChecksumFailure(t *testing.T) {
	if !haveAny("bash") {
		t.Skip("bash not available")
	}
	if !haveAny("curl", "wget") || !haveAny("sha256sum", "shasum") {
		t.Skip("installer needs curl/wget and sha256sum/shasum")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	asset, ok := assetName(t)
	if !ok {
		t.Skipf("installer does not target %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	repo, tag := "testowner/testrepo", "v0.9.0"
	body := []byte("NEW-payload\n")
	sum := sha256.Sum256(body)
	srv := fakeReleaseServer(repo, tag, asset, body, hex.EncodeToString(sum[:])) // VALID checksum
	defer srv.Close()

	installDir := t.TempDir()
	nolefile := filepath.Join(installDir, "nole")
	if err := os.WriteFile(nolefile, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the install dir unwritable so staging the new binary fails AFTER the
	// checksum verifies. Restore perms so t.TempDir cleanup can remove it.
	if err := os.Chmod(installDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(installDir, 0o755) })

	out, err := runInstaller(t, srv.URL, repo, installDir)
	if err == nil {
		t.Fatalf("installer should fail when the install dir is unwritable; output:\n%s", out)
	}
	got, readErr := os.ReadFile(nolefile)
	if readErr != nil {
		t.Fatalf("existing binary was destroyed by a failed install: %v", readErr)
	}
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary was modified by a failed install: %q", string(got))
	}
}

// --- additive attestation gate (GitHub build provenance) ---
//
// These tests inject a fake `gh` (withFakeGh) so they exercise install.sh's
// three-way fail taxonomy without the real gh or any network:
//   - verifier unusable / off              -> soft skip (SHA256 already passed)
//   - attestation invalid, or provably absent on a KNOWN-signed version -> fail closed
//   - API unreachable / anonymous / pre-signing release -> soft skip
// The mandatory SHA256 floor is held constant (valid) so each test isolates the
// optional gate.

func TestInstall_AttestationValid_Installs(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("SIGNED-binary-payload\n")
	srv, asset := validSumServer(t, "v0.10.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=0",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=sigstore.dev: Verification succeeded for "+asset,
	)
	if err != nil {
		t.Fatalf("install with a valid attestation must succeed: %v\n%s", err, out)
	}
	got, readErr := os.ReadFile(filepath.Join(installDir, "nole"))
	if readErr != nil || string(got) != string(body) {
		t.Fatalf("binary not installed correctly (err=%v):\n%s", readErr, out)
	}
	if !strings.Contains(out, "checksum verified") || !strings.Contains(out, "attestation verified") {
		t.Fatalf("expected checksum + attestation verification in output:\n%s", out)
	}
}

func TestInstall_AttestationMismatch_FailsClosed(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	body := []byte("TAMPERED-binary\n")
	srv, _ := validSumServer(t, "v0.10.0", body) // SHA256 (floor) PASSES
	defer srv.Close()

	installDir := t.TempDir()
	nolefile := filepath.Join(installDir, "nole")
	if err := os.WriteFile(nolefile, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=failed to verify certificate identity: signer mismatch",
	)
	if err == nil {
		t.Fatalf("a cryptographic attestation FAILURE must fail closed; output:\n%s", out)
	}
	if !strings.Contains(out, "attestation verification FAILED") {
		t.Fatalf("expected fail-closed message:\n%s", out)
	}
	// Fail-closed runs before staging, so the existing binary is untouched.
	got, _ := os.ReadFile(nolefile)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary was replaced despite a failed attestation: %q", string(got))
	}
}

func TestInstall_VerifierTooOldCVE_SoftSkip(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("payload-old-gh\n")
	srv, _ := validSumServer(t, "v0.9.0", body)
	defer srv.Close()

	sentinel := filepath.Join(t.TempDir(), "verify-was-called")
	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.89.0", // pre-CVE-2026-48501 fix
		"NOLE_FAKE_GH_SENTINEL="+sentinel,
		"NOLE_FAKE_GH_VERIFY_EXIT=1", // would fail if ever called
	)
	if err != nil {
		t.Fatalf("an old (CVE-vulnerable) gh must soft-skip, not fail: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "skipping attestation check") || !strings.Contains(out, "2.93.0") {
		t.Fatalf("expected a CVE-gated soft-skip mentioning the minimum version:\n%s", out)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("the token-leaking `gh attestation verify` must NOT be invoked on old gh (sentinel exists)")
	}
}

func TestInstall_SubcommandAbsent_SoftSkip(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("payload-no-subcmd\n")
	srv, _ := validSumServer(t, "v0.9.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_HELP_EXIT=1", // gh present but lacks `attestation verify`
	)
	if err != nil {
		t.Fatalf("a gh without the attestation subcommand must soft-skip: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "skipping attestation check") {
		t.Fatalf("expected a soft-skip for a gh lacking the subcommand:\n%s", out)
	}
}

func TestInstall_NoAttestationOldVersion_SoftSkip(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("pre-signing-release-binary\n")
	srv, _ := validSumServer(t, "v0.9.0", body) // BELOW SIGNED_SINCE
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=no attestation found for subject digest sha256:abc",
	)
	if err != nil {
		t.Fatalf("a pre-signing release with no attestation must soft-skip: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "pre-signing release") {
		t.Fatalf("expected the pre-signing soft-skip message:\n%s", out)
	}
}

func TestInstall_NoAttestationSignedVersion_FailsClosed(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	body := []byte("signed-version-but-attestation-stripped\n")
	srv, _ := validSumServer(t, "v1.0.0", body) // AT/ABOVE SIGNED_SINCE
	defer srv.Close()

	installDir := t.TempDir()
	nolefile := filepath.Join(installDir, "nole")
	if err := os.WriteFile(nolefile, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=no attestation found for subject digest sha256:def",
	)
	if err == nil {
		t.Fatalf("a KNOWN-signed version with a missing attestation must fail closed (downgrade defense); output:\n%s", out)
	}
	if !strings.Contains(out, "attestation verification FAILED") {
		t.Fatalf("expected a tampering fail-closed message:\n%s", out)
	}
	got, _ := os.ReadFile(nolefile)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary replaced despite a missing attestation on a signed version: %q", string(got))
	}
}

// Regression for the diff-review downgrade dodge: on the unpinned path the
// resolved tag comes from the (attacker-controllable, under MITM) releases API.
// A version-SHAPED but malformed tag like "v0.10" must NOT be treated as a
// pre-signing release and soft-skipped — it must fail closed, because we cannot
// confirm it predates the SIGNED_SINCE cutover.
func TestInstall_MalformedReleaseTag_FailsClosed(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	body := []byte("malformed-tag-binary\n")
	srv, _ := validSumServer(t, "v0.10", body) // SHAPED like a release tag, but only 2 segments
	defer srv.Close()

	installDir := t.TempDir()
	nolefile := filepath.Join(installDir, "nole")
	if err := os.WriteFile(nolefile, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=no attestation found for subject digest sha256:abc",
	)
	if err == nil {
		t.Fatalf("a malformed-but-version-shaped tag must fail closed, not soft-skip; output:\n%s", out)
	}
	if !strings.Contains(out, "malformed release tag") {
		t.Fatalf("expected the malformed-tag fail-closed message:\n%s", out)
	}
	got, _ := os.ReadFile(nolefile)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary replaced despite a malformed tag with no attestation: %q", string(got))
	}
}

// A genuine non-release ref (not version-shaped) still soft-skips, so an install
// pinned to a branch/dev ref is not bricked by the malformed-tag guard above.
func TestInstall_NonReleaseRef_SoftSkip(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("nightly-ref-binary\n")
	srv, _ := validSumServer(t, "nightly", body) // not version-shaped
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=no attestation found for subject digest sha256:xyz",
	)
	if err != nil {
		t.Fatalf("a non-release ref with no attestation must soft-skip: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "pre-signing release") {
		t.Fatalf("expected a soft-skip for a non-release ref:\n%s", out)
	}
}

func TestInstall_ApiUnreachable_SoftSkipEvenOnSignedVersion(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("offline-install-binary\n")
	srv, _ := validSumServer(t, "v1.0.0", body) // signed version, but API can't be reached
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=Get \"https://api.github.com\": dial tcp: lookup api.github.com: no such host",
	)
	if err != nil {
		t.Fatalf("an unreachable attestation API must soft-skip (can't-verify != tampering): %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Fatalf("expected an API-unreachable soft-skip message:\n%s", out)
	}
}

func TestInstall_RequireMode_VerifierUnusable_Fails(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	body := []byte("require-mode-binary\n")
	srv, _ := validSumServer(t, "v0.10.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=require",
		"NOLE_FAKE_GH_VERSION=2.89.0", // unusable (CVE) -> require turns soft-skip into hard error
	)
	if err == nil {
		t.Fatalf("require mode must fail when no usable verifier exists; output:\n%s", out)
	}
	if !strings.Contains(out, "require") {
		t.Fatalf("expected a require-mode error message:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); !os.IsNotExist(statErr) {
		t.Fatalf("nothing should have installed in require mode with no verifier")
	}
}

func TestInstall_OffMode_SkipsEvenWhenGhPresent(t *testing.T) {
	skipUnlessInstallerToolsPresent(t)
	body := []byte("off-mode-binary\n")
	srv, _ := validSumServer(t, "v0.10.0", body)
	defer srv.Close()

	sentinel := filepath.Join(t.TempDir(), "verify-was-called")
	installDir := t.TempDir()
	out, err := runInstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=off",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_SENTINEL="+sentinel,
		"NOLE_FAKE_GH_VERIFY_EXIT=1", // would fail closed if ever called
	)
	if err != nil {
		t.Fatalf("off mode must install on SHA256 alone: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole")); statErr != nil {
		t.Fatalf("binary should have installed:\n%s", out)
	}
	if !strings.Contains(out, "attestation check disabled") {
		t.Fatalf("expected the off-mode message:\n%s", out)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("off mode must NOT invoke `gh attestation verify` (sentinel exists)")
	}
}
