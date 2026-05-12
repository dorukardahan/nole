package cli

import "testing"

func TestRootCommandExists(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Use != "searchmcp" {
		t.Fatalf("expected root command use searchmcp, got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Fatal("expected short description")
	}
}
