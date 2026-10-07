package check

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
)

// publishedPort is one published host port usage within a combination.
type publishedPort struct {
	compose  string
	service  string
	hostIP   string
	port     int
	protocol string
}

// collectPublishedPorts gathers published ports from one combination's
// rendered model; compose-go has already normalized short syntax to long.
func collectPublishedPorts(c *ruleContext) []publishedPort {
	var ports []publishedPort
	compose := c.composeRel()
	for _, serviceName := range c.render.Project.ServiceNames() {
		service := c.render.Project.Services[serviceName]
		for _, port := range service.Ports {
			if port.Published == "" {
				continue
			}
			protocol := strings.ToLower(strings.TrimSpace(port.Protocol))
			if protocol == "" {
				protocol = "tcp"
			}
			hostIP := strings.TrimSpace(port.HostIP)
			if hostIP == "" {
				hostIP = "*"
			}
			for _, p := range expandPortValue(port.Published) {
				ports = append(ports, publishedPort{
					compose:  compose,
					service:  serviceName,
					hostIP:   hostIP,
					port:     p,
					protocol: protocol,
				})
			}
		}
	}
	return ports
}

// expandPortValue expands a published port value ("8080" or "8000-8010").
func expandPortValue(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if start, end, ok := strings.Cut(value, "-"); ok {
		lo, errLo := strconv.Atoi(strings.TrimSpace(start))
		hi, errHi := strconv.Atoi(strings.TrimSpace(end))
		if errLo != nil || errHi != nil || lo > hi {
			return nil
		}
		var ports []int
		for p := lo; p <= hi; p++ {
			if p >= 1 && p <= 65535 {
				ports = append(ports, p)
			}
		}
		return ports
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return nil
	}
	return []int{port}
}

type portUsage struct {
	publishedPort
	stack   string
	variant string
}

// conflictsWith reports whether two usages can collide at runtime: usages from
// different stacks always can; within one stack only usages of the same
// combination (variant) with distinct bindings can, because variants are
// alternative deployments. The same service publishing one port on two host
// IPs is a duplicate, matching the oracle.
func conflictsWith(a, b portUsage) bool {
	if a.stack != b.stack {
		return true
	}
	if a.variant != b.variant {
		return false
	}
	return a.service != b.service || a.hostIP != b.hostIP
}

// checkPublishedPorts enforces published port uniqueness within each port
// group and matches conflicts against [[published_port]] approvals.
func checkPublishedPorts(cfg *config.Config, approvals *config.Approvals, combos []*ruleContext, reviewed *reviewTracker) []Diagnostic {
	var usages []portUsage
	for _, c := range combos {
		for _, port := range collectPublishedPorts(c) {
			usages = append(usages, portUsage{
				publishedPort: port,
				stack:         c.combo.Stack.RelPath,
				variant:       c.combo.Variant,
			})
		}
	}

	var diagnostics []Diagnostic
	for _, groupRoot := range cfg.PortGroupRoots() {
		name := cfg.PortGroupName(groupRoot)
		byPort := map[string][]portUsage{}
		for _, u := range usages {
			if !stackInGroup(cfg, u.stack, groupRoot) {
				continue
			}
			key := fmt.Sprintf("%d\x00%s", u.port, u.protocol)
			byPort[key] = append(byPort[key], u)
		}
		for _, key := range sortedKeys(byPort) {
			group := byPort[key]
			var conflicts []portUsage
			for _, u := range group {
				for _, v := range group {
					if u != v && conflictsWith(u, v) {
						conflicts = append(conflicts, u)
						break
					}
				}
			}
			// Dedupe identical bindings appearing in multiple variants.
			seen := map[string]bool{}
			var bindings []portUsage
			for _, u := range conflicts {
				id := u.compose + "\x00" + u.service + "\x00" + u.hostIP
				if !seen[id] {
					seen[id] = true
					bindings = append(bindings, u)
				}
			}
			if len(bindings) < 2 {
				continue
			}
			port := group[0].port
			protocol := group[0].protocol
			approval := findPortApproval(cfg, approvals, name, port, protocol)
			mismatch := ""
			if approval != nil {
				reviewed.ports[portKey(name, port, protocol)] = true
				mismatch = portApprovalMismatch(*approval, bindings)
			}
			if approval != nil && mismatch == "" {
				continue
			}
			var hint string
			if approval == nil {
				hint = "Choose another published port, or add a [[published_port]] approval with the exact bindings and a reason in approvals.toml."
			} else {
				hint = fmt.Sprintf("Existing [[published_port]] approval does not match because %s; update it in approvals.toml after reviewing the changed bindings.", mismatch)
			}
			for _, usage := range bindings {
				var others []string
				for _, other := range bindings {
					if other != usage {
						others = append(others, other.compose+":"+other.service)
					}
				}
				diagnostics = append(diagnostics, Diagnostic{
					Severity: "error",
					Rule:     "published_port",
					Stack:    usage.stack,
					Service:  usage.service,
					Message:  fmt.Sprintf("%d/%s is duplicated by %s in group %s", port, protocol, strings.Join(sortedStrings(others), ", "), name),
					Hint:     hint,
				})
			}
		}
	}
	return diagnostics
}

func stackInGroup(cfg *config.Config, stackRelPath, groupRoot string) bool {
	if stackInRoot(stackRelPath, groupRoot) {
		return true
	}
	for _, shared := range cfg.Ports.SharedRoots {
		if stackInRoot(stackRelPath, shared) {
			return true
		}
	}
	return false
}

func stackInRoot(stackRelPath, root string) bool {
	root = strings.TrimSuffix(root, "/")
	return stackRelPath == root || strings.HasPrefix(stackRelPath, root+"/")
}

func findPortApproval(cfg *config.Config, approvals *config.Approvals, group string, port int, protocol string) *config.PortApproval {
	for i := range approvals.Ports {
		candidate := approvals.Ports[i]
		if candidate.Port != port || candidate.Protocol != protocol {
			continue
		}
		approvalGroup, err := cfg.PortGroup(candidate)
		if err == nil && approvalGroup == group {
			return &approvals.Ports[i]
		}
	}
	return nil
}

// portApprovalMismatch compares approval bindings with the current conflict
// set; binding collections must be identical.
func portApprovalMismatch(approval config.PortApproval, bindings []portUsage) string {
	current := map[string]bool{}
	for _, u := range bindings {
		current[u.compose+"\x00"+u.service+"\x00"+u.hostIP] = true
	}
	approved := map[string]bool{}
	for _, b := range approval.Bindings {
		approved[b.Compose+"\x00"+b.Service+"\x00"+b.HostIP] = true
	}
	if len(current) == len(approved) {
		equal := true
		for id := range current {
			if !approved[id] {
				equal = false
			}
		}
		if equal {
			return ""
		}
	}
	format := func(set map[string]bool) string {
		var parts []string
		for id := range set {
			compose, rest, _ := strings.Cut(id, "\x00")
			service, hostIP, _ := strings.Cut(rest, "\x00")
			parts = append(parts, compose+":"+service+"@"+hostIP)
		}
		return strings.Join(sortedStrings(parts), ", ")
	}
	return fmt.Sprintf("current bindings are %s, while approved bindings are %s", format(current), format(approved))
}
