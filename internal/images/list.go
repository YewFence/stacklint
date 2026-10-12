// Package images builds the image usage inventory of stacks and verifies
// references against remote registries.
package images

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/YewFence/stacklint/internal/check"
	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/loader"
)

// Record is one image usage: a service's image reference in one stack
// combination. Records are never deduplicated across positions: where an
// image is deployed is information for the consumer.
type Record struct {
	Stack       string `json:"stack"`
	Variant     string `json:"variant"`
	ComposeFile string `json:"compose_file"` // repository-relative main Compose file of the stack
	Service     string `json:"service"`
	Reference   string `json:"reference"`
}

// List renders every combination of the given stacks and collects image
// references. A stack combination that fails to render produces a diagnostic
// and the remaining combinations are still listed.
func List(ctx context.Context, cfg *config.Config, repoRoot string, stacks []discover.Stack) ([]Record, []check.Diagnostic) {
	records := []Record{}
	diagnostics := []check.Diagnostic{}
	for _, stack := range stacks {
		combos, err := check.Combinations(cfg, repoRoot, stack)
		if err != nil {
			diagnostics = append(diagnostics, renderDiagnostic(stack.RelPath, "", err))
			continue
		}
		for _, combo := range combos {
			rendered, err := loader.RenderCombination(ctx, cfg, stack, combo.Files, combo.Output)
			if err != nil {
				diagnostics = append(diagnostics, renderDiagnostic(stack.RelPath, combo.Variant, err))
				continue
			}
			composeRel, err := filepath.Rel(repoRoot, stack.ComposeFile)
			if err != nil {
				composeRel = stack.ComposeFile
			}
			names := make([]string, 0, len(rendered.Project.Services))
			for name := range rendered.Project.Services {
				names = append(names, name)
			}
			slices.Sort(names)
			for _, name := range names {
				service := rendered.Project.Services[name]
				if service.Image == "" {
					continue
				}
				records = append(records, Record{
					Stack:       stack.RelPath,
					Variant:     combo.Variant,
					ComposeFile: filepath.ToSlash(composeRel),
					Service:     name,
					Reference:   service.Image,
				})
			}
		}
	}
	return records, diagnostics
}

func renderDiagnostic(stack, variant string, err error) check.Diagnostic {
	return check.Diagnostic{
		Severity: "error",
		Rule:     "render",
		Stack:    stack,
		Variant:  variant,
		Message:  "could not render combination: " + err.Error(),
		Hint:     "Fix the Compose files or static render inputs, then re-run.",
	}
}
