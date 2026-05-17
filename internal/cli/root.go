package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "nole",
		Short:         "Nólë: BYOK free-tier search/retrieval router for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newSearchCommand())
	cmd.AddCommand(newExtractCommand())
	cmd.AddCommand(newResearchCommand())
	cmd.AddCommand(newBenchCommand())
	cmd.AddCommand(newProvidersCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newMCPCommand())
	cmd.AddCommand(newServeCommand())
	cmd.AddCommand(newSetupCommand())
	return cmd
}
