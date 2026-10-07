package check

import (
	"fmt"
	"os"
	"strings"

	"github.com/YewFence/stacklint/internal/wiring"
	"go.yaml.in/yaml/v3"
)

// checkVolumeNames requires every rendered top-level volume to be an object
// with a non-empty name that is explicitly written in a source file. Compose's
// auto-generated project-prefixed names do not count.
func checkVolumeNames(c *ruleContext) []Diagnostic {
	if len(c.render.Project.Volumes) == 0 {
		return nil
	}
	explicit, err := explicitVolumeNames(c.combo.Files, c.combo.Stack.Dir)
	if err != nil {
		return []Diagnostic{c.diagnose("volume_name", "", fmt.Sprintf("could not scan source files: %v", err),
			"Fix the Compose files so volume declarations can be read.")}
	}
	var diagnostics []Diagnostic
	for _, key := range sortedKeys(c.render.Project.Volumes) {
		volume := c.render.Project.Volumes[key]
		if strings.TrimSpace(volume.Name) == "" {
			diagnostics = append(diagnostics, c.diagnose("volume_name", "",
				fmt.Sprintf("top-level volume %s is missing rendered name", key),
				"Set an explicit name on the volume declaration."))
		}
		hasName, declared := explicit[key]
		if !declared {
			diagnostics = append(diagnostics, c.diagnose("volume_name", "",
				fmt.Sprintf("top-level volume %s must be declared with an explicit name", key),
				"Declare the volume with an explicit name attribute in the source Compose file."))
		} else if !hasName {
			diagnostics = append(diagnostics, c.diagnose("volume_name", "",
				fmt.Sprintf("top-level volume %s must set explicit name", key),
				"Add a name attribute to the volume declaration."))
		}
	}
	return diagnostics
}


// explicitVolumeNames scans every Compose file in the include graph with
// yaml.v3 and records, per top-level volume key, whether any file writes an
// explicit non-empty name for it.
func explicitVolumeNames(files []string, workingDir string) (map[string]bool, error) {
	sources, err := wiring.SourceFiles(files, workingDir)
	if err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("parse Compose file %s: %w", source, err)
		}
		if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
			continue
		}
		var volumes *yaml.Node
		for i := 0; i < len(document.Content[0].Content); i += 2 {
			if document.Content[0].Content[i].Value == "volumes" {
				volumes = document.Content[0].Content[i+1]
			}
		}
		if volumes == nil || volumes.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i < len(volumes.Content); i += 2 {
			key := volumes.Content[i].Value
			value := volumes.Content[i+1]
			if _, declared := result[key]; !declared {
				result[key] = false
			}
			if value.Kind == yaml.MappingNode && hasExplicitName(value) {
				result[key] = true
			}
		}
	}
	return result, nil
}

func hasExplicitName(mapping *yaml.Node) bool {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "name" {
			value := mapping.Content[i+1]
			return value.Kind == yaml.ScalarNode && strings.TrimSpace(value.Value) != ""
		}
	}
	return false
}
