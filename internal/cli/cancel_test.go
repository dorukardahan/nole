package cli

import (
	"context"
	"errors"
	"io"
	"testing"
)

// The search/extract commands must thread cmd.Context() (signal-aware in main)
// into the service so Ctrl-C / SIGTERM cancels in-flight provider work instead
// of hard-killing the process mid-request. A pre-cancelled context
// short-circuits inside core.Service before any provider/network call, so these
// assert the wiring deterministically and without touching the network.
func TestSearchCommandHonorsContextCancellation(t *testing.T) {
	// defaultService() eagerly builds the quota ledger before the cancellation
	// short-circuit; keep it in-memory so the test never touches the operator's
	// real $HOME ledger.
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	cmd := newSearchCommand()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"anything"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("search with a cancelled context should surface an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("search error = %v, want context.Canceled", err)
	}
}

func TestExtractCommandHonorsContextCancellation(t *testing.T) {
	t.Setenv("NOLE_QUOTA_LEDGER_PATH", "memory")
	cmd := newExtractCommand()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	// A public literal IP passes safenet.ValidateURL without DNS (extract
	// validates the URL before the context check), so the only thing that can
	// fail here is the cancellation short-circuit.
	cmd.SetArgs([]string{"http://93.184.216.34/"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("extract with a cancelled context should surface an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("extract error = %v, want context.Canceled", err)
	}
}
