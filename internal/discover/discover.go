package discover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
)

// Stack is a discovered deployment unit: a directory holding a main Compose file.
type Stack struct {
	// Dir is the absolute path to the stack directory.
	Dir string
	// ComposeFile is the absolute path to the main Compose file.
	ComposeFile string
	// RelPath is the stack directory relative to the repository root, slash-separated.
	RelPath string
}

// FindAll discovers stacks under every configured deployment root, sorted by RelPath.
func FindAll(cfg *config.Config, repoRoot string) ([]Stack, error) {
	filenames := map[string]struct{}{}
	for _, name := range cfg.Repository.ComposeFilenames {
		filenames[name] = struct{}{}
	}

	var stacks []Stack
	for _, root := range cfg.Repository.DeploymentRoots {
		absRoot := filepath.Join(repoRoot, root)
		if _, err := os.Stat(absRoot); err != nil {
			return nil, fmt.Errorf("deployment root %s: %w", root, err)
		}
		err := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := filenames[d.Name()]; !ok {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				return fmt.Errorf("relativize %s: %w", path, err)
			}
			stacks = append(stacks, Stack{
				Dir:         filepath.Dir(path),
				ComposeFile: path,
				RelPath:     filepath.ToSlash(rel),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk deployment root %s: %w", root, err)
		}
	}
	sort.Slice(stacks, func(i, j int) bool { return stacks[i].RelPath < stacks[j].RelPath })
	return stacks, nil
}

// MatchTarget selects the single stack identified by target, which may be a stack
// directory, a Compose file, or a path fragment. Zero or multiple matches fail.
func MatchTarget(target string, stacks []Stack, repoRoot string) (Stack, error) {
	if abs, err := filepath.Abs(target); err == nil {
		for _, s := range stacks {
			if abs == s.Dir || abs == s.ComposeFile {
				return s, nil
			}
		}
	}

	fragment := filepath.ToSlash(target)
	var matched []Stack
	for _, s := range stacks {
		if s.RelPath == fragment || strings.Contains(s.RelPath, fragment) {
			matched = append(matched, s)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return Stack{}, fmt.Errorf("target %q matched no stack", target)
	default:
		names := make([]string, len(matched))
		for i, s := range matched {
			names[i] = s.RelPath
		}
		return Stack{}, fmt.Errorf("target %q matched multiple stacks: %v", target, names)
	}
}
