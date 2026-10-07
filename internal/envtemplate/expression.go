package envtemplate

import (
	"slices"

	"github.com/YewFence/stacklint/internal/interp"
)

// Behavior describes a verified simple input; complex expressions have no Behavior.
type Behavior = interp.Behavior

type occurrence struct {
	File        string
	Path        string
	Expression  string
	Behavior    *Behavior
	Source      string
	SourceValue string
	Effective   bool
	Passthrough bool
}

type input struct {
	Name         string
	Occurrences  []occurrence
	Descriptions []string
	Source       string
	Group        string
}

func variables(expression string) (map[string]*Behavior, error) {
	return interp.Variables(expression)
}

func cloneMapping(values map[string]string) map[string]string {
	copy := map[string]string{}
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func sortedNames[V any](values map[string]V) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, char := range name {
		if char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}
