package envtemplate

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/template"
)

// Behavior describes a verified simple input; complex expressions have no Behavior.
type Behavior struct {
	Default          *string
	Required         bool
	Nonempty         bool
	EmptyUsesDefault bool
}

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
	summary := template.ExtractVariables(map[string]any{"expression": expression}, nil)
	names := map[string]bool{}
	for name := range summary {
		names[name] = true
	}
	evaluate := func(values map[string]string) (string, error) {
		return template.SubstituteWithOptions(expression, func(name string) (string, bool) {
			names[name] = true
			value, ok := values[name]
			return value, ok
		}, template.WithoutLogging)
	}
	_, err := evaluate(nil)
	var invalid *template.InvalidTemplateError
	if errors.As(err, &invalid) {
		return nil, fmt.Errorf("invalid interpolation %q: %w", expression, err)
	}
	for {
		before := len(names)
		present := map[string]string{}
		empty := map[string]string{}
		for name := range names {
			present[name] = probeMarker(expression, name)
			empty[name] = ""
		}
		_, _ = evaluate(present)
		_, _ = evaluate(empty)
		for _, name := range sortedNames(names) {
			without := cloneMapping(present)
			delete(without, name)
			_, _ = evaluate(without)
			_, _ = evaluate(map[string]string{name: present[name]})
		}
		if len(names) == before {
			break
		}
	}
	all := map[string]string{}
	for name := range names {
		if !validName(name) {
			return nil, fmt.Errorf("unsupported input name %q", name)
		}
		all[name] = probeMarker(expression, name)
	}
	baseline, err := evaluate(all)
	if err != nil {
		return nil, err
	}
	result := map[string]*Behavior{}
	dependent := map[string]bool{}
	for name, variable := range summary {
		for _, branch := range []string{variable.DefaultValue, variable.PresenceValue} {
			nested := template.ExtractVariables(map[string]any{"branch": branch}, nil)
			if len(nested) > 0 {
				dependent[name] = true
				for inner := range nested {
					dependent[inner] = true
				}
			}
		}
	}
	for _, name := range sortedNames(names) {
		result[name] = nil
		if dependent[name] {
			continue
		}
		marker := all[name]
		if strings.Count(baseline, marker) != 1 {
			continue
		}
		prefix, suffix, _ := strings.Cut(baseline, marker)
		missing := cloneMapping(all)
		delete(missing, name)
		absent, absentErr := evaluate(missing)
		emptyValues := cloneMapping(all)
		emptyValues[name] = ""
		empty, emptyErr := evaluate(emptyValues)
		if absentErr != nil {
			var required *template.MissingRequiredError
			if !errors.As(absentErr, &required) || required.Variable != name || len(names) != 1 {
				continue
			}
			result[name] = &Behavior{Required: true, Nonempty: emptyErr != nil}
			continue
		}
		if emptyErr != nil {
			continue
		}
		fallback, ok := slot(absent, prefix, suffix)
		emptySlot, emptyOK := slot(empty, prefix, suffix)
		if !ok || !emptyOK || (emptySlot != "" && emptySlot != fallback) {
			continue
		}
		contaminated := false
		for _, value := range all {
			if strings.Contains(fallback, value) {
				contaminated = true
			}
		}
		if contaminated || strings.ContainsAny(fallback, "\r\n") {
			continue
		}
		variable, hasSummary := summary[name]
		if !hasSummary {
			continue
		}
		if variable.PresenceValue != "" {
			continue
		}
		behavior := &Behavior{}
		if variable.DefaultValue != "" {
			behavior.Default = &fallback
			behavior.EmptyUsesDefault = emptySlot == fallback
		} else if fallback != "" {
			continue
		}
		result[name] = behavior
	}
	return result, nil
}

func probeMarker(expression, name string) string {
	prefix := "__STACKLINT_PROBE_"
	for strings.Contains(expression, prefix) {
		prefix += "_"
	}
	return prefix + name + "__"
}

func slot(value, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) || len(value) < len(prefix)+len(suffix) {
		return "", false
	}
	return value[len(prefix) : len(value)-len(suffix)], true
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
