package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
)

// checkEnvFiles flags service-level env_file entries that resolve to the stack
// directory's .env, unless a matching [[env_file]] approval exists. Entries
// pointing at other files are not constrained by this rule.
func checkEnvFiles(c *ruleContext) []Diagnostic {
	services, ok := c.render.Raw["services"].(map[string]any)
	if !ok {
		return nil
	}
	projectEnv := filepath.Join(c.combo.Stack.Dir, ".env")
	var diagnostics []Diagnostic
	for _, serviceName := range sortedKeys(services) {
		service, ok := services[serviceName].(map[string]any)
		if !ok {
			continue
		}
		for _, path := range envFilePaths(service["env_file"]) {
			if filepath.Clean(path) != projectEnv {
				continue
			}
			compose := c.composeRel()
			approval := findEnvFileApproval(c.approvals, compose, serviceName, ".env")
			if approval != nil {
				c.reviewed.envFiles[envFileKey(compose, serviceName, ".env")] = true
				continue
			}
			diagnostics = append(diagnostics, c.diagnose("env_file", serviceName,
				fmt.Sprintf("service %s uses project .env through env_file", serviceName),
				"Declare the service variables in environment, or add an [[env_file]] approval with the exact compose/service/path and a reason in approvals.toml."))
		}
	}
	return diagnostics
}

// envFilePaths normalizes env_file entries (string or mapping form) into
// resolved paths. The uninterpolated model carries loader-resolved absolute
// paths; unresolved entries are ignored.
func envFilePaths(value any) []string {
	var entries []any
	switch typed := value.(type) {
	case string:
		entries = []any{typed}
	case map[string]any:
		entries = []any{typed}
	case []any:
		entries = typed
	default:
		return nil
	}
	var paths []string
	for _, entry := range entries {
		switch typed := entry.(type) {
		case string:
			if strings.TrimSpace(typed) != "" && filepath.IsAbs(typed) {
				paths = append(paths, typed)
			}
		case map[string]any:
			if path, ok := typed["path"].(string); ok && strings.TrimSpace(path) != "" && filepath.IsAbs(path) {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func findEnvFileApproval(approvals *config.Approvals, compose, service, path string) *config.EnvFileApproval {
	for i := range approvals.EnvFiles {
		if approvals.EnvFiles[i].Compose == compose && approvals.EnvFiles[i].Service == service && approvals.EnvFiles[i].Path == path {
			return &approvals.EnvFiles[i]
		}
	}
	return nil
}

func sortedKeys[V any](mapping map[string]V) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
