package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/YewFence/stacklint/internal/check"
	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/images"
	"github.com/YewFence/stacklint/internal/refload"
)

func newImagesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Image reference validation and inventory",
	}

	digestsCmd := &cobra.Command{
		Use:   "digests",
		Short: "Verify image digests against remote registries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			if format != "text" && format != "json" {
				return fmt.Errorf("images digests only supports --format text or json, got %q", format)
			}
			base, _ := cmd.Flags().GetString("base")
			full, _ := cmd.Flags().GetBool("full")
			ci, _ := cmd.Flags().GetString("ci")
			return runImagesDigests(cmd, base, full, ci, format)
		},
	}
	digestsCmd.Flags().String("base", "", "Incremental base Git ref")
	digestsCmd.Flags().Bool("full", false, "Check all images (no base)")
	digestsCmd.Flags().String("ci", "", "CI provider (e.g., crow)")
	digestsCmd.Flags().String("format", "text", "Output format (text or json)")
	digestsCmd.MarkFlagsMutuallyExclusive("base", "full", "ci")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Output image inventory as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			if format != "json" {
				return fmt.Errorf("images list only supports --format json, got %q", format)
			}
			ref, _ := cmd.Flags().GetString("ref")
			return runImagesList(cmd, ref)
		},
	}
	listCmd.Flags().String("ref", "", "Load images from Git ref")
	listCmd.Flags().String("format", "json", "Output format")

	cmd.AddCommand(digestsCmd, listCmd)
	return cmd
}

type imagesDigestsReport struct {
	Version     int               `json:"version"`
	Mode        string            `json:"mode"` // "full" or "incremental"
	Base        string            `json:"base,omitempty"`
	Results     []images.Result   `json:"results"`
	Diagnostics []check.Diagnostic `json:"diagnostics"`
	Summary     digestsSummary    `json:"summary"`
	ExitCode    int               `json:"exit_code"`
}

type digestsSummary struct {
	Checked  int `json:"checked"`
	Failed   int `json:"failed"`
	Warnings int `json:"warnings"`
}

func runImagesDigests(cmd *cobra.Command, base string, full bool, ciProvider string, format string) error {
	report, err := buildImagesDigests(cmd, base, full, ciProvider)
	if err != nil {
		report = &imagesDigestsReport{
			Version: 1,
			Results: []images.Result{},
			Diagnostics: []check.Diagnostic{{
				Severity: "error",
				Rule:     "configuration",
				Message:  err.Error(),
				Hint:     "Check stacklint.toml, the selected base ref and CI environment.",
			}},
			ExitCode: 2,
		}
	}
	if err := printDigestsReport(cmd, report, format); err != nil {
		return err
	}
	if report.ExitCode != 0 {
		return &commandFailure{code: report.ExitCode, message: "images digests failed", reported: true}
	}
	return nil
}

func buildImagesDigests(cmd *cobra.Command, base string, full bool, ciProvider string) (*imagesDigestsReport, error) {
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	root := cfg.RepoRoot(dir)
	ctx := cmd.Context()

	report := &imagesDigestsReport{Version: 1, Results: []images.Result{}, Diagnostics: []check.Diagnostic{}}

	baseRef := base
	if ciProvider != "" {
		decision, err := images.ResolveCIBase(ciProvider, os.Getenv, root)
		if err != nil {
			return nil, err
		}
		baseRef, full = decision.Ref, decision.Full
	}
	if baseRef == "" && !full {
		baseRef = "origin/main"
	}
	report.Mode = "incremental"
	report.Base = baseRef
	if full {
		report.Mode = "full"
		report.Base = ""
	}

	stacks, err := discover.FindAll(cfg, root)
	if err != nil {
		return nil, err
	}
	head, headDiagnostics := images.List(ctx, cfg, root, stacks)
	report.Diagnostics = append(report.Diagnostics, headDiagnostics...)

	var baseline []images.Record
	if report.Mode == "incremental" {
		if _, err := refload.Resolve(root, baseRef); err != nil {
			return nil, fmt.Errorf("incremental base is unavailable: %w", err)
		}
		materialized, err := refload.Materialize(root, baseRef)
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(materialized) }()

		var baselineDiagnostics []check.Diagnostic
		baseline, baselineDiagnostics = baselineImages(ctx, cfg, materialized)
		if changed, err := images.PolicyChangedAtRef(dir, materialized); err != nil {
			return nil, err
		} else if changed {
			baseline = nil
			report.Diagnostics = append(report.Diagnostics, fallbackWarning("images policy changed relative to the base ref"))
		} else if len(baselineDiagnostics) > 0 {
			baseline = nil
			report.Diagnostics = append(report.Diagnostics, fallbackWarning("could not render every baseline combination"))
		}
		if baseline == nil {
			report.Mode = "full"
			report.Base = ""
		}
	}

	planned := images.Plan(head, baseline)
	if len(planned) == 0 {
		report.ExitCode = summarize(report)
		return report, nil
	}

	client, err := images.NewRegClient(cfg.Images.TimeoutOrDefault(), cfg.Images.RetriesOrDefault())
	if err != nil {
		return nil, err
	}
	checker := &images.Checker{
		Client:              client,
		FloatingTags:        cfg.Images.FloatingTags,
		PrivateRepositories: cfg.Images.PrivateRepositories,
		Jobs:                cfg.Images.ConcurrencyOrDefault(),
		RegistryJobs:        cfg.Images.PerRegistryConcurrencyOrDefault(),
	}
	report.Results = checker.Check(ctx, planned, func(completed, total int, result images.Result) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] %s %s\n", completed, total, result.Outcome, result.Image)
	})
	report.ExitCode = summarize(report)
	return report, nil
}

// baselineImages collects the baseline inventory. Discovery or render
// failures are returned as diagnostics; the caller degrades to a full check.
func baselineImages(ctx context.Context, cfg *config.Config, materialized string) ([]images.Record, []check.Diagnostic) {
	stacks, err := discover.FindAll(cfg, materialized)
	if err != nil {
		return nil, []check.Diagnostic{{Severity: "warning", Rule: "render", Message: "could not discover baseline stacks: " + err.Error()}}
	}
	return images.List(ctx, cfg, materialized, stacks)
}

func fallbackWarning(reason string) check.Diagnostic {
	return check.Diagnostic{
		Severity: "warning",
		Rule:     "baseline",
		Message:  reason + "; running a full image digest check",
	}
}

func summarize(report *imagesDigestsReport) int {
	for _, result := range report.Results {
		report.Summary.Checked++
		switch result.Outcome {
		case "error":
			report.Summary.Failed++
		case "warning":
			report.Summary.Warnings++
		}
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Severity == "error" {
			report.Summary.Failed++
		}
	}
	if report.Summary.Failed > 0 {
		return 1
	}
	return 0
}

func printDigestsReport(cmd *cobra.Command, report *imagesDigestsReport, format string) error {
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}
	out := cmd.OutOrStdout()
	for _, diagnostic := range report.Diagnostics {
		if _, err := fmt.Fprintf(out, "%s [%s]: %s\n", diagnostic.Severity, diagnostic.Rule, diagnostic.Message); err != nil {
			return err
		}
	}
	for _, result := range report.Results {
		if result.Outcome == "ok" {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s %s: %s\n", result.Outcome, result.Image, result.Detail); err != nil {
			return err
		}
		for _, location := range result.Locations {
			if _, err := fmt.Fprintf(out, "  %s %s %s (%s)\n", location.Stack, location.Variant, location.Service, location.ComposeFile); err != nil {
				return err
			}
		}
	}
	summary := fmt.Sprintf("Checked %d image reference(s)", report.Summary.Checked)
	if report.Mode != "" {
		summary += " in " + report.Mode + " mode"
	}
	summary += fmt.Sprintf("; %d failed, %d warning(s).", report.Summary.Failed, report.Summary.Warnings)
	if _, err := fmt.Fprintln(out, summary); err != nil {
		return err
	}
	return nil
}

type imagesListReport struct {
	Version     int               `json:"version"`
	Ref         string            `json:"ref,omitempty"`
	Images      []images.Record   `json:"images"`
	Diagnostics []check.Diagnostic `json:"diagnostics"`
	ExitCode    int               `json:"exit_code"`
}

func runImagesList(cmd *cobra.Command, ref string) error {
	report, err := buildImagesList(cmd.Context(), cmd, ref)
	if err != nil {
		report = &imagesListReport{
			Version: 1,
			Images:  []images.Record{},
			Diagnostics: []check.Diagnostic{{
				Severity: "error",
				Rule:     "configuration",
				Message:  err.Error(),
				Hint:     "Check stacklint.toml, approvals.toml and target selection.",
			}},
			ExitCode: 2,
		}
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
		return err
	}
	if report.ExitCode != 0 {
		return &commandFailure{code: report.ExitCode, message: "images list failed", reported: true}
	}
	return nil
}

func buildImagesList(ctx context.Context, cmd *cobra.Command, ref string) (*imagesListReport, error) {
	dir, err := resolveConfigDir(cmd)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	root := cfg.RepoRoot(dir)

	// Policy always comes from the working-tree configuration; the ref only
	// decides which content is loaded.
	renderRoot := root
	if ref != "" {
		materialized, err := refload.Materialize(root, ref)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := os.RemoveAll(materialized); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not remove materialized ref %s: %v\n", materialized, err)
			}
		}()
		renderRoot = materialized
	}

	stacks, err := discover.FindAll(cfg, renderRoot)
	if err != nil {
		return nil, err
	}
	records, diagnostics := images.List(ctx, cfg, renderRoot, stacks)
	report := &imagesListReport{Version: 1, Ref: ref, Images: records, Diagnostics: diagnostics}
	if len(diagnostics) > 0 {
		report.ExitCode = 1
	}
	return report, nil
}
