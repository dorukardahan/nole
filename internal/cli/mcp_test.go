package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMCPCommandIsExposedThroughRootHelp(t *testing.T) {
	cmd := NewRootCommand()
	mcpCmd, remaining, err := cmd.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp command: %v", err)
	}
	if mcpCmd == cmd || mcpCmd.Name() != "mcp" || len(remaining) != 0 {
		t.Fatalf("root command did not resolve mcp subcommand: command=%q remaining=%v", mcpCmd.CommandPath(), remaining)
	}
	if mcpCmd.CommandPath() != "nole mcp" {
		t.Fatalf("MCP command path = %q, want %q", mcpCmd.CommandPath(), "nole mcp")
	}

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"mcp", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute mcp help: %v", err)
	}
	if !strings.Contains(stdout.String(), "Start MCP stdio server") {
		t.Fatalf("MCP help does not describe the stdio server:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--compact") {
		t.Fatalf("MCP help does not expose the compact surface flag:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("MCP help wrote to stderr: %q", stderr.String())
	}
}

func TestMCPCommandCancellationKeepsDiagnosticsOutOfStdout(t *testing.T) {
	const (
		protocolLine = `{"jsonrpc":"2.0","id":1,"result":{}}`
		diagnostic   = "MCP test runner started"
	)

	started := make(chan struct{})
	runner := func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, compact bool) error {
		if stdin == nil {
			return fmt.Errorf("stdin is nil")
		}
		if compact {
			return fmt.Errorf("compact unexpectedly enabled")
		}
		if _, err := fmt.Fprintln(stdout, protocolLine); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stderr, diagnostic); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	cmd := newRootCommand(newMCPCommandWithRunner(runner))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd.SetContext(ctx)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"mcp"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("MCP command did not start within 2s")
	}
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("MCP command returned an error after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MCP command did not stop within 2s of cancellation")
	}

	if got, want := stdout.String(), protocolLine+"\n"; got != want {
		t.Fatalf("MCP stdout must contain protocol output only: got %q, want %q", got, want)
	}
	if strings.Contains(stdout.String(), diagnostic) {
		t.Fatalf("human diagnostic leaked to MCP stdout: %q", stdout.String())
	}
	if got, want := stderr.String(), diagnostic+"\n"; got != want {
		t.Fatalf("MCP diagnostic output = %q, want stderr output %q", got, want)
	}
}

func TestMCPCommandPassesCompactFlagToRunner(t *testing.T) {
	var gotCompact bool
	runner := func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, compact bool) error {
		gotCompact = compact
		return nil
	}

	cmd := newRootCommand(newMCPCommandWithRunner(runner))
	cmd.SetArgs([]string{"mcp", "--compact"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute compact MCP command: %v", err)
	}
	if !gotCompact {
		t.Fatal("compact flag was not passed to the MCP runner")
	}
}
