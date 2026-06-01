package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dorukardahan/nole/internal/version"
)

func runDoctorWith(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	t.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"doctor"}, args...))
	err := root.Execute()
	return out.String(), err
}

// Offline / unreachable releases endpoint: --check-updates must NOT fail doctor
// and must print NO update line (fail-soft + silent).
func TestDoctorCheckUpdatesSilentWhenOffline(t *testing.T) {
	t.Setenv("NOLE_RELEASES_API", "http://127.0.0.1:1") // unroutable
	out, err := runDoctorWith(t, "--check-updates")
	if err != nil {
		t.Fatalf("doctor --check-updates must never fail (offline), got %v\n%s", err, out)
	}
	if strings.Contains(out, "- update:") {
		t.Fatalf("offline check must print no update line:\n%s", out)
	}
}

// A newer release than the running version warns, but the warning is fail-soft:
// exit code stays 0.
func TestDoctorCheckUpdatesWarnsWhenStale(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer srv.Close()
	t.Setenv("NOLE_RELEASES_API", srv.URL)

	// Pin a release-shaped current version below the served tag (save/restore).
	old := version.Version
	version.Version = "v0.9.0"
	t.Cleanup(func() { version.Version = old })

	out, err := runDoctorWith(t, "--check-updates")
	if err != nil {
		t.Fatalf("a stale warning must not fail doctor, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "- update:") || !strings.Contains(out, "v99.0.0") {
		t.Fatalf("expected a stale update line naming the latest tag:\n%s", out)
	}
}

func TestDoctorCheckUpdatesJSONCarriesUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer srv.Close()
	t.Setenv("NOLE_RELEASES_API", srv.URL)
	old := version.Version
	version.Version = "v0.9.0"
	t.Cleanup(func() { version.Version = old })

	out, err := runDoctorWith(t, "--json", "--check-updates")
	if err != nil {
		t.Fatalf("doctor --json --check-updates failed: %v\n%s", err, out)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if report.Update == nil || !report.Update.Stale || report.Update.Latest != "v99.0.0" {
		t.Fatalf("expected update{stale,latest=v99.0.0} in JSON, got %+v", report.Update)
	}
}

// A dev / non-release build must NOT be reported as "up to date" — it should say
// it is a development build (Codex review PR #42).
func TestDoctorCheckUpdatesDevBuildNotReportedUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.9.0"}`))
	}))
	defer srv.Close()
	t.Setenv("NOLE_RELEASES_API", srv.URL)
	old := version.Version
	version.Version = "dev"
	t.Cleanup(func() { version.Version = old })

	out, err := runDoctorWith(t, "--check-updates")
	if err != nil {
		t.Fatalf("doctor --check-updates failed on a dev build: %v\n%s", err, out)
	}
	if strings.Contains(out, "up to date") {
		t.Fatalf("a dev build must not be reported up to date:\n%s", out)
	}
	if !strings.Contains(out, "development build") {
		t.Fatalf("expected a development-build update line:\n%s", out)
	}
}

// Without --check-updates, doctor must make ZERO requests to the releases API.
func TestDoctorWithoutFlagDoesNoNetwork(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer srv.Close()
	t.Setenv("NOLE_RELEASES_API", srv.URL)

	out, err := runDoctorWith(t) // no --check-updates
	if err != nil {
		t.Fatalf("doctor failed: %v\n%s", err, out)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("doctor without --check-updates hit the releases API %d times; want 0", got)
	}
	if strings.Contains(out, "- update:") {
		t.Fatalf("no update line expected without the flag:\n%s", out)
	}
}
