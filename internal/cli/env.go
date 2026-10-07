package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/envtemplate"
	"github.com/spf13/cobra"
)

type envDiagnostic struct {
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Stack    string `json:"stack"`
	Variant  string `json:"variant,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

type envReport struct {
	Results     []envStatus     `json:"results"`
	Diagnostics []envDiagnostic `json:"diagnostics"`
	ExitCode    int             `json:"exit_code"`
}

type envStatus struct {
	Stack   string `json:"stack"`
	Variant string `json:"variant"`
	Output  string `json:"output"`
	Status  string `json:"status"`
}

type commandFailure struct {
	code     int
	message  string
	reported bool
}

func (err *commandFailure) Error() string { return err.message }

// ExitCode distinguishes check failures from usage/configuration/incomplete checks.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if failure, ok := err.(*commandFailure); ok {
		return failure.code
	}
	return 2
}

func newEnvCommand() *cobra.Command {
	command := &cobra.Command{Use: "env", Short: "Generate or check environment override templates"}
	for _, action := range []string{"generate", "check"} {
		var variant, format string
		subcommand := &cobra.Command{
			Use:   action + " [targets...]",
			Short: map[string]string{"generate": "Generate complete environment templates from Compose combinations", "check": "Read-only check for template drift and exemptions"}[action],
			RunE: func(cmd *cobra.Command, args []string) error {
				if action == "generate" && len(args) == 0 {
					return fmt.Errorf("env generate requires at least one explicit target")
				}
				if format != "json" && format != "text" {
					return fmt.Errorf("env only supports --format text or json")
				}
				report, err := runEnv(cmd, action, args, variant)
				if err != nil {
					report = envReport{Results: []envStatus{}, Diagnostics: []envDiagnostic{{Severity: "error", Rule: "env.configuration", Message: err.Error(), Hint: "Check stacklint.toml, approvals.toml and target selection."}}, ExitCode: 2}
				}
				if err := printEnvReport(cmd, report, format); err != nil {
					return err
				}
				if report.ExitCode != 0 {
					return &commandFailure{code: report.ExitCode, message: "environment template checks failed", reported: true}
				}
				return nil
			},
		}
		subcommand.Flags().StringVar(&variant, "variant", "", "Select one registered variant (base selects the implicit base)")
		subcommand.Flags().StringVar(&format, "format", "text", "Output format (text or json)")
		command.AddCommand(subcommand)
	}
	return command
}

func runEnv(cmd *cobra.Command, action string, args []string, variant string) (envReport, error) {
	report := envReport{Results: []envStatus{}, Diagnostics: []envDiagnostic{}}
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		return report, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return report, err
	}
	root := cfg.RepoRoot(dir)
	stacks, err := discover.FindAll(cfg, root)
	if err != nil {
		return report, err
	}
	all, err := envtemplate.Targets(cfg, root, stacks)
	if err != nil {
		return report, err
	}
	exemptions, err := config.LoadEnvExemptions(dir)
	if err != nil {
		return report, err
	}
	selected := map[string]bool{}
	if len(args) == 0 {
		for _, stack := range stacks {
			selected[stack.RelPath] = true
		}
	}
	for _, arg := range args {
		stack, err := discover.MatchTarget(arg, stacks, root)
		if err != nil {
			stack, err = discover.MatchTarget(filepath.Join(root, arg), stacks, root)
			if err != nil {
				return report, err
			}
		}
		selected[stack.RelPath] = true
	}
	excluded := map[string][]string{}
	var targets []envtemplate.Target
	matching := map[string]bool{}
	for _, target := range all {
		excluded[target.Stack.RelPath] = append(excluded[target.Stack.RelPath], target.Output)
		if selected[target.Stack.RelPath] && (variant == "" || target.Variant == variant) {
			targets = append(targets, target)
			matching[target.Stack.RelPath] = true
		}
	}
	for _, stack := range sortedEnvKeys(selected) {
		if !matching[stack] {
			return report, fmt.Errorf("variant %q is not registered for stack %s", variant, stack)
		}
	}
	if len(targets) == 0 {
		return report, fmt.Errorf("no stacks selected; check repository discovery configuration")
	}
	if len(args) == 0 {
		known := map[string]bool{}
		for _, stack := range stacks {
			known[stack.RelPath] = true
		}
		for _, stack := range sortedEnvKeys(exemptions) {
			if !known[stack] {
				report.failure(1, stack, "", "env.exemption-stale", "exemption target no longer exists", "Remove the stale [[env_exemption]] from approvals.toml.")
			}
		}
	}
	var prepared []*envtemplate.Result
	for _, target := range targets {
		stack := target.Stack.RelPath
		output, err := envtemplate.ReadOutput(target)
		status := envStatus{Stack: stack, Variant: target.Variant, Output: filepath.Base(target.Output)}
		if err != nil {
			report.failure(2, stack, target.Variant, "env.output", err.Error(), "Use a regular output file inside the stack directory.")
			status.Status = "error"
			report.Results = append(report.Results, status)
			continue
		}
		if exemption, exists := exemptions[stack]; exists {
			status.Status = "exempt"
			if action == "generate" {
				report.failure(2, stack, target.Variant, "env.exempt", "refusing to generate an exempt stack", "Remove its [[env_exemption]] before generating.")
				status.Status = "error"
			} else if envtemplate.Generated(output) {
				report.failure(1, stack, target.Variant, "env.exemption-generated", "exempt template carries a generated header", "Remove the exemption or restore a hand-maintained template.")
				status.Status = "error"
			} else if exemption.Kind == "env-file-passthrough" {
				passthrough, warnings, err := envtemplate.EnvFilePassthrough(cmd.Context(), cfg, root, target, excluded[stack])
				report.warnings(stack, target.Variant, warnings)
				if err != nil {
					report.failure(2, stack, target.Variant, "env.analysis", err.Error(), "Check the registered Compose combination and static inputs.")
					status.Status = "error"
				} else if !passthrough {
					report.failure(1, stack, target.Variant, "env.exemption-stale", "env-file-passthrough exemption has no service-level project .env reference", "Remove or update the stack's [[env_exemption]].")
					status.Status = "error"
				}
			}
			report.Results = append(report.Results, status)
			continue
		}
		result, err := envtemplate.Generate(cmd.Context(), cfg, root, target, excluded[stack])
		if result != nil {
			report.warnings(stack, target.Variant, result.Warnings)
		}
		if err != nil {
			report.failure(2, stack, target.Variant, "env.analysis", err.Error(), "Check Compose inputs, annotations, include authorization and static placeholders.")
			status.Status = "error"
			report.Results = append(report.Results, status)
			continue
		}
		if action == "generate" {
			prepared = append(prepared, result)
			status.Status = "prepared"
		} else if !bytes.Equal(output, result.Content) {
			status.Status = "drift"
			report.failure(1, stack, target.Variant, "env.drift", "template is missing or differs from generated content", "Run stacklint env generate "+stack+" --variant "+target.Variant+" or record an explicit [[env_exemption]].")
		} else {
			status.Status = "ok"
		}
		report.Results = append(report.Results, status)
	}
	if action == "generate" && report.ExitCode == 0 {
		for _, result := range prepared {
			if err := envtemplate.Write(result); err != nil {
				report.failure(2, result.Target.Stack.RelPath, result.Target.Variant, "env.write", "could not atomically replace template: "+err.Error(), "Earlier successful replacements remain written; retry after resolving the filesystem error.")
				setEnvStatus(&report, result.Target, "error")
				break
			}
			setEnvStatus(&report, result.Target, "generated")
		}
	}
	return report, nil
}

func setEnvStatus(report *envReport, target envtemplate.Target, status string) {
	for i := range report.Results {
		if report.Results[i].Stack == target.Stack.RelPath && report.Results[i].Variant == target.Variant {
			report.Results[i].Status = status
		}
	}
}

func sortedEnvKeys[V any](values map[string]V) []string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (report *envReport) failure(code int, stack, variant, rule, message, hint string) {
	if code > report.ExitCode {
		report.ExitCode = code
	}
	report.Diagnostics = append(report.Diagnostics, envDiagnostic{Severity: "error", Rule: rule, Stack: stack, Variant: variant, Message: message, Hint: hint})
}

func (report *envReport) warnings(stack, variant string, warnings []string) {
	for _, warning := range warnings {
		report.Diagnostics = append(report.Diagnostics, envDiagnostic{Severity: "warning", Rule: "env.include-dotenv", Stack: stack, Variant: variant, Message: warning})
	}
}

func printEnvReport(cmd *cobra.Command, report envReport, format string) error {
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}
	for _, diagnostic := range report.Diagnostics {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s [%s] %s: %s\n", diagnostic.Stack, diagnostic.Variant, diagnostic.Severity, diagnostic.Rule, diagnostic.Message); err != nil {
			return err
		}
		if diagnostic.Hint != "" {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "  hint: "+diagnostic.Hint); err != nil {
				return err
			}
		}
	}
	for _, result := range report.Results {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s (%s)\n", result.Stack, result.Variant, result.Status, result.Output); err != nil {
			return err
		}
	}
	return nil
}
