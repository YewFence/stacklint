// Package wiring derives render-relevant facts from a Compose combination's
// source YAML: which registered modules it includes, the variable sets of
// module wiring contracts, and @env-example values. The include graph is
// walked from source YAML so results cover recursive includes.
package wiring

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"go.yaml.in/yaml/v3"
)

// SourceFiles returns every Compose file in the combination's include graph:
// the given files followed by recursively included files, in first-seen order.
// Paths are absolute and clean. Include cycles and unreadable files fail.
//
// Resolution follows compose-go: include paths resolve against the including
// file's working directory (workingDir for the roots), while an entry's
// project_directory only becomes the working directory of the included files.
func SourceFiles(files []string, workingDir string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	var walk func(path, dir string) error
	walk = func(path, dir string) error {
		path = filepath.Clean(path)
		if seen[path] {
			return nil
		}
		seen[path] = true
		result = append(result, path)
		includes, err := includePaths(path, dir)
		if err != nil {
			return err
		}
		for _, include := range includes {
			if err := walk(include.path, include.workingDir); err != nil {
				return err
			}
		}
		return nil
	}
	for _, file := range files {
		if err := walk(file, workingDir); err != nil {
			return nil, err
		}
	}
	return result, nil
}

type includeTarget struct {
	path       string
	workingDir string
}

// includePaths reads one Compose file and returns the absolute paths its
// include entries point at, each with the working directory compose-go gives
// the included files.
func includePaths(path string, workingDir string) ([]includeTarget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Compose file %s: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse Compose file %s: %w", path, err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must contain one mapping document", path)
	}
	var includes *yaml.Node
	root := document.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "include" {
			includes = root.Content[i+1]
		}
	}
	if includes == nil {
		return nil, nil
	}
	var targets []includeTarget
	add := func(raw, projectDir string) {
		resolved := raw
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(workingDir, resolved)
		}
		resolved = filepath.Clean(resolved)
		if projectDir == "" {
			projectDir = filepath.Dir(resolved)
		}
		targets = append(targets, includeTarget{path: resolved, workingDir: projectDir})
	}
	switch includes.Kind {
	case yaml.SequenceNode:
		for _, entry := range includes.Content {
			switch entry.Kind {
			case yaml.ScalarNode:
				add(entry.Value, "")
			case yaml.MappingNode:
				projectDir := ""
				var entryPaths []string
				for j := 0; j < len(entry.Content); j += 2 {
					key, value := entry.Content[j], entry.Content[j+1]
					switch key.Value {
					case "path":
						if value.Kind == yaml.SequenceNode {
							for _, p := range value.Content {
								entryPaths = append(entryPaths, p.Value)
							}
						} else {
							entryPaths = append(entryPaths, value.Value)
						}
					case "project_directory":
						if filepath.IsAbs(value.Value) {
							projectDir = filepath.Clean(value.Value)
						} else {
							projectDir = filepath.Clean(filepath.Join(workingDir, value.Value))
						}
					}
				}
				for _, p := range entryPaths {
					add(p, projectDir)
				}
			}
		}
	case yaml.ScalarNode:
		add(includes.Value, "")
	default:
		return nil, fmt.Errorf("%s: unsupported include shape", path)
	}
	return targets, nil
}

// IncludedModules returns the registered modules whose Compose file appears in
// the combination's include graph, in configuration order.
func IncludedModules(cfg *config.Config, repoRoot string, files []string, workingDir string) ([]config.ModuleConfig, error) {
	sources, err := SourceFiles(files, workingDir)
	if err != nil {
		return nil, err
	}
	inGraph := map[string]bool{}
	for _, source := range sources {
		inGraph[source] = true
	}
	var modules []config.ModuleConfig
	for _, module := range cfg.Modules {
		if inGraph[filepath.Join(repoRoot, module.Compose)] {
			modules = append(modules, module)
		}
	}
	return modules, nil
}

// Variables reads a dotenv file and returns its declared variable names.
// A missing file reports an empty set so callers decide whether absence matters.
func Variables(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	values, err := dotenv.ParseWithLookup(bytes.NewReader(data), func(string) (string, bool) { return "", false })
	if err != nil {
		return nil, fmt.Errorf("parse env file %s: %w", path, err)
	}
	names := map[string]bool{}
	for name := range values {
		names[name] = true
	}
	return names, nil
}

// FileExists reports whether path is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
