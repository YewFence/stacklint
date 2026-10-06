package cli

import (
	"github.com/spf13/cobra"
)

func newCheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [targets...]",
		Short: "Validate Compose configurations against repository rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "check")
		},
	}
	return cmd
}
