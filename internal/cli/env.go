package cli

import (
	"github.com/spf13/cobra"
)

func newEnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Generate or check environment override templates",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "generate <targets...>",
			Short: "Generate .env.example from Compose files",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return notImplemented(cmd, "env generate")
			},
		},
		&cobra.Command{
			Use:   "check [targets...]",
			Short: "Check .env.example files against Compose sources",
			RunE: func(cmd *cobra.Command, args []string) error {
				return notImplemented(cmd, "env check")
			},
		},
	)
	return cmd
}
