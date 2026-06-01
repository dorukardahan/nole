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

func runInstaller(t *testing.T, srvURL, repo, installDir string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "scripts/install.sh")
	cmd.Env = append(os.Environ(),
		"NOLE_INSTALL_REPO="+repo,
		"NOLE_INSTALL_API_URL="+srvURL,
		"NOLE_INSTALL_DOWNLOAD_URL="+srvURL,
		"NOLE_INSTALL_DIR="+installDir,
		"NOLE_INSTALL_VERSION=", // exercise latest-tag resolution
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
