package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/loader"
)

func newRenderCommand() *cobra.Command {
	var noInterpolate bool
	var format string
	cmd := &cobra.Command{
		Use:   "render [targets...]",
		Short: "Render Compose configurations as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "json" {
				return fmt.Errorf("render only supports --format json, got %q", format)
			}
			return runRender(cmd, args, noInterpolate)
		},
	}
	cmd.Flags().BoolVar(&noInterpolate, "no-interpolate", false, "Output uninterpolated view")
	cmd.Flags().StringVar(&format, "format", "json", "Output format")
	return cmd
}

func runRender(cmd *cobra.Command, args []string, noInterpolate bool) error {
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	repoRoot := cfg.RepoRoot(dir)

	stacks, err := discover.FindAll(cfg, repoRoot)
	if err != nil {
		return err
	}
	selected, err := selectStacks(args, stacks, repoRoot)
	if err != nil {
		return err
	}

	rendered := map[string]any{}
	for _, stack := range selected {
		result, err := loader.Render(cmd.Context(), cfg, stack)
		if err != nil {
			return fmt.Errorf("render %s: %w", stack.RelPath, err)
		}
		if noInterpolate {
			rendered[stack.RelPath] = result.Raw
		} else {
			rendered[stack.RelPath] = result.Project
		}
	}
	data, err := json.MarshalIndent(rendered, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal render result: %w", err)
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(data)); err != nil {
		return fmt.Errorf("write render output: %w", err)
	}
	return nil
}
