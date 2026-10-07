package cli

import (
	"fmt"
	"os"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/spf13/cobra"
)

func resolveConfigDir(cmd *cobra.Command) (string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return config.ResolveDir(configDir, workdir)
}
