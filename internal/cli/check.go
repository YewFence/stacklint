package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/YewFence/stacklint/internal/check"
	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/spf13/cobra"
)

func newCheckCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "check [targets...]",
		Short: "Validate Compose configurations against repository rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "json" && format != "text" {
				return fmt.Errorf("check only supports --format text or json")
			}
			report, err := runCheck(cmd, args)
			if err != nil {
				report = &check.Report{
					Results: []check.Status{},
					Diagnostics: []check.Diagnostic{{
						Severity: "error",
						Rule:     "configuration",
						Message:  err.Error(),
						Hint:     "Check stacklint.toml, approvals.toml and target selection.",
					}},
					ExitCode: 2,
				}
			}
			if err := printCheckReport(cmd, report, format); err != nil {
				return err
			}
			if report.ExitCode != 0 {
				return &commandFailure{code: report.ExitCode, message: "checks failed", reported: true}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text or json)")
	return cmd
}

func runCheck(cmd *cobra.Command, args []string) (*check.Report, error) {
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	root := cfg.RepoRoot(dir)
	stacks, err := discover.FindAll(cfg, root)
	if err != nil {
		return nil, err
	}
	approvals, err := config.LoadApprovals(dir, root, cfg)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return check.Run(cmd.Context(), cfg, root, approvals, stacks)
	}
	var selected []discover.Stack
	for _, arg := range args {
		stack, err := discover.MatchTarget(arg, stacks, root)
		if err != nil {
			stack, err = discover.MatchTarget(filepath.Join(root, arg), stacks, root)
			if err != nil {
				return nil, err
			}
		}
		selected = append(selected, stack)
	}
	return check.Run(cmd.Context(), cfg, root, approvals, selected)
}

func printCheckReport(cmd *cobra.Command, report *check.Report, format string) error {
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}
	for _, diagnostic := range report.Diagnostics {
		target := diagnostic.Stack
		if diagnostic.Variant != "" && diagnostic.Variant != "base" {
			target += " " + diagnostic.Variant
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] %s: %s\n", target, diagnostic.Rule, diagnostic.Severity, diagnostic.Message); err != nil {
			return err
		}
		if diagnostic.Hint != "" {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "  hint: "+diagnostic.Hint); err != nil {
				return err
			}
		}
	}
	for _, result := range report.Results {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", result.Stack, result.Variant, result.Status); err != nil {
			return err
		}
	}
	return nil
}
