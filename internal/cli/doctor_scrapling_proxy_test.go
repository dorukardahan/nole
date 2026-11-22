package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDoctorScraplingProxyPrivacy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is unix-only")
	}
	for _, script := range []string{"printf '%s' \"$NOLE_TEST_DIAGNOSTIC\"\n", "printf '%s' \"$NOLE_TEST_DIAGNOSTIC\" >&2\nexit 1\n"} {
		for _, proxy := range []string{"http://fixture-user:fixture-password@127.0.0.1:8080", "invalid-fixture-password"} {
			t.Run("safe status", func(t *testing.T) {
				python := filepath.Join(t.TempDir(), "python")
				if err := os.WriteFile(python, []byte("#!/bin/sh\n"+script), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("NOLE_SCRAPLING_PYTHON", python)
				t.Setenv("NOLE_SCRAPLING_PROXY", proxy)
				t.Setenv("NOLE_TEST_DIAGNOSTIC", proxy)
				for _, args := range [][]string{nil, {"--json"}} {
					out, err := runDoctor(t, args...)
					if err != nil {
						t.Fatal(err)
					}
					for _, secret := range []string{"fixture-user", "fixture-password", "127.0.0.1:8080"} {
						if strings.Contains(out, secret) {
							t.Fatal("doctor leaked proxy configuration")
						}
					}
					if !strings.Contains(out, "proxy: configured") {
						t.Fatal("doctor omitted proxy presence")
					}
				}
			})
		}
	}
}
