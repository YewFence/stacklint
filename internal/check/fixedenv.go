package check

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YewFence/stacklint/internal/interp"
	"github.com/YewFence/stacklint/internal/wiring"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"go.yaml.in/yaml/v3"
)

// checkFixedEnv reports keys of the configured fixed-value env files
// (env.fixed_files, e.g. a git-tracked .env.conf merged into the deployment
// .env by repository tooling) that no Compose interpolation in any of the
// stack's combinations references. Such entries are dead: renamed or removed
// references leave them silently shipping to every deployment host.
//
// The rule compares key sets only; the values never enter the interpolation
// environment. Providers subtracted from the reference set: module wiring
// files (their declared keys and the external variables their values
// reference, which deployment fills from .env) and bind_address.default_variable.
// It runs once per stack on the base combination, over the union of all
// combinations' include graphs.
func checkFixedEnv(c *ruleContext) []Diagnostic {
	if c.combo.Variant != "base" || len(c.cfg.Env.FixedFiles) == 0 {
		return nil
	}
	// Stacks with an env-file-passthrough exemption consume their fixed env
	// through env_file: .env rather than interpolation, so the rule does not
	// apply to them.
	for _, exemption := range c.approvals.Exemptions {
		if exemption.Stack == c.combo.Stack.RelPath && exemption.Kind == "env-file-passthrough" {
			return nil
		}
	}
	var diagnostics []Diagnostic
	for _, name := range c.cfg.Env.FixedFiles {
		path := filepath.Join(c.combo.Stack.Dir, name)
		keys, err := wiring.Variables(path)
		if err != nil {
			diagnostics = append(diagnostics, c.diagnose("fixed_env", "",
				fmt.Sprintf("could not parse %s: %v", name, err),
				"Fix the dotenv syntax so the file can be merged into the deployment .env."))
			continue
		}
		if len(keys) == 0 {
			continue
		}
		referenced, err := interpolationVariables(c)
		if err != nil {
			diagnostics = append(diagnostics, c.diagnose("fixed_env", "",
				fmt.Sprintf("could not analyze Compose interpolation: %v", err), ""))
			continue
		}
		var dead []string
		for _, key := range sortedKeys(keys) {
			if !referenced[key] {
				dead = append(dead, key)
			}
		}
		if len(dead) > 0 {
			diagnostics = append(diagnostics, c.diagnose("fixed_env", "",
				fmt.Sprintf("%s declares keys no Compose interpolation references: %s", name, joinQuoted(dead)),
				"Remove the dead entries, or reference the keys in the stack's Compose files."))
		}
	}
	return diagnostics
}

// interpolationVariables returns the union of variables referenced by string
// values anywhere in every combination's include graph, plus pass-through
// environment keys (null-valued `KEY:` entries and `- KEY` list entries) that
// deployment fills from the merged .env. Keys of mappings are not interpolated
// by compose-go and are ignored.
func interpolationVariables(c *ruleContext) (map[string]bool, error) {
	sources, err := wiring.SourceFiles(c.allFiles(), c.combo.Stack.Dir)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("parse Compose file %s: %w", source, err)
		}
		if err := collectReferenced(&document, referenced); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
	}
	// Wiring files provide their declared keys and may derive values from
	// external variables that deployment fills from .env.
	for _, file := range wiringFiles(c) {
		declared, dependencies, err := wiringKeyReferences(file)
		if err != nil {
			return nil, err
		}
		for name := range declared {
			referenced[name] = true
		}
		for _, name := range dependencies {
			referenced[name] = true
		}
	}
	if c.cfg.BindAddress.DefaultVariable != "" {
		referenced[c.cfg.BindAddress.DefaultVariable] = true
	}
	return referenced, nil
}

// wiringKeyReferences reads a wiring dotenv file once and returns its
// declared keys together with the external variables its values reference:
// everything the interpolation lookup consults that the file itself does not
// declare.
func wiringKeyReferences(path string) (map[string]bool, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	consulted := map[string]bool{}
	values, err := dotenv.ParseWithLookup(bytes.NewReader(data), func(name string) (string, bool) {
		consulted[name] = true
		return "", false
	})
	if err != nil {
		return nil, nil, fmt.Errorf("parse env file %s: %w", path, err)
	}
	var dependencies []string
	for name := range consulted {
		if _, internal := values[name]; !internal {
			dependencies = append(dependencies, name)
		}
	}
	declared := map[string]bool{}
	for name := range values {
		declared[name] = true
	}
	return declared, dependencies, nil
}

// wiringFiles returns the existing wiring files of the modules the stack
// includes, absolute in the stack directory.
func wiringFiles(c *ruleContext) []string {
	included, err := wiring.IncludedModules(c.cfg, c.repoRoot, c.allFiles(), c.combo.Stack.Dir)
	if err != nil {
		return nil // include graph errors are reported by the wiring rule
	}
	var files []string
	for _, module := range included {
		if module.WiringFile == "" {
			continue
		}
		path := filepath.Join(c.combo.Stack.Dir, module.WiringFile)
		if wiring.FileExists(path) {
			files = append(files, path)
		}
	}
	return files
}

// collectReferenced walks a YAML document's string scalar values and records
// the interpolation variable names they reference, plus pass-through keys
// from environment/build-args containers (null-valued `KEY:` entries and
// `- KEY` list entries without `=`). Mapping keys are not interpolated by
// compose-go and are ignored; non-string scalars never interpolate.
func collectReferenced(node *yaml.Node, referenced map[string]bool) error {
	return collectReferencedAt(node, nil, referenced)
}

// passthroughContainer reports whether the given path is a services.*.environment
// or services.*.build.args mapping/sequence, matching envtemplate/scan.go.
func passthroughContainer(path []string) bool {
	return len(path) == 3 && (path[0] == "services" || path[0] == "jobs") && path[2] == "environment" ||
		len(path) == 4 && path[0] == "services" && path[2] == "build" && path[3] == "args"
}

// resolvedNode follows alias nodes to their target, mirroring envtemplate's
// resolvedNode.
func resolvedNode(node *yaml.Node) *yaml.Node {
	for node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

func collectReferencedAt(node *yaml.Node, path []string, referenced map[string]bool) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return collectReferencedAt(node.Alias, path, referenced)
	}
	switch node.Kind {
	case yaml.DocumentNode:
		return collectReferencedAt(node.Content[0], path, referenced)
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return nil
		}
		value := node.Value
		// environment/build args list entries: `- KEY` or `- KEY=value`
		// These appear as SequenceNode children of an environment/build.args mapping.
		// We distinguish them from MappingNode values (KEY: value) by checking if
		// the current path's last segment is a numeric index (SequenceNode child).
		if len(path) > 0 {
			lastSeg := path[len(path)-1]
			if _, err := strconv.Atoi(lastSeg); err == nil && passthroughContainer(path[:len(path)-1]) {
				if !strings.Contains(value, "=") {
					if !strings.Contains(value, "$") {
						referenced[value] = true
					}
					return nil
				}
				if _, expr, hasEquals := strings.Cut(value, "="); hasEquals {
					value = expr
				}
			}
		}
		if !strings.Contains(value, "$") {
			return nil
		}
		names, err := interp.Names(value)
		if err != nil {
			return err
		}
		for _, name := range names {
			referenced[name] = true
		}
		return nil
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			key := node.Content[i-1]
			child := node.Content[i]
			if key.Tag == "!!merge" {
				// A merge key's value is an alias (or a sequence of aliases)
				// whose target mapping contents belong at the current path,
				// mirroring envtemplate's walk.
				targets := []*yaml.Node{child}
				if resolved := resolvedNode(child); resolved.Kind == yaml.SequenceNode {
					targets = resolved.Content
				}
				for _, target := range targets {
					if err := collectReferencedAt(resolvedNode(target), path, referenced); err != nil {
						return err
					}
				}
				continue
			}
			resolved := resolvedNode(child)
			childPath := append(path, key.Value)
			if passthroughContainer(path) && resolved.Kind != yaml.ScalarNode {
				return fmt.Errorf("environment/build args values must be scalars")
			}
			if passthroughContainer(path) && resolved.Tag == "!!null" {
				// `KEY:` pass-through entry
				referenced[key.Value] = true
			}
			if err := collectReferencedAt(child, childPath, referenced); err != nil {
				return err
			}
		}
		return nil
	case yaml.SequenceNode:
		for i, child := range node.Content {
			childPath := append(path, strconv.Itoa(i))
			resolved := resolvedNode(child)
			if passthroughContainer(path) && resolved.Kind != yaml.ScalarNode {
				return fmt.Errorf("environment/build args list entries must be scalars")
			}
			if err := collectReferencedAt(child, childPath, referenced); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func joinQuoted(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}
