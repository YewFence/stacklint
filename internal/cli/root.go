package cli

import (
	"fmt"
	"os"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/spf13/cobra"
)

var configDir string

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "stacklint",
		Short:         "Static analysis, environment template generation, and image checks for Docker Compose stack repositories",
		Long:          "Static analysis, environment template generation, and image checks for Docker Compose stack repositories",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "Configuration directory (overrides "+config.EnvVar+")")
	rootCmd.AddCommand(
		newVersionCommand(version),
		newCheckCommand(),
		newEnvCommand(),
		newImagesCommand(),
		newRenderCommand(),
	)
	return rootCmd
}

func Execute(version string) {
	rootCmd := NewRootCommand(version)
	if err := rootCmd.Execute(); err != nil {
		if failure, ok := err.(*commandFailure); !ok || !failure.reported {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(ExitCode(err))
	}
}
