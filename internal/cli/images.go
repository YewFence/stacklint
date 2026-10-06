package cli

import (
	"github.com/spf13/cobra"
)

func newImagesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Image reference validation and inventory",
	}

	digestsCmd := &cobra.Command{
		Use:   "digests",
		Short: "Verify image digests against remote registries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "images digests")
		},
	}
	digestsCmd.Flags().String("base", "", "Incremental base Git ref")
	digestsCmd.Flags().Bool("full", false, "Check all images (no base)")
	digestsCmd.Flags().String("ci", "", "CI provider (e.g., crow)")
	digestsCmd.MarkFlagsMutuallyExclusive("base", "full", "ci")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Output image inventory as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "images list")
		},
	}
	listCmd.Flags().String("ref", "", "Load images from Git ref")
	listCmd.Flags().String("format", "json", "Output format")

	cmd.AddCommand(digestsCmd, listCmd)
	return cmd
}
