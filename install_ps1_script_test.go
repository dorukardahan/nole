package main

// Functional tests for scripts/install.ps1 — the PowerShell installer's first
// real (not parse-only) coverage. They shell out to `pwsh -File scripts/install.ps1`
// against a fake release server + injected fake `gh`, mirroring the install.sh
// suite (install_script_test.go) case-for-case, and SKIP cleanly when pwsh is
// absent (so the local macOS gate stays green; CI ubuntu ships pwsh 7).
//
// They reuse the install.sh harness helpers from install_script_test.go in this
// same `package main`: haveAny, fakeReleaseServer, fakeGhScript, withFakeGh. The
// ps1-specific shape differences are the asset name (nole-windows-<arch>.exe), the
// installed file (nole.exe), and the pwsh invocation. NOLE_INSTALL_DIR redirects
// every write into a temp dir (also dodging the Windows-only %LOCALAPPDATA%).

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skipUnlessPwsh(t *testing.T) {
	t.Helper()
	if !haveAny("pwsh") {
		t.Skip("pwsh not available — install.ps1 functional test runs on CI ubuntu (pwsh 7 preinstalled); skipped here")
	}
}

// ps1AssetName mirrors install.ps1's `nole-windows-$arch.exe`. Get-Arch reads
// RuntimeInformation.OSArchitecture, which reports the HOST arch even under
// pwsh-on-Linux, so the asset is nole-windows-<hostarch>.exe regardless of OS.
func ps1AssetName(t *testing.T) (string, bool) {
	t.Helper()
	switch runtime.GOARCH {
	case "amd64":
		return "nole-windows-amd64.exe", true
	case "arm64":
		return "nole-windows-arm64.exe", true
	default:
		return "", false
	}
}

// runPs1InstallerEnv runs install.ps1 under pwsh against the fake server with the
// standard override env, plus extraEnv appended last (a later duplicate key wins).
func runPs1InstallerEnv(t *testing.T, srvURL, repo, installDir string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-File", "scripts/install.ps1")
	env := append(os.Environ(),
		"NOLE_INSTALL_REPO="+repo,
		"NOLE_INSTALL_API_URL="+srvURL,
		"NOLE_INSTALL_DOWNLOAD_URL="+srvURL,
		"NOLE_INSTALL_DIR="+installDir, // redirect writes to temp; also avoids %LOCALAPPDATA%
		"NOLE_INSTALL_VERSION=",        // exercise latest-tag resolution
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runPs1Installer is the SHA256-focused harness: it disables the attestation gate
// (VERIFY=off) so the checksum tests never invoke `gh` or the network.
func runPs1Installer(t *testing.T, srvURL, repo, installDir string) (string, error) {
	t.Helper()
	return runPs1InstallerEnv(t, srvURL, repo, installDir, "NOLE_INSTALL_VERIFY=off")
}

// validSumServerPs1: fake release server whose SHA256SUMS matches body for the
// windows .exe asset (so the mandatory floor passes and the test isolates the
// additive attestation gate).
func validSumServerPs1(t *testing.T, tag string, body []byte) (*httptest.Server, string) {
	t.Helper()
	asset, ok := ps1AssetName(t)
	if !ok {
		t.Skipf("install.ps1 does not target arch %s", runtime.GOARCH)
	}
	sum := sha256.Sum256(body)
	srv := fakeReleaseServer("testowner/testrepo", tag, asset, body, hex.EncodeToString(sum[:]))
	return srv, asset
}

func TestInstallPs1VerifiesAndInstalls(t *testing.T) {
	skipUnlessPwsh(t)
	asset, ok := ps1AssetName(t)
	if !ok {
		t.Skipf("install.ps1 does not target arch %s", runtime.GOARCH)
	}

	repo, tag := "testowner/testrepo", "v0.9.0"
	body := []byte("FAKE-NOLE-EXE-payload-for-install-test\n")
	sum := sha256.Sum256(body)
	srv := fakeReleaseServer(repo, tag, asset, body, hex.EncodeToString(sum[:]))
	defer srv.Close()

	installDir := t.TempDir()
	// Pre-existing binary: the install must atomically replace it (not error).
	if err := os.WriteFile(filepath.Join(installDir, "nole.exe"), []byte("OLD-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runPs1Installer(t, srv.URL, repo, installDir)
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(installDir, "nole.exe"))
	if err != nil {
		t.Fatalf("binary not installed: %v\n%s", err, out)
	}
	if string(got) != string(body) {
		t.Fatalf("installed content mismatch")
	}
	if !strings.Contains(out, "checksum verified") || !strings.Contains(out, "ZERO keys") {
		t.Fatalf("expected verification + keyless message in output:\n%s", out)
	}
}

func TestInstallPs1FailsClosedOnChecksumMismatch(t *testing.T) {
	skipUnlessPwsh(t)
	asset, ok := ps1AssetName(t)
	if !ok {
		t.Skipf("install.ps1 does not target arch %s", runtime.GOARCH)
	}

	repo, tag := "testowner/testrepo", "v0.9.0"
	body := []byte("REAL-payload\n")
	// Wrong checksum (64 zeros) -> verification must fail and nothing installs.
	srv := fakeReleaseServer(repo, tag, asset, body, strings.Repeat("0", 64))
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1Installer(t, srv.URL, repo, installDir)
	if err == nil {
		t.Fatalf("installer must fail closed on checksum mismatch; output:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); !os.IsNotExist(statErr) {
		t.Fatalf("a binary was installed despite a checksum mismatch (statErr=%v)", statErr)
	}
}

// A post-checksum failure (unwritable install dir) must NOT destroy an existing
// binary — stage+atomic-rename replaces the old binary only when the new one is
// fully staged.
func TestInstallPs1PreservesExistingBinaryOnPostChecksumFailure(t *testing.T) {
	skipUnlessPwsh(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permissions")
	}
	body := []byte("NEW-payload\n")
	srv, _ := validSumServerPs1(t, "v0.9.0", body) // VALID checksum
	defer srv.Close()

	installDir := t.TempDir()
	exe := filepath.Join(installDir, "nole.exe")
	if err := os.WriteFile(exe, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Unwritable install dir so staging the new binary fails AFTER the checksum
	// verifies. Restore perms so t.TempDir cleanup can remove it.
	if err := os.Chmod(installDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(installDir, 0o755) })

	out, err := runPs1Installer(t, srv.URL, "testowner/testrepo", installDir)
	if err == nil {
		t.Fatalf("installer should fail when the install dir is unwritable; output:\n%s", out)
	}
	got, readErr := os.ReadFile(exe)
	if readErr != nil {
		t.Fatalf("existing binary was destroyed by a failed install: %v", readErr)
	}
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary was modified by a failed install: %q", string(got))
	}
}

// --- additive attestation gate (GitHub build provenance), three-way taxonomy ---

func TestInstallPs1_AttestationValid_Installs(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("SIGNED-binary-payload\n")
	srv, asset := validSumServerPs1(t, "v0.10.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=0",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=sigstore.dev: Verification succeeded for "+asset,
	)
	if err != nil {
		t.Fatalf("install with a valid attestation must succeed: %v\n%s", err, out)
	}
	got, readErr := os.ReadFile(filepath.Join(installDir, "nole.exe"))
	if readErr != nil || string(got) != string(body) {
		t.Fatalf("binary not installed correctly (err=%v):\n%s", readErr, out)
	}
	if !strings.Contains(out, "checksum verified") || !strings.Contains(out, "attestation verified") {
		t.Fatalf("expected checksum + attestation verification in output:\n%s", out)
	}
}

func TestInstallPs1_AttestationMismatch_FailsClosed(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("TAMPERED-binary\n")
	srv, _ := validSumServerPs1(t, "v0.10.0", body) // SHA256 (floor) PASSES
	defer srv.Close()

	installDir := t.TempDir()
	exe := filepath.Join(installDir, "nole.exe")
	if err := os.WriteFile(exe, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
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
	got, _ := os.ReadFile(exe)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary was replaced despite a failed attestation: %q", string(got))
	}
}

func TestInstallPs1_VerifierTooOldCVE_SoftSkip(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("payload-old-gh\n")
	srv, _ := validSumServerPs1(t, "v0.9.0", body)
	defer srv.Close()

	sentinel := filepath.Join(t.TempDir(), "verify-was-called")
	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.89.0", // pre-CVE-2026-48501 fix
		"NOLE_FAKE_GH_SENTINEL="+sentinel,
		"NOLE_FAKE_GH_VERIFY_EXIT=1", // would fail if ever called
	)
	if err != nil {
		t.Fatalf("an old (CVE-vulnerable) gh must soft-skip, not fail: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "skipping attestation check") || !strings.Contains(out, "2.93.0") {
		t.Fatalf("expected a CVE-gated soft-skip mentioning the minimum version:\n%s", out)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("the token-leaking `gh attestation verify` must NOT be invoked on old gh (sentinel exists)")
	}
}

func TestInstallPs1_SubcommandAbsent_SoftSkip(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("payload-no-subcmd\n")
	srv, _ := validSumServerPs1(t, "v0.9.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_HELP_EXIT=1", // gh present but lacks `attestation verify`
	)
	if err != nil {
		t.Fatalf("a gh without the attestation subcommand must soft-skip: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "skipping attestation check") {
		t.Fatalf("expected a soft-skip for a gh lacking the subcommand:\n%s", out)
	}
}

func TestInstallPs1_NoAttestationOldVersion_SoftSkip(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("pre-signing-release-binary\n")
	srv, _ := validSumServerPs1(t, "v0.9.0", body) // BELOW SignedSince
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=no attestation found for subject digest sha256:abc",
	)
	if err != nil {
		t.Fatalf("a pre-signing release with no attestation must soft-skip: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "pre-signing release") {
		t.Fatalf("expected the pre-signing soft-skip message:\n%s", out)
	}
}

func TestInstallPs1_NoAttestationSignedVersion_FailsClosed(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("signed-version-but-attestation-stripped\n")
	srv, _ := validSumServerPs1(t, "v1.0.0", body) // AT/ABOVE SignedSince
	defer srv.Close()

	installDir := t.TempDir()
	exe := filepath.Join(installDir, "nole.exe")
	if err := os.WriteFile(exe, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
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
	got, _ := os.ReadFile(exe)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary replaced despite a missing attestation on a signed version: %q", string(got))
	}
}

// On the unpinned path the resolved tag comes from the (MITM-controllable)
// releases API. A version-SHAPED but malformed tag like "v0.10" must fail closed,
// not be treated as pre-signing and soft-skipped.
func TestInstallPs1_MalformedReleaseTag_FailsClosed(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("malformed-tag-binary\n")
	srv, _ := validSumServerPs1(t, "v0.10", body) // shaped like a tag, but 2 segments
	defer srv.Close()

	installDir := t.TempDir()
	exe := filepath.Join(installDir, "nole.exe")
	if err := os.WriteFile(exe, []byte("EXISTING-good-binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
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
	got, _ := os.ReadFile(exe)
	if string(got) != "EXISTING-good-binary\n" {
		t.Fatalf("existing binary replaced despite a malformed tag with no attestation: %q", string(got))
	}
}

func TestInstallPs1_InvalidVerifyMode_Fails(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("invalid-verify-mode-binary\n")
	srv, _ := validSumServerPs1(t, "v0.10.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		"NOLE_INSTALL_VERIFY=required", // common typo for `require`
	)
	if err == nil {
		t.Fatalf("an unknown NOLE_INSTALL_VERIFY must fail early; output:\n%s", out)
	}
	if !strings.Contains(out, "invalid NOLE_INSTALL_VERIFY") {
		t.Fatalf("expected an explicit invalid-mode error:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); !os.IsNotExist(statErr) {
		t.Fatalf("nothing should install when the verify mode is invalid")
	}
}

func TestInstallPs1_RequireMode_VerifierUnusable_Fails(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("require-mode-binary\n")
	srv, _ := validSumServerPs1(t, "v0.10.0", body)
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
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
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); !os.IsNotExist(statErr) {
		t.Fatalf("nothing should have installed in require mode with no verifier")
	}
}

func TestInstallPs1_OffMode_SkipsEvenWhenGhPresent(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("off-mode-binary\n")
	srv, _ := validSumServerPs1(t, "v0.10.0", body)
	defer srv.Close()

	sentinel := filepath.Join(t.TempDir(), "verify-was-called")
	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=off",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_SENTINEL="+sentinel,
		"NOLE_FAKE_GH_VERIFY_EXIT=1", // would fail closed if ever called
	)
	if err != nil {
		t.Fatalf("off mode must install on SHA256 alone: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); statErr != nil {
		t.Fatalf("binary should have installed:\n%s", out)
	}
	if !strings.Contains(out, "attestation check disabled") {
		t.Fatalf("expected the off-mode message:\n%s", out)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("off mode must NOT invoke `gh attestation verify` (sentinel exists)")
	}
}

func TestInstallPs1_ApiUnreachable_SoftSkipEvenOnSignedVersion(t *testing.T) {
	skipUnlessPwsh(t)
	body := []byte("offline-install-binary\n")
	srv, _ := validSumServerPs1(t, "v1.0.0", body) // signed version, but API can't be reached
	defer srv.Close()

	installDir := t.TempDir()
	out, err := runPs1InstallerEnv(t, srv.URL, "testowner/testrepo", installDir,
		withFakeGh(t),
		"NOLE_INSTALL_VERIFY=auto",
		"NOLE_FAKE_GH_VERSION=2.93.0",
		"NOLE_FAKE_GH_VERIFY_EXIT=1",
		"NOLE_FAKE_GH_VERIFY_OUTPUT=Get \"https://api.github.com\": dial tcp: lookup api.github.com: no such host",
	)
	if err != nil {
		t.Fatalf("an unreachable attestation API must soft-skip (can't-verify != tampering): %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "nole.exe")); statErr != nil {
		t.Fatalf("binary should have installed via SHA256 alone:\n%s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Fatalf("expected an API-unreachable soft-skip message:\n%s", out)
	}
}
