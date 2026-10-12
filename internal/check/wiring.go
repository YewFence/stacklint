package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/wiring"
)

// checkWiring validates module wiring contracts: a stack including a registered
// module must carry the wiring file with exactly the template variables minus
// the optional ones; @env-example markers must not provide values for wiring
// variables; a wiring file without the module include is an orphan.
//
// Wiring is stack-level state, so the rule runs once per stack (on the base
// combination) over the union of all combinations' include graphs: a module
// included only by one variant still requires the wiring file.
func checkWiring(c *ruleContext) []Diagnostic {
	if c.combo.Variant != "base" {
		return nil
	}
	var diagnostics []Diagnostic
	var allFiles []string
	for _, combo := range c.allCombos {
		allFiles = append(allFiles, combo.Files...)
	}
	included, err := wiring.IncludedModules(c.cfg, c.repoRoot, allFiles, c.combo.Stack.Dir)
	if err != nil {
		return []Diagnostic{c.diagnose("wiring", "", fmt.Sprintf("could not walk include graph: %v", err),
			"Fix the include entries in the stack's Compose files.")}
	}
	includedByCompose := map[string]config.ModuleConfig{}
	for _, module := range included {
		includedByCompose[module.Compose] = module
	}

	exampleValues, err := wiring.ExampleValues(allFiles, c.combo.Stack.Dir)
	if err != nil {
		return []Diagnostic{c.diagnose("wiring", "", fmt.Sprintf("could not scan @env-example markers: %v", err),
			"Fix the conflicting markers in the stack's Compose files.")}
	}
	exampleNames := map[string]bool{}
	for name := range exampleValues {
		exampleNames[name] = true
	}

	for _, module := range included {
		if module.WiringFile == "" {
			continue
		}
		wiringPath := filepath.Join(c.combo.Stack.Dir, module.WiringFile)
		if !wiring.FileExists(wiringPath) {
			diagnostics = append(diagnostics, c.diagnose("wiring", "",
				fmt.Sprintf("shared module %s requires %s in the service directory", module.Compose, module.WiringFile),
				"Create the wiring file from the module's wiring template."))
			continue
		}
		wiringVars, err := wiring.Variables(wiringPath)
		if err != nil {
			diagnostics = append(diagnostics, c.diagnose("wiring", "", fmt.Sprintf("could not read %s: %v", module.WiringFile, err), ""))
			continue
		}
		templateVars, err := wiring.Variables(filepath.Join(c.repoRoot, filepath.Dir(module.Compose), module.WiringTemplate))
		if err != nil {
			diagnostics = append(diagnostics, c.diagnose("wiring", "", fmt.Sprintf("could not read module template for %s: %v", module.Compose, err), ""))
			continue
		}
		var extra, missing []string
		for name := range wiringVars {
			if !templateVars[name] {
				extra = append(extra, name)
			}
		}
		for name := range templateVars {
			if !wiringVars[name] && !containsString(module.OptionalWiringVariables, name) {
				missing = append(missing, name)
			}
		}
		if len(extra) > 0 {
			diagnostics = append(diagnostics, c.diagnose("wiring", "",
				fmt.Sprintf("%s declares variables missing from the module template: %s", module.WiringFile, strings.Join(sortedStrings(extra), ", ")),
				"Remove the extra variables or extend the module wiring template."))
		}
		if len(missing) > 0 {
			diagnostics = append(diagnostics, c.diagnose("wiring", "",
				fmt.Sprintf("%s is missing required wiring variables: %s", module.WiringFile, strings.Join(sortedStrings(missing), ", ")),
				"Add the missing variables from the module wiring template."))
		}
		if overlap := intersectKeys(exampleNames, wiringVars); len(overlap) > 0 {
			diagnostics = append(diagnostics, c.diagnose("wiring", "",
				fmt.Sprintf("@env-example must not provide values for %s wiring variables (the render environment wins over include env_file and would shadow them): %s", module.WiringFile, strings.Join(overlap, ", ")),
				"Remove the @env-example marker for the wiring variable."))
		}
	}

	// Orphan wiring files: present without the module include.
	for _, module := range c.cfg.Modules {
		if module.WiringFile == "" {
			continue
		}
		if _, included := includedByCompose[module.Compose]; included {
			continue
		}
		if wiring.FileExists(filepath.Join(c.combo.Stack.Dir, module.WiringFile)) {
			diagnostics = append(diagnostics, c.diagnose("wiring", "",
				fmt.Sprintf("service directory has orphan %s: its compose file does not include the shared module", module.WiringFile),
				"Remove the orphan file or include the module."))
		}
	}
	return diagnostics
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func intersectKeys(a, b map[string]bool) []string {
	var overlap []string
	for name := range a {
		if b[name] {
			overlap = append(overlap, name)
		}
	}
	return sortedStrings(overlap)
}
