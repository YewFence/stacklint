package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/spf13/cobra"
)

func resolveConfigDir(cmd *cobra.Command) (string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return config.ResolveDir(configDir, workdir)
}

// selectStacks resolves target arguments the same way for every command: no
// arguments select every discovered stack, each argument matches one stack by
// directory, Compose file, or unique path fragment, and duplicate matches
// collapse to a single stack.
func selectStacks(args []string, stacks []discover.Stack, repoRoot string) ([]discover.Stack, error) {
	if len(args) == 0 {
		return stacks, nil
	}
	var selected []discover.Stack
	seen := map[string]bool{}
	for _, arg := range args {
		stack, err := discover.MatchTarget(arg, stacks, repoRoot)
		if err != nil {
			stack, err = discover.MatchTarget(filepath.Join(repoRoot, arg), stacks, repoRoot)
			if err != nil {
				return nil, err
			}
		}
		if !seen[stack.RelPath] {
			seen[stack.RelPath] = true
			selected = append(selected, stack)
		}
	}
	return selected, nil
}
