package envtemplate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/wiring"
	"github.com/compose-spec/compose-go/v2/dotenv"
	composeloader "github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"
	"go.yaml.in/yaml/v3"
)

type sourceValue struct {
	File  string
	Value string
}
type sourceFile struct {
	Path         string
	Document     *yaml.Node
	Sources      map[string]sourceValue
	Dependencies map[string]*Behavior
	EnvFile      bool
}

type graph struct {
	cfg      *config.Config
	repoRoot string
	target   Target
	sources  []sourceFile
	pool     map[string]string
	warnings []string
}

func newGraph(cfg *config.Config, repoRoot string, target Target, excluded []string) (*graph, error) {
	pool, err := probeEnvironment(target, excluded)
	if err != nil {
		return nil, err
	}
	return &graph{cfg: cfg, repoRoot: repoRoot, target: target, pool: pool}, nil
}

// probeEnvironment builds the interpolation pool: the combination's own
// template via dotenv (live values of exempted, hand-maintained templates),
// unless excluded as a generated output, plus @env-example marker values from
// the include graph, which take precedence.
func probeEnvironment(target Target, excluded []string) (map[string]string, error) {
	skip := map[string]bool{}
	for _, path := range excluded {
		skip[filepath.Clean(path)] = true
	}
	pool := map[string]string{}
	if !skip[filepath.Clean(target.Output)] {
		err := regularFile(target.Output)
		switch {
		case err == nil:
			data, err := os.ReadFile(target.Output)
			if err != nil {
				return nil, fmt.Errorf("read static input %s: %w", target.Output, err)
			}
			values, err := dotenv.ParseWithLookup(bytes.NewReader(data), func(string) (string, bool) { return "", false })
			if err != nil {
				return nil, fmt.Errorf("cannot parse static input %s", target.Output)
			}
			for name, value := range values {
				pool[name] = value
			}
		case os.IsNotExist(err):
		default:
			return nil, fmt.Errorf("static input %s: %w", target.Output, err)
		}
	}
	examples, err := wiring.ExampleValues(target.Files, target.Stack.Dir)
	if err != nil {
		return nil, err
	}
	for name, value := range examples {
		pool[name] = value
	}
	return pool, nil
}

func readDocument(path string) (*yaml.Node, error) {
	if err := regularFile(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("cannot parse Compose file %s", path)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must contain one mapping document", path)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s must contain one mapping document", path)
	}
	var decoded map[string]any
	if err := document.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("cannot decode Compose file %s", path)
	}
	return &document, nil
}

func regularFile(path string) error {
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if filepath.Clean(actual) != filepath.Clean(absolute) {
		return fmt.Errorf("refusing symlink path %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func resolvedNode(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

func field(node *yaml.Node, name string) *yaml.Node {
	node = resolvedNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return resolvedNode(node.Content[i+1])
		}
	}
	return nil
}

func pathsNode(node *yaml.Node) ([]string, error) {
	node = resolvedNode(node)
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" || node.Value == "" {
			return nil, fmt.Errorf("expected nonempty path string")
		}
		refs := template.ExtractVariables(map[string]any{"path": node.Value}, nil)
		if len(refs) != 0 || strings.Contains(node.Value, "$") {
			return nil, fmt.Errorf("dynamic file graph paths are not supported")
		}
		if strings.Contains(node.Value, "://") {
			return nil, fmt.Errorf("remote Compose resources are not supported")
		}
		return []string{node.Value}, nil
	case yaml.SequenceNode:
		var paths []string
		for _, child := range node.Content {
			if resolvedNode(child).Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("path list entries must be strings")
			}
			part, err := pathsNode(child)
			if err != nil {
				return nil, err
			}
			paths = append(paths, part...)
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("path list must not be empty")
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("expected a path or path list")
	}
}

func absolutePath(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(dir, path))
}

func (g *graph) inspect(files []string, workingDir string, env map[string]string, sources map[string]sourceValue, ancestors map[string]bool) error {
	for _, path := range files {
		path = filepath.Clean(path)
		if ancestors[path] {
			return fmt.Errorf("include cycle at %s", g.relative(path))
		}
		document, err := readDocument(path)
		if err != nil {
			return err
		}
		if err := validateNodes(document, nil, map[*yaml.Node]bool{}); err != nil {
			return fmt.Errorf("%s: %w", g.relative(path), err)
		}
		g.sources = append(g.sources, sourceFile{Path: path, Document: document, Sources: sources})
		includes := field(document.Content[0], "include")
		if includes == nil {
			continue
		}
		if includes.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: include must be a list", g.relative(path))
		}
		nextAncestors := map[string]bool{}
		for key, value := range ancestors {
			nextAncestors[key] = value
		}
		nextAncestors[path] = true
		for _, entry := range includes.Content {
			entry = resolvedNode(entry)
			var paths, envPaths []string
			projectDir := ""
			switch entry.Kind {
			case yaml.ScalarNode:
				paths, err = pathsNode(entry)
			case yaml.MappingNode:
				for j := 0; j < len(entry.Content); j += 2 {
					key := entry.Content[j].Value
					if key != "path" && key != "project_directory" && key != "env_file" {
						return fmt.Errorf("%s: unknown include key %s", g.relative(path), key)
					}
				}
				paths, err = pathsNode(field(entry, "path"))
				if err != nil {
					return fmt.Errorf("%s include.path: %w", g.relative(path), err)
				}
				envPaths, err = pathsNode(field(entry, "env_file"))
				if err != nil {
					return fmt.Errorf("%s include.env_file: %w", g.relative(path), err)
				}
				if directory := field(entry, "project_directory"); directory != nil {
					dirs, directoryErr := pathsNode(directory)
					if directoryErr != nil || len(dirs) != 1 {
						return fmt.Errorf("%s: include.project_directory must be a fixed path", g.relative(path))
					}
					projectDir = absolutePath(workingDir, dirs[0])
				}
			default:
				return fmt.Errorf("%s: unsupported include shape", g.relative(path))
			}
			if err != nil {
				return fmt.Errorf("%s include: %w", g.relative(path), err)
			}
			if len(paths) == 0 {
				return fmt.Errorf("%s: include.path is required", g.relative(path))
			}
			for j, includePath := range paths {
				paths[j] = absolutePath(workingDir, includePath)
			}
			if projectDir == "" {
				projectDir = filepath.Dir(paths[0])
			}
			childEnv := cloneMapping(env)
			childSources := map[string]sourceValue{}
			for name, value := range sources {
				childSources[name] = value
			}
			for _, envPath := range envPaths {
				if envPath == "/dev/null" {
					continue
				}
				envPath = absolutePath(workingDir, envPath)
				if !g.allowedEnv(envPath, paths, workingDir) {
					return fmt.Errorf("include env_file %s is not authorized; register env.include_env_files or module.wiring_file", g.relative(envPath))
				}
				values, dependencies, err := g.readWiring(envPath, env, childEnv)
				if err != nil {
					return err
				}
				for name, value := range values {
					if _, exists := env[name]; !exists {
						childEnv[name] = value
					}
					childSources[name] = sourceValue{File: g.relative(envPath), Value: value}
				}
				g.sources = append(g.sources, sourceFile{Path: envPath, EnvFile: true, Dependencies: dependencies, Sources: sources})
			}
			if len(envPaths) == 0 {
				implicit := filepath.Join(projectDir, ".env")
				if _, statErr := os.Stat(implicit); statErr == nil {
					if err := regularFile(implicit); err != nil {
						return err
					}
					g.warnings = append(g.warnings, fmt.Sprintf("include may read implicit %s; its values are not template defaults or approved wiring sources", g.relative(implicit)))
				} else if !os.IsNotExist(statErr) {
					return statErr
				}
			}
			if err := g.inspect(paths, projectDir, childEnv, childSources, nextAncestors); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *graph) allowedEnv(path string, included []string, workingDir string) bool {
	for _, allowed := range g.cfg.Env.IncludeEnvFiles {
		if absolutePath(g.repoRoot, allowed) == path {
			return true
		}
	}
	for _, module := range g.cfg.Modules {
		if module.WiringFile == "" {
			continue
		}
		for _, compose := range included {
			if absolutePath(g.repoRoot, module.Compose) == compose && absolutePath(workingDir, module.WiringFile) == path {
				return true
			}
		}
	}
	return false
}

func (g *graph) readWiring(path string, parentEnv, previousEnv map[string]string) (map[string]string, map[string]*Behavior, error) {
	if err := regularFile(path); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	consulted := map[string]bool{}
	missing := map[string]bool{}
	values, err := dotenv.ParseWithLookup(bytes.NewReader(data), func(name string) (string, bool) {
		consulted[name] = true
		value, exists := parentEnv[name]
		if !exists {
			value, exists = previousEnv[name]
		}
		if !exists {
			missing[name] = true
		}
		return value, exists
	})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot parse include env_file %s (value details suppressed)", g.relative(path))
	}
	dependencies := map[string]bool{}
	for name := range consulted {
		if _, internal := values[name]; !internal {
			if missing[name] {
				return nil, nil, fmt.Errorf("include env_file %s requires static placeholder for %s", g.relative(path), name)
			}
			dependencies[name] = true
		}
	}
	behaviors := map[string]*Behavior{}
	summary := template.ExtractVariables(map[string]any{"dotenv": string(data)}, nil)
	for _, name := range sortedNames(dependencies) {
		behaviors[name] = nil
		if len(dependencies) != 1 || !summary[name].Required {
			continue
		}
		probe := func(present bool, value string) error {
			_, err := dotenv.ParseWithLookup(bytes.NewReader(data), func(key string) (string, bool) {
				if key == name {
					return value, present
				}
				if _, internal := values[key]; internal {
					return "", false
				}
				return "", false
			})
			return err
		}
		var required *template.MissingRequiredError
		if absentErr := probe(false, ""); errors.As(absentErr, &required) && required.Variable == name {
			behaviors[name] = &Behavior{Required: true, Nonempty: probe(true, "") != nil}
		}
	}
	return values, behaviors, nil
}

func (g *graph) relative(path string) string {
	rel, err := filepath.Rel(g.repoRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (g *graph) load(ctx context.Context) (map[string]any, error) {
	if err := g.inspect(g.target.Files, g.target.Stack.Dir, g.pool, map[string]sourceValue{}, map[string]bool{}); err != nil {
		return nil, err
	}
	details := types.ConfigDetails{WorkingDir: g.target.Stack.Dir, ConfigFiles: types.ToConfigFiles(g.target.Files), Environment: types.Mapping(cloneMapping(g.pool))}
	model, err := composeloader.LoadModelWithContext(ctx, details, func(options *composeloader.Options) {
		options.SkipInterpolation = true
		options.SkipValidation = true
		options.ResolvePaths = false
		options.SkipNormalization = true
		options.SkipDefaultValues = true
		options.SetProjectName("stacklint-probe", true)
		options.Profiles = []string{"*"}
	})
	// Loader errors can contain values obtained from an implicit include .env.
	if err != nil {
		return nil, fmt.Errorf("compose-go could not load %s variant %s; check Compose structure and include inputs (value details suppressed)", g.target.Stack.RelPath, g.target.Variant)
	}
	return model, nil
}

func validateNodes(node *yaml.Node, path []string, ancestors map[*yaml.Node]bool) error {
	if ancestors[node] {
		return fmt.Errorf("recursive YAML alias at %s", strings.Join(path, "."))
	}
	ancestors[node] = true
	defer delete(ancestors, node)
	if node.Kind == yaml.AliasNode {
		return validateNodes(node.Alias, path, ancestors)
	}
	if node.Tag != "" && !strings.HasPrefix(node.Tag, "!!") && node.Tag != "!reset" && node.Tag != "!override" {
		return fmt.Errorf("unsupported YAML tag %s at %s", node.Tag, strings.Join(path, "."))
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Tag != "!!str" && key.Tag != "!!merge" {
				return fmt.Errorf("mapping keys must be strings at %s", strings.Join(path, "."))
			}
			if key.Value == "extends" && len(path) == 2 && path[0] == "services" {
				return fmt.Errorf("extends is not supported at %s", strings.Join(appendPath(path, key.Value), "."))
			}
			if key.Value == "label_file" && len(path) == 2 && path[0] == "services" {
				return fmt.Errorf("label_file input discovery is not supported at %s", strings.Join(appendPath(path, key.Value), "."))
			}
			if err := validateNodes(node.Content[i+1], appendPath(path, key.Value), ancestors); err != nil {
				return err
			}
		}
	} else {
		for _, child := range node.Content {
			if err := validateNodes(child, path, ancestors); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendPath(path []string, part string) []string {
	return append(append([]string(nil), path...), part)
}
