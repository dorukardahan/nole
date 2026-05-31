package cli

import (
	"context"
	"testing"
	"time"
)

func TestBindIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8765", true},
		{"localhost:8765", true},
		{"[::1]:8765", true},
		{"0.0.0.0:8765", false},
		{":8765", false},
		{"[::]:8765", false},
		{"192.168.1.10:8765", false},
		{"example.com:8765", false},
	}
	for _, c := range cases {
		if got := bindIsLoopback(c.addr); got != c.want {
			t.Errorf("bindIsLoopback(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestHTTPServerGracefulShutdown(t *testing.T) {
	// start() must return nil (clean) when its context is cancelled, having
	// gone through server.Shutdown rather than returning ErrServerClosed as an
	// error. Binds an ephemeral port; we never connect — we only assert the
	// SIGINT/SIGTERM drain path exits cleanly and promptly.
	h := newTestHTTPHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.start(ctx, "127.0.0.1:0")
	}()

	// Give the listener time to bind before triggering shutdown.
	time.Sleep(75 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("graceful shutdown returned error, want nil: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5s of context cancellation")
	}
}
