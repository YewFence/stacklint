package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// DeploymentConfig describes which stacks are alternative deployments.
type DeploymentConfig struct {
	Alternatives []AlternativeGroup `toml:"alternatives"`
}

// AlternativeGroup permits at most one member in a deployment environment.
type AlternativeGroup struct {
	Name   string   `toml:"name"`
	Stacks []string `toml:"stacks"`
}

func (c DeploymentConfig) validate() error {
	names := map[string]bool{}
	members := map[string]bool{}
	for i, group := range c.Alternatives {
		label := fmt.Sprintf("deployment.alternatives[%d]", i)
		if strings.TrimSpace(group.Name) == "" || names[group.Name] {
			return fmt.Errorf("%s: name must be nonempty and unique", label)
		}
		names[group.Name] = true
		if len(group.Stacks) < 2 {
			return fmt.Errorf("%s: at least two stacks are required", label)
		}
		for _, stack := range group.Stacks {
			if !filepath.IsLocal(stack) || filepath.ToSlash(filepath.Clean(stack)) != stack || stack == "." {
				return fmt.Errorf("%s: stack %q must be a canonical repository-relative directory", label, stack)
			}
			if members[stack] {
				return fmt.Errorf("%s: stack %q belongs to more than one alternative entry", label, stack)
			}
			members[stack] = true
		}
	}
	return nil
}

// CanCoexist reports whether two stacks can occur in the same environment.
// A stack always coexists with itself so internal rules still apply.
func (c DeploymentConfig) CanCoexist(a, b string) bool {
	if a == b {
		return true
	}
	for _, group := range c.Alternatives {
		if slices.Contains(group.Stacks, a) && slices.Contains(group.Stacks, b) {
			return false
		}
	}
	return true
}
