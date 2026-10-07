package check

import (
	"fmt"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/interp"
)

// checkBindAddress reads host IP interpolation variables from the
// uninterpolated view's port declarations (short syntax prefix or long syntax
// host_ip). Only the configured default variable is allowed without an
// approval; other variables need a [[bind_address]] approval whose variable
// set matches exactly.
func checkBindAddress(c *ruleContext) []Diagnostic {
	services, ok := c.render.Raw["services"].(map[string]any)
	if !ok {
		return nil
	}
	defaultVariable := c.cfg.BindAddress.DefaultVariable
	var diagnostics []Diagnostic
	for _, serviceName := range sortedKeys(services) {
		service, ok := services[serviceName].(map[string]any)
		if !ok {
			continue
		}
		ports, ok := service["ports"].([]any)
		if !ok {
			continue
		}
		variables := map[string]bool{}
		for _, port := range ports {
			for _, name := range bindAddressVariables(port) {
				variables[name] = true
			}
		}
		nonstandard := map[string]bool{}
		for name := range variables {
			if name != defaultVariable {
				nonstandard[name] = true
			}
		}
		if len(nonstandard) == 0 {
			continue
		}
		compose := c.composeRel()
		approval := findBindApproval(c.approvals, compose, serviceName)
		if approval == nil {
			diagnostics = append(diagnostics, c.diagnose("bind_address", serviceName,
				fmt.Sprintf("service %s binds host IP with %s instead of %s", serviceName, formatVariables(nonstandard), defaultVariable),
				fmt.Sprintf("Use %s, or add a [[bind_address]] approval with the exact variables and a reason in approvals.toml.", defaultVariable)))
			continue
		}
		c.reviewed.binds[bindKey(compose, serviceName)] = true
		if !equalStringSets(nonstandard, approval.Variables) {
			diagnostics = append(diagnostics, c.diagnose("bind_address", serviceName,
				fmt.Sprintf("service %s binds host IP with %s instead of %s; existing [[bind_address]] approval permits %s", serviceName, formatVariables(nonstandard), defaultVariable, strings.Join(sortedStrings(approval.Variables), ", ")),
				"Update the approval in approvals.toml after reviewing the changed variables."))
		}
	}
	return diagnostics
}

// bindAddressVariables extracts host IP interpolation variables from one port
// entry via the interpolation analyzer, never regexes.
func bindAddressVariables(port any) []string {
	switch typed := port.(type) {
	case string:
		// Short syntax: the host IP prefix ends at the first colon outside any
		// interpolation expression.
		prefix := hostPrefix(typed)
		if prefix == "" {
			return nil
		}
		names, err := interp.Names(prefix)
		if err != nil {
			return nil
		}
		return names
	case map[string]any:
		hostIP, ok := typed["host_ip"].(string)
		if !ok || !strings.Contains(hostIP, "$") {
			return nil
		}
		names, err := interp.Names(hostIP)
		if err != nil {
			return nil
		}
		return names
	}
	return nil
}

// hostPrefix returns the leading interpolation expression of a short-syntax
// port string when it starts with a variable, e.g. "${BIND_ADDRESS}" in
// "${BIND_ADDRESS}:8080:80". Literal IP prefixes yield "".
func hostPrefix(port string) string {
	if !strings.HasPrefix(port, "$") {
		return ""
	}
	depth := 0
	for i := 0; i < len(port); i++ {
		switch port[i] {
		case '{':
			if i > 0 && port[i-1] == '$' {
				depth++
			}
		case '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return port[:i]
			}
		}
	}
	return ""
}

func formatVariables(variables map[string]bool) string {
	return strings.Join(sortedKeys(variables), ", ")
}

func equalStringSets(set map[string]bool, list []string) bool {
	if len(set) != len(list) {
		return false
	}
	for _, item := range list {
		if !set[item] {
			return false
		}
	}
	return true
}

func findBindApproval(approvals *config.Approvals, compose, service string) *config.BindApproval {
	for i := range approvals.Binds {
		if approvals.Binds[i].Compose == compose && approvals.Binds[i].Service == service {
			return &approvals.Binds[i]
		}
	}
	return nil
}
