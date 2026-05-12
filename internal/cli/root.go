package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "searchmcp",
		Short:         "BYOK free-tier search/retrieval router for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newSearchCommand())
	cmd.AddCommand(newExtractCommand())
	cmd.AddCommand(newProvidersCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newMCPCommand())
	cmd.AddCommand(newSetupCommand())
	return cmd
}
