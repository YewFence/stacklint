package check

import (
	"fmt"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/imageref"
)

// checkStaleApprovals reports approvals that are no longer needed: their target
// disappeared, the rule is compliant again, or the approval content no longer
// matches reality. A stale approval is only judged when every Compose file it
// references was part of this check run.
func checkStaleApprovals(cfg *config.Config, approvals *config.Approvals, combos []*ruleContext, reviewed *reviewTracker) []Diagnostic {
	checked := map[string]bool{}
	stackOfCompose := map[string]string{}
	for _, c := range combos {
		checked[c.composeRel()] = true
		stackOfCompose[c.composeRel()] = c.combo.Stack.RelPath
	}

	var diagnostics []Diagnostic
	stale := func(compose, rule, message, hint string) {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: "error",
			Rule:     rule,
			Stack:    stackOfCompose[compose],
			Message:  message,
			Hint:     hint,
		})
	}

	for _, approval := range approvals.Images {
		if !checked[approval.Compose] {
			continue
		}
		key := imageKey(approval.Compose, approval.Service)
		if reviewed.images[key] {
			continue
		}
		if current, observed := reviewed.observedImages[key]; observed {
			if _, violation := imageref.Parse(current); violation == nil {
				stale(approval.Compose, "stale_approval",
					fmt.Sprintf("image approval for service %s with rule %s is unnecessary because %s is pinned to a digest", approval.Service, approval.Rule, current),
					"Remove the redundant [[image]] approval from approvals.toml.")
				continue
			}
		}
		stale(approval.Compose, "stale_approval",
			fmt.Sprintf("image approval for service %s and repository %s no longer has a rendered service or image", approval.Service, approval.Repository),
			"Remove the stale [[image]] approval from approvals.toml.")
	}
	for _, approval := range approvals.Ports {
		group, err := cfg.PortGroup(approval)
		if err != nil {
			continue // invalid approvals already fail config validation
		}
		allChecked := true
		for _, binding := range approval.Bindings {
			if !checked[binding.Compose] {
				allChecked = false
			}
		}
		if !allChecked || reviewed.ports[portKey(group, approval.Port, approval.Protocol)] {
			continue
		}
		stale(approval.Bindings[0].Compose, "stale_approval",
			fmt.Sprintf("published port approval for %d/%s in group %s is unnecessary because those bindings are no longer duplicated", approval.Port, approval.Protocol, group),
			"Remove the stale [[published_port]] approval from approvals.toml.")
	}
	for _, approval := range approvals.Binds {
		if !checked[approval.Compose] {
			continue
		}
		if reviewed.binds[bindKey(approval.Compose, approval.Service)] {
			continue
		}
		stale(approval.Compose, "stale_approval",
			fmt.Sprintf("bind address approval for service %s is stale because the service no longer uses nonstandard host binding variables", approval.Service),
			"Remove the [[bind_address]] approval from approvals.toml.")
	}
	for _, approval := range approvals.EnvFiles {
		if !checked[approval.Compose] {
			continue
		}
		if reviewed.envFiles[envFileKey(approval.Compose, approval.Service, approval.Path)] {
			continue
		}
		stale(approval.Compose, "stale_approval",
			fmt.Sprintf("env_file approval for service %s and path %s is stale because the service no longer uses the approved project .env", approval.Service, approval.Path),
			"Remove the stale [[env_file]] approval from approvals.toml.")
	}
	return diagnostics
}
