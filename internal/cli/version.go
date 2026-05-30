package cli

import (
	"fmt"

	"github.com/dorukardahan/nole/internal/version"
	"github.com/spf13/cobra"
)

// newVersionCommand prints the binary's build metadata. It is the runtime
// consumer for version.Commit and version.Date (otherwise dead vars) and gives
// users/agents a CLI way to query the running binary's version — the MCP server
// already reports version.Version, but there was no equivalent CLI surface.
// Commit/Date show "unknown" until a build stamps them via ldflags
// (see scripts/check-release-builds.sh).
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the nole binary version, commit, and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "nole %s\n", version.Version)
			fmt.Fprintf(out, "commit: %s\n", version.Commit)
			fmt.Fprintf(out, "date:   %s\n", version.Date)
			return nil
		},
	}
}
