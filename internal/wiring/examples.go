package wiring

import (
	"bytes"
	"fmt"
	"os"

	"github.com/YewFence/stacklint/internal/markers"
	"go.yaml.in/yaml/v3"
)

// ExampleValues returns the @env-example declarations found in the comments of
// every Compose file in the combination's include graph. Declaring the same
// variable with conflicting values fails; identical duplicates collapse.
// Include-graph walk failures yield an empty result: markers are best-effort
// render inputs, and the Compose load that follows reports graph errors
// authoritatively. Malformed markers are likewise ignored here; env template
// generation validates them.
func ExampleValues(files []string, workingDir string) (map[string]string, error) {
	sources, err := SourceFiles(files, workingDir)
	if err != nil {
		return map[string]string{}, nil
	}
	values := map[string]string{}
	for _, path := range sources {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read Compose file %s: %w", path, err)
		}
		var document yaml.Node
		if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&document); err != nil {
			return nil, fmt.Errorf("parse Compose file %s: %w", path, err)
		}
		if err := collectExamples(&document, values, map[*yaml.Node]bool{}); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return values, nil
}

func collectExamples(node *yaml.Node, values map[string]string, seen map[*yaml.Node]bool) error {
	if node == nil || seen[node] {
		return nil
	}
	seen[node] = true
	for _, comment := range []string{node.HeadComment, node.LineComment, node.FootComment} {
		for _, line := range splitLines(comment) {
			kind, name, text, ok := markers.Parse(line)
			if !ok || kind != markers.KindExample {
				continue
			}
			if existing, exists := values[name]; exists && existing != text {
				return fmt.Errorf("conflicting @env-example values for %s", name)
			}
			values[name] = text
		}
	}
	for _, child := range node.Content {
		if err := collectExamples(child, values, seen); err != nil {
			return err
		}
	}
	if node.Kind == yaml.AliasNode {
		return collectExamples(node.Alias, values, seen)
	}
	return nil
}

func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	return append(lines, text[start:])
}
