package check

import (
	"fmt"

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
	for identity, stacks := range resources {
		if len(stacks) < 2 {
			continue
		}
		owners := 0
		for _, declarations := range stacks {
			if hasOwner(declarations) {
				owners++
			}
		}
		mode := combinations[0].cfg.ResourceOwnership.ModeOrDefault()
		valid := mode == config.ResourceOwnershipEither && owners <= 1 ||
			mode == config.ResourceOwnershipHostOwn && owners == 0 ||
			mode == config.ResourceOwnershipStackOwn && owners == 1
		if valid {
			continue
		}
		for _, declarations := range stacks {
			for _, declaration := range declarations {
				if mode == config.ResourceOwnershipHostOwn && declaration.external {
					continue
				}
				if mode != config.ResourceOwnershipHostOwn && owners > 0 && declaration.external {
					continue
				}
				message := resourceOwnershipMessage(identity, mode)
				diagnostics = append(diagnostics, declaration.context.diagnose("resource_owner", "", message,
					resourceOwnershipHint(mode)))
			}
		}
	}
	return diagnostics
}

func resourceOwnershipMessage(identity resourceIdentity, mode string) string {
	message := fmt.Sprintf("%s %q is declared by multiple stacks", identity.kind, identity.name)
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
