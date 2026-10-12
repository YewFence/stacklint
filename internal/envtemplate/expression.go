package envtemplate

import (
	"slices"

	"github.com/YewFence/stacklint/internal/interp"
	"github.com/YewFence/stacklint/internal/markers"
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
	Preambles    []string
	Source       string
	// SourcedFromMarker reports that Source came from an @env-source
	// annotation rather than a wiring occurrence; annotated sources carry no
	// repository-side value, so an @env-example cannot shadow them.
	SourcedFromMarker bool
	Group             string
	Example           string
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
	return markers.ValidName(name)
}
