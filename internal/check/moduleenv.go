package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YewFence/stacklint/internal/wiring"
)

// checkModuleEnvironment enforces module required_environment contracts: for
// every included module, the configured service's rendered environment must
// have non-empty values for every variable with the configured prefix, and the
// uninterpolated view must declare at least one such variable.
// It runs once per stack, over the union of all combinations' include graphs.
func checkModuleEnvironment(c *ruleContext) []Diagnostic {
	if c.combo.Variant != "base" {
		return nil
	}
	included, err := wiring.IncludedModules(c.cfg, c.repoRoot, c.allFiles(), c.combo.Stack.Dir)
	if err != nil {
		return nil // include graph errors are reported by the wiring rule
	}
	var diagnostics []Diagnostic
	for _, module := range included {
		req := module.RequiredEnvironment
		if req == nil {
			continue
		}
		services, _ := c.render.Raw["services"].(map[string]any)
		rawService, _ := services[req.Service].(map[string]any)
		rawEnv, _ := rawService["environment"].(map[string]any)
		var declared []string
		for name := range rawEnv {
			if strings.HasPrefix(name, req.Prefix) {
				declared = append(declared, name)
			}
		}
		if len(declared) == 0 {
			diagnostics = append(diagnostics, c.diagnose("module_environment", req.Service,
				fmt.Sprintf("service %s must declare %s* environment variables", req.Service, req.Prefix),
				fmt.Sprintf("Declare the variables the %s module documents in its wiring template.", filepath.Dir(module.Compose))))
			continue
		}
		service, ok := c.render.Project.Services[req.Service]
		if !ok {
			diagnostics = append(diagnostics, c.diagnose("module_environment", req.Service,
				fmt.Sprintf("rendered service %s is missing", req.Service),
				"Check the module include and profile configuration."))
			continue
		}
		var missing []string
		for _, name := range declared {
			value, present := service.Environment[name]
			if !present || value == nil || strings.TrimSpace(*value) == "" {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			diagnostics = append(diagnostics, c.diagnose("module_environment", req.Service,
				fmt.Sprintf("module environment variables are missing or empty after rendering: %s", strings.Join(sortedStrings(missing), ", ")),
				"Provide the values through the stack's wiring file or static render inputs."))
		}
	}
	return diagnostics
}
