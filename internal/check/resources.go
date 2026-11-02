package check

import (
	"fmt"
	"slices"

	"github.com/YewFence/stacklint/internal/config"
)

type resourceDeclaration struct {
	context  *ruleContext
	external bool
}

type resourceIdentity struct {
	kind string
	name string
}

func checkResourceOwnership(combinations []*ruleContext) []Diagnostic {
	if len(combinations) == 0 {
		return nil
	}
	resources := map[resourceIdentity]map[string][]resourceDeclaration{}
	for _, combination := range combinations {
		for key, volume := range combination.render.Project.Volumes {
			name := volume.Name
			if name == "" {
				name = key
			}
			identity := resourceIdentity{kind: "volume", name: name}
			resources[identity] = addResourceDeclaration(resources[identity], combination, bool(volume.External))
		}
		for key, network := range combination.render.Project.Networks {
			name := network.Name
			if name == "" {
				name = key
			}
			identity := resourceIdentity{kind: "network", name: name}
			resources[identity] = addResourceDeclaration(resources[identity], combination, bool(network.External))
		}
	}

	var diagnostics []Diagnostic
	for _, groupRoot := range combinations[0].cfg.PortGroupRoots() {
		groupName := combinations[0].cfg.PortGroupName(groupRoot)
		for identity, declarationsByStack := range resources {
			mode := combinations[0].cfg.ResourceOwnership.VolumesModeOrDefault()
			if identity.kind == "network" {
				mode = combinations[0].cfg.ResourceOwnership.NetworksModeOrDefault()
			}
			stacks := map[string][]resourceDeclaration{}
			for stack, declarations := range declarationsByStack {
				if stackInGroup(combinations[0].cfg, stack, groupRoot) {
					stacks[stack] = declarations
				}
			}
			reported := map[resourceDeclaration]bool{}
			for _, selection := range resourceSelections(combinations[0].cfg.Deployment, stacks) {
				if len(selection) < 2 {
					continue
				}
				owners := 0
				for _, stack := range selection {
					if hasOwner(stacks[stack]) {
						owners++
					}
				}
				valid := mode == config.ResourceOwnershipEither && owners <= 1 ||
					mode == config.ResourceOwnershipHostOwn && owners == 0 ||
					mode == config.ResourceOwnershipStackOwn && owners == 1
				if valid {
					continue
				}
				for _, stack := range selection {
					for _, declaration := range stacks[stack] {
						if reported[declaration] || mode == config.ResourceOwnershipHostOwn && declaration.external ||
							mode != config.ResourceOwnershipHostOwn && owners > 0 && declaration.external {
							continue
						}
						reported[declaration] = true
						message := resourceOwnershipMessage(identity, mode, groupName)
						diagnostics = append(diagnostics, declaration.context.diagnose("resource_owner", "", message,
							resourceOwnershipHint(mode)))
					}
				}
			}
		}
	}
	return diagnostics
}

// resourceSelections expands only alternative groups touching this resource.
// An alternative group may contribute no stack at all.
func resourceSelections(deployment config.DeploymentConfig, stacks map[string][]resourceDeclaration) [][]string {
	grouped := map[string]bool{}
	var choices [][][]string
	for _, group := range deployment.Alternatives {
		var options [][]string
		for _, stack := range group.Stacks {
			grouped[stack] = true
			if _, exists := stacks[stack]; exists {
				options = append(options, []string{stack})
			}
		}
		if len(options) == 0 {
			continue
		}
		options = append(options, nil)
		choices = append(choices, options)
	}
	var common []string
	for stack := range stacks {
		if !grouped[stack] {
			common = append(common, stack)
		}
	}
	slices.Sort(common)
	selections := [][]string{common}
	for _, options := range choices {
		var next [][]string
		for _, selection := range selections {
			for _, option := range options {
				next = append(next, append(slices.Clone(selection), option...))
			}
		}
		selections = next
	}
	return selections
}

func resourceOwnershipMessage(identity resourceIdentity, mode, group string) string {
	message := fmt.Sprintf("%s %q is declared by multiple stacks in group %s", identity.kind, identity.name, group)
	switch mode {
	case config.ResourceOwnershipHostOwn:
		return message + "; every declaration must set external: true"
	case config.ResourceOwnershipStackOwn:
		return message + "; exactly one stack must own it and every other declaration must set external: true"
	default:
		return message + "; either every declaration must set external: true or exactly one stack must own it"
	}
}

func resourceOwnershipHint(mode string) string {
	switch mode {
	case config.ResourceOwnershipHostOwn:
		return "Mark every declaration external: true; the resource must be created outside Compose."
	case config.ResourceOwnershipStackOwn:
		return "Keep exactly one owning declaration and mark the other stacks' declarations external: true."
	default:
		return "Use either all external declarations or exactly one owner with all other declarations external: true."
	}
}

func addResourceDeclaration(stacks map[string][]resourceDeclaration, combination *ruleContext, external bool) map[string][]resourceDeclaration {
	if stacks == nil {
		stacks = map[string][]resourceDeclaration{}
	}
	stack := combination.combo.Stack.RelPath
	stacks[stack] = append(stacks[stack], resourceDeclaration{context: combination, external: external})
	return stacks
}

func hasOwner(declarations []resourceDeclaration) bool {
	for _, declaration := range declarations {
		if !declaration.external {
			return true
		}
	}
	return false
}
