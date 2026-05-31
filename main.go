package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dorukardahan/nole/internal/cli"
	"github.com/dorukardahan/nole/internal/safeerr"
)

func main() {
	// A signal-aware root context lets Ctrl-C / SIGTERM cancel in-flight
	// provider work (search/extract/research walks, the Scrapling subprocess)
	// instead of hard-killing mid-request. Commands thread cmd.Context() into
	// the service; `nole serve` layers its own graceful-shutdown handling.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Restore default signal handling once the first interrupt has cancelled the
	// context, so a SECOND Ctrl-C / SIGTERM force-exits the process instead of
	// being swallowed (re-cancelling an already-cancelled context) while a slow
	// graceful shutdown is in progress — e.g. `nole serve` draining in-flight
	// requests for up to 30s, or a wedged child/extract path. The first signal
	// stays graceful; the second hits the default terminate disposition.
	go func() {
		<-ctx.Done()
		stop()
	}()

	if err := cli.NewRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, safeerr.Message(err))
		os.Exit(1)
	}
}
