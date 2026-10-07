package check

import "strings"

// checkStructure requires a non-empty top-level name and a non-empty services
// mapping. The name must be checked in the uninterpolated view because
// compose-go derives one from the directory when it is absent.
func checkStructure(c *ruleContext) []Diagnostic {
	var diagnostics []Diagnostic
	name, _ := c.render.Raw["name"].(string)
	if strings.TrimSpace(name) == "" {
		diagnostics = append(diagnostics, c.diagnose("structure", "",
			"rendered config is missing top-level name",
			"Add a top-level name to the stack's Compose file."))
	}
	if len(c.render.Project.Services) == 0 {
		diagnostics = append(diagnostics, c.diagnose("structure", "",
			"rendered config is missing non-empty services mapping",
			"Declare at least one service in the stack's Compose files."))
	}
	return diagnostics
}
