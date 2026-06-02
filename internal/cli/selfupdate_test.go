package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dorukardahan/nole/internal/version"
)

func runSelfUpdate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"self-update"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

// An unknown --verify value must be rejected before any network/IO (mirrors the
// install.sh NOLE_INSTALL_VERIFY validation).
func TestSelfUpdateRejectsInvalidVerifyMode(t *testing.T) {
	out, err := runSelfUpdate(t, "--verify", "required")
	if err == nil || !strings.Contains(err.Error(), "invalid verify mode") {
		t.Fatalf("expected invalid-mode rejection, got err=%v out=%s", err, out)
	}
}

// --check-only reports a newer release and downloads/installs nothing.
func TestSelfUpdateCheckOnlyReportsNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("self-update must stay anonymous; got auth header")
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer srv.Close()
	t.Setenv("NOLE_RELEASES_API", srv.URL)

	origV := version.Version
	version.Version = "0.10.0"
	defer func() { version.Version = origV }()

	out, err := runSelfUpdate(t, "--check-only")
	if err != nil {
		t.Fatalf("check-only must not error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "v9.9.9") || !strings.Contains(out, "newer release is available") {
		t.Fatalf("expected a newer-release notice, got:\n%s", out)
	}
}
