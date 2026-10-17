// Package check renders stack combinations and evaluates the repository rules
// against them: structure, image pinning, env_file passthrough, module wiring,
// module required environment, host bind addresses, published port uniqueness,
// volume naming and ownership, network ownership, and stale approval detection.
package check

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/loader"
)

// Diagnostic is one rule finding, tied to a stack combination.
type Diagnostic struct {
	Severity string `json:"severity"` // "error" or "warning"
	Rule     string `json:"rule"`
	Stack    string `json:"stack"`
	Variant  string `json:"variant,omitempty"`
	Service  string `json:"service,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

// Status is the per-combination outcome.
type Status struct {
	Stack   string `json:"stack"`
	Variant string `json:"variant"`
	Status  string `json:"status"` // "ok", "error", or "render-error"
}

// Report is the full check result.
type Report struct {
	Results     []Status     `json:"results"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	ExitCode    int          `json:"exit_code"`
}

// Combination is one deployable file combination of a stack: the implicit base
// or a registered variant.
type Combination struct {
	Stack   discover.Stack
	Variant string
	Files   []string // absolute, ordered
	Output  string   // absolute template path
}

// Combinations resolves the base and all registered variants of a stack.
func Combinations(cfg *config.Config, repoRoot string, stack discover.Stack) ([]Combination, error) {
	combos := []Combination{{Stack: stack, Variant: "base", Files: []string{stack.ComposeFile}, Output: filepath.Join(stack.Dir, cfg.Env.OutputOrDefault())}}
	for _, variant := range cfg.Env.Variants {
		if filepath.ToSlash(filepath.Clean(variant.Stack)) != stack.RelPath {
			continue
		}
		combo := Combination{Stack: stack, Variant: variant.Name, Output: filepath.Join(stack.Dir, variant.Output)}
		for _, file := range variant.Files {
			combo.Files = append(combo.Files, filepath.Join(repoRoot, file))
		}
		if len(combo.Files) == 0 || combo.Files[0] != stack.ComposeFile {
			return nil, fmt.Errorf("env.variant %s: first file must be stack %s's main Compose file", variant.Name, variant.Stack)
		}
		combos = append(combos, combo)
	}
	slices.SortFunc(combos, func(a, b Combination) int {
		if a.Variant == b.Variant {
			return 0
		}
		if a.Variant == "base" {
			return -1
		}
		if b.Variant == "base" {
			return 1
		}
		if a.Variant < b.Variant {
			return -1
		}
		return 1
	})
	return combos, nil
}

// reviewTracker records which approvals were consumed so stale ones can be
// reported at the end. Staleness is only judged when every Compose file an
// approval references was checked.
type reviewTracker struct {
	images   map[string]bool
	ports    map[string]bool
	binds    map[string]bool
	envFiles map[string]bool
	// observedImages holds the current image per compose/service target, so
	// stale image approvals can distinguish a removed service from a service
	// whose image is now pinned.
	observedImages map[string]string
}

func newReviewTracker() *reviewTracker {
	return &reviewTracker{
		images:         map[string]bool{},
		ports:          map[string]bool{},
		binds:          map[string]bool{},
		envFiles:       map[string]bool{},
		observedImages: map[string]string{},
	}
}

func imageKey(compose, service string) string { return compose + "\x00" + service }
func bindKey(compose, service string) string  { return compose + "\x00" + service }
func envFileKey(compose, service, path string) string {
	return compose + "\x00" + service + "\x00" + path
}
func portKey(group string, port int, protocol string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", group, port, protocol)
}

// ruleContext carries everything one combination evaluation needs.
type ruleContext struct {
	cfg       *config.Config
	repoRoot  string
	approvals *config.Approvals
	combo     Combination
	// allCombos holds every combination of the same stack, so stack-level
	// rules (wiring) can union include graphs across variants.
	allCombos []Combination
	render    *loader.StackRender
	reviewed  *reviewTracker
}

func (c *ruleContext) composeRel() string {
	rel, err := filepath.Rel(c.repoRoot, c.combo.Stack.ComposeFile)
	if err != nil {
		return filepath.ToSlash(c.combo.Stack.ComposeFile)
	}
	return filepath.ToSlash(rel)
}

// allFiles returns every Compose file of the stack across all combinations.
func (c *ruleContext) allFiles() []string {
	var files []string
	for _, combo := range c.allCombos {
		files = append(files, combo.Files...)
	}
	return files
}

func (c *ruleContext) diagnose(rule, service, message, hint string) Diagnostic {
	return Diagnostic{
		Severity: "error",
		Rule:     rule,
		Stack:    c.combo.Stack.RelPath,
		Variant:  c.combo.Variant,
		Service:  service,
		Message:  message,
		Hint:     hint,
	}
}

// combinationRule is one per-combination rule.
type combinationRule func(c *ruleContext) []Diagnostic

var combinationRules = []struct {
	name string
	run  combinationRule
}{
	{"structure", checkStructure},
	{"image", checkImages},
	{"env_file", checkEnvFiles},
	{"wiring", checkWiring},
	{"module_environment", checkModuleEnvironment},
	{"bind_address", checkBindAddress},
	{"volume_name", checkVolumeNames},
}

// Run renders every selected stack's combinations and evaluates all rules.
// A stack that fails to render is reported and the others still run.
func Run(ctx context.Context, cfg *config.Config, repoRoot string, approvals *config.Approvals, stacks []discover.Stack) (*Report, error) {
	report := &Report{Results: []Status{}, Diagnostics: []Diagnostic{}}
	reviewed := newReviewTracker()
	var combos []*ruleContext
	for _, stack := range stacks {
		stackCombos, err := Combinations(cfg, repoRoot, stack)
		if err != nil {
			return nil, err
		}
		for _, combo := range stackCombos {
			render, err := loader.RenderCombination(ctx, cfg, stack, combo.Files, combo.Output)
			if err != nil {
				report.Results = append(report.Results, Status{Stack: stack.RelPath, Variant: combo.Variant, Status: "render-error"})
				report.Diagnostics = append(report.Diagnostics, Diagnostic{
					Severity: "error",
					Rule:     "render",
					Stack:    stack.RelPath,
					Variant:  combo.Variant,
					Message:  fmt.Sprintf("could not render combination: %v", err),
					Hint:     "Fix the Compose files or static render inputs, then re-run stacklint check.",
				})
				// The wiring rule reads source files only and produces the
				// actionable diagnostic when a missing wiring file is what broke
				// the render, so it still runs.
				rc := &ruleContext{cfg: cfg, repoRoot: repoRoot, approvals: approvals, combo: combo, allCombos: stackCombos, reviewed: reviewed}
				report.Diagnostics = append(report.Diagnostics, checkWiring(rc)...)
				continue
			}
			rc := &ruleContext{cfg: cfg, repoRoot: repoRoot, approvals: approvals, combo: combo, allCombos: stackCombos, render: render, reviewed: reviewed}
			combos = append(combos, rc)
			comboFailed := false
			for _, rule := range combinationRules {
				diagnostics := rule.run(rc)
				comboFailed = comboFailed || len(diagnostics) > 0
				report.Diagnostics = append(report.Diagnostics, diagnostics...)
			}
			if comboFailed {
				report.Results = append(report.Results, Status{Stack: stack.RelPath, Variant: combo.Variant, Status: "error"})
			} else {
				report.Results = append(report.Results, Status{Stack: stack.RelPath, Variant: combo.Variant, Status: "ok"})
			}
		}
	}
	report.Diagnostics = append(report.Diagnostics, checkPublishedPorts(cfg, approvals, combos, reviewed)...)
	report.Diagnostics = append(report.Diagnostics, checkResourceOwnership(combos)...)
	report.Diagnostics = append(report.Diagnostics, checkStaleApprovals(cfg, approvals, combos, reviewed)...)
	sortDiagnostics(report.Diagnostics)
	for i := range report.Results {
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Stack == report.Results[i].Stack && diagnostic.Variant == report.Results[i].Variant {
				report.Results[i].Status = "error"
			}
		}
	}
	if len(report.Diagnostics) > 0 {
		report.ExitCode = 1
	}
	return report, nil
}

func sortDiagnostics(diagnostics []Diagnostic) {
	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		for _, pair := range [][2]string{
			{a.Stack, b.Stack},
			{a.Variant, b.Variant},
			{a.Rule, b.Rule},
			{a.Service, b.Service},
			{a.Message, b.Message},
		} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
}
