package cli

import (
	"github.com/spf13/cobra"
)

func newRenderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render [target]",
		Short: "Render a single stack's configuration as JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "render")
		},
	}
	cmd.Flags().Bool("no-interpolate", false, "Output uninterpolated view")
	cmd.Flags().String("format", "json", "Output format")
	return cmd
}
