package cli

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("NOLE_DISABLE_ENV_FILE", "1")
	os.Exit(m.Run())
}
