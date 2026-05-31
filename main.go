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

	if err := cli.NewRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, safeerr.Message(err))
		os.Exit(1)
	}
}
