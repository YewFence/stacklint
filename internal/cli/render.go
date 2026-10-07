package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/loader"
)

func newRenderCommand() *cobra.Command {
	var noInterpolate bool
	var format string
	cmd := &cobra.Command{
		Use:   "render <target>",
		Short: "Render a single stack's configuration as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "json" {
				return fmt.Errorf("render only supports --format json, got %q", format)
			}
			return runRender(cmd.Context(), args[0], noInterpolate)
		},
	}
	cmd.Flags().BoolVar(&noInterpolate, "no-interpolate", false, "Output uninterpolated view")
	cmd.Flags().StringVar(&format, "format", "json", "Output format")
	return cmd
}

func runRender(ctx context.Context, target string, noInterpolate bool) error {
	configDir, err := resolveConfigDir(nil)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	repoRoot := cfg.RepoRoot(configDir)

	stacks, err := discover.FindAll(cfg, repoRoot)
	if err != nil {
		return err
	}
	stack, err := discover.MatchTarget(target, stacks, repoRoot)
	if err != nil {
		return err
	}

	rendered, err := loader.Render(ctx, cfg, repoRoot, stack)
	if err != nil {
		return err
	}

	var out any
	if noInterpolate {
		out = rendered.Raw
	} else {
		out = rendered.Project
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal render result for %s: %w", stack.RelPath, err)
	}
	if _, err := fmt.Fprintln(os.Stdout, string(data)); err != nil {
		return fmt.Errorf("write render output: %w", err)
	}
	return nil
}
