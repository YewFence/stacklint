package cli

import (
	"errors"
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

func notImplemented(cmd *cobra.Command, feature string) error {
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		if errors.Is(err, config.ErrNotFound) || errors.Is(err, config.ErrInvalid) {
			return fmt.Errorf("configuration error: %w", err)
		}
		return err
	}
	return fmt.Errorf("%s is not implemented yet (config: %s)", feature, dir)
}
