package envtemplate

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
)

// Target is one complete, independently deployable Compose combination.
type Target struct {
	Stack   discover.Stack
	Variant string
	Files   []string
	Output  string
}

// Targets resolves registered combinations, including the implicit base of every stack.
func Targets(cfg *config.Config, repoRoot string, stacks []discover.Stack) ([]Target, error) {
	byStack := map[string]discover.Stack{}
	var targets []Target
	output := cfg.Env.Output
	if output == "" {
		output = ".env.example"
	}
	for _, stack := range stacks {
		if _, exists := byStack[stack.RelPath]; exists {
			return nil, fmt.Errorf("multiple main Compose files for stack %s", stack.RelPath)
		}
		byStack[stack.RelPath] = stack
		targets = append(targets, Target{Stack: stack, Variant: "base", Files: []string{stack.ComposeFile}, Output: filepath.Join(stack.Dir, output)})
	}
	for _, variant := range cfg.Env.Variants {
		path := filepath.ToSlash(filepath.Clean(variant.Stack))
		stack, ok := byStack[path]
		if !ok {
			return nil, fmt.Errorf("env.variant %s: stack %s does not exist", variant.Name, variant.Stack)
		}
		target := Target{Stack: stack, Variant: variant.Name, Output: filepath.Join(stack.Dir, variant.Output)}
		for _, file := range variant.Files {
			target.Files = append(target.Files, filepath.Join(repoRoot, file))
		}
		if target.Files[0] != stack.ComposeFile {
			return nil, fmt.Errorf("env.variant %s: first file must be stack %s's main Compose file", variant.Name, variant.Stack)
		}
		targets = append(targets, target)
	}
	slices.SortFunc(targets, func(a, b Target) int {
		if a.Stack.RelPath < b.Stack.RelPath {
			return -1
		}
		if a.Stack.RelPath > b.Stack.RelPath {
			return 1
		}
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
	return targets, nil
}
