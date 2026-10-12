package envtemplate

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/markers"
	"github.com/compose-spec/compose-go/v2/template"
	"go.yaml.in/yaml/v3"
)

// Result is a fully prepared template, without filesystem writes.
type Result struct {
	Target             Target
	Content            []byte
	Warnings           []string
	EnvFilePassthrough bool
}

type annotation struct {
	Name string
	Kind string
	Text string
	File string
}

type scanner struct {
	graph                *graph
	inputs               []*input
	byName               map[string]*input
	annotations          []annotation
	originals            map[string]bool
	effectiveExpressions map[string]bool
	effectivePass        map[string]bool
	passthrough          bool
}

// Generate analyzes a complete file graph and prepares its deterministic template.
// excluded contains every generated output belonging to this stack.
// On failure, a nonnil result contains warnings only and must not be written.
func Generate(ctx context.Context, cfg *config.Config, repoRoot string, target Target, excluded []string) (result *Result, err error) {
	graph, err := newGraph(cfg, repoRoot, target, excluded)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && len(graph.warnings) > 0 {
			result = &Result{Target: target, Warnings: uniqueStrings(graph.warnings)}
		}
	}()
	model, err := graph.load(ctx)
	if err != nil {
		return nil, err
	}
	for _, source := range graph.sources {
		if source.Path == target.Output {
			return nil, fmt.Errorf("output would replace input file %s", graph.relative(source.Path))
		}
	}
	scan := &scanner{graph: graph, byName: map[string]*input{}, originals: map[string]bool{}, effectiveExpressions: map[string]bool{}, effectivePass: map[string]bool{}}
	for _, file := range graph.sources {
		if file.EnvFile {
			for _, name := range sortedNames(file.Dependencies) {
				if err := scan.add(name, occurrence{File: graph.relative(file.Path), Path: "include.env_file", Behavior: file.Dependencies[name]}, file.Sources); err != nil {
					return nil, err
				}
			}
			continue
		}
		found := map[string]bool{}
		if err := scan.walk(file.Document, nil, file, found, map[*yaml.Node]bool{}); err != nil {
			return nil, err
		}
		var dict map[string]any
		if err := file.Document.Decode(&dict); err != nil {
			return nil, fmt.Errorf("cannot decode %s", graph.relative(file.Path))
		}
		for _, name := range sortedNames(template.ExtractVariables(dict, nil)) {
			if !found[name] {
				return nil, fmt.Errorf("input discovery incomplete for %s in %s", name, graph.relative(file.Path))
			}
		}
	}
	scan.collectEffective(model, nil)
	for _, item := range scan.inputs {
		for i := range item.Occurrences {
			occurrence := &item.Occurrences[i]
			occurrence.Effective = occurrence.Path == "include.env_file" || occurrence.Expression != "" && scan.effectiveExpressions[occurrence.Expression] || occurrence.Passthrough && scan.effectivePass[passthroughKey(occurrence.Path, item.Name)]
		}
	}
	if err := scan.applyAnnotations(); err != nil {
		return nil, err
	}
	var missingExamples []string
	for _, item := range scan.inputs {
		if item.Example != "" || item.Source != "" {
			continue
		}
		for _, occ := range item.Occurrences {
			if occ.Behavior != nil && occ.Behavior.Required && occ.Behavior.Nonempty {
				missingExamples = append(missingExamples, item.Name)
				break
			}
		}
	}
	if len(missingExamples) > 0 {
		slices.Sort(missingExamples)
		return nil, fmt.Errorf("required inputs have no example value: %s; declare `# @env-example NAME: value` next to the required expression", strings.Join(missingExamples, ", "))
	}
	content, err := render(scan.inputs, target, graph.relative)
	if err != nil {
		return nil, err
	}
	return &Result{Target: target, Content: []byte(content), Warnings: uniqueStrings(graph.warnings), EnvFilePassthrough: scan.passthrough}, nil
}

func (s *scanner) add(name string, occ occurrence, sources map[string]sourceValue) error {
	if !validName(name) {
		return fmt.Errorf("unsupported input name at %s:%s", occ.File, occ.Path)
	}
	if source, ok := sources[name]; ok {
		occ.Source = source.File
		occ.SourceValue = source.Value
	}
	item := s.byName[name]
	if item == nil {
		group := s.group(name)
		if group == "" && occ.File != "" {
			entryFile := false
			for _, file := range s.graph.target.Files {
				if s.graph.relative(file) == occ.File {
					entryFile = true
				}
			}
			if !entryFile {
				group = occ.File
			}
		}
		item = &input{Name: name, Group: group}
		s.byName[name] = item
		s.inputs = append(s.inputs, item)
	}
	for _, existing := range item.Occurrences {
		if existing.Source != occ.Source || existing.SourceValue != occ.SourceValue {
			return fmt.Errorf("input %s has inconsistent sources in %s and %s; use distinct variable names", name, existing.File, occ.File)
		}
		if existing.File == occ.File && existing.Path == occ.Path && existing.Expression == occ.Expression {
			return nil
		}
	}
	item.Occurrences = append(item.Occurrences, occ)
	return nil
}

func (s *scanner) group(name string) string {
	best := ""
	for _, module := range s.graph.cfg.Modules {
		prefix := module.EnvGroup
		if prefix == "" && module.RequiredEnvironment != nil {
			prefix = module.RequiredEnvironment.Prefix
		}
		if prefix != "" && strings.HasPrefix(name, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	return best
}

func passthroughContainer(path []string) bool {
	return len(path) == 3 && (path[0] == "services" || path[0] == "jobs") && path[2] == "environment" || len(path) == 4 && path[0] == "services" && path[2] == "build" && path[3] == "args"
}

func (s *scanner) walk(node *yaml.Node, path []string, source sourceFile, found map[string]bool, ancestors map[*yaml.Node]bool) error {
	if ancestors[node] {
		return fmt.Errorf("recursive alias in %s", s.graph.relative(source.Path))
	}
	ancestors[node] = true
	defer delete(ancestors, node)
	s.comments(node, source)
	defer s.commentText(node.FootComment, source)
	if node.Kind == yaml.AliasNode {
		return s.walk(node.Alias, path, source, found, ancestors)
	}
	switch node.Kind {
	case yaml.DocumentNode:
		return s.walk(node.Content[0], path, source, found, ancestors)
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			key, child := node.Content[i], node.Content[i+1]
			s.comments(key, source)
			childPath := appendPath(path, key.Value)
			if key.Tag == "!!merge" {
				if err := s.walk(child, path, source, found, ancestors); err != nil {
					return err
				}
				s.commentText(key.FootComment, source)
				continue
			}
			resolved := resolvedNode(child)
			containerPath := appendPath(path, key.Value)
			if passthroughContainer(containerPath) && resolved.Kind != yaml.MappingNode && resolved.Kind != yaml.SequenceNode && resolved.Tag != "!!null" {
				return fmt.Errorf("unsupported environment/build args shape at %s:%s", s.graph.relative(source.Path), strings.Join(containerPath, "."))
			}
			if passthroughContainer(path) && resolved.Kind != yaml.ScalarNode {
				return fmt.Errorf("environment/build args values must be scalars at %s:%s", s.graph.relative(source.Path), strings.Join(childPath, "."))
			}
			if passthroughContainer(path) && resolved.Tag == "!!null" {
				found[key.Value] = true
				if err := s.add(key.Value, occurrence{File: s.graph.relative(source.Path), Path: strings.Join(childPath, "."), Behavior: &Behavior{}, Passthrough: true}, source.Sources); err != nil {
					return err
				}
			}
			if len(path) == 2 && (path[0] == "configs" || path[0] == "secrets") && key.Value == "environment" {
				if resolved.Kind != yaml.ScalarNode || resolved.Tag != "!!str" || strings.Contains(resolved.Value, "$") {
					return fmt.Errorf("unsupported environment source at %s:%s", s.graph.relative(source.Path), strings.Join(childPath, "."))
				}
				found[resolved.Value] = true
				if err := s.add(resolved.Value, occurrence{File: s.graph.relative(source.Path), Path: strings.Join(childPath, "."), Behavior: &Behavior{Required: true}, Passthrough: true}, source.Sources); err != nil {
					return err
				}
			}
			if passthroughContainer(path) && resolved.Kind == yaml.ScalarNode && resolved.Tag != "!!str" && resolved.Tag != "!!null" && resolved.Tag != "!!bool" && resolved.Tag != "!!int" && resolved.Tag != "!!float" {
				return fmt.Errorf("unsupported input value at %s", strings.Join(childPath, "."))
			}
			if err := s.walk(child, childPath, source, found, ancestors); err != nil {
				return err
			}
			s.commentText(key.FootComment, source)
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			childPath := appendPath(path, strconv.Itoa(i))
			if passthroughContainer(path) {
				resolved := resolvedNode(child)
				if resolved.Kind != yaml.ScalarNode || resolved.Tag != "!!str" {
					return fmt.Errorf("environment/build args list entries must be strings at %s", strings.Join(path, "."))
				}
				if !strings.Contains(resolved.Value, "=") {
					if !strings.Contains(resolved.Value, "$") {
						found[resolved.Value] = true
						if err := s.add(resolved.Value, occurrence{File: s.graph.relative(source.Path), Path: strings.Join(path, ".") + "." + resolved.Value, Behavior: &Behavior{}, Passthrough: true}, source.Sources); err != nil {
							return err
						}
					}
				}
			}
			if err := s.walk(child, childPath, source, found, ancestors); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" || !strings.Contains(node.Value, "$") {
			return nil
		}
		expression := node.Value
		if len(path) > 0 && passthroughContainer(path[:len(path)-1]) {
			if _, err := strconv.Atoi(path[len(path)-1]); err == nil {
				if _, value, hasEquals := strings.Cut(expression, "="); hasEquals {
					expression = value
				}
			}
		}
		behaviors, err := variables(expression)
		if err != nil {
			return fmt.Errorf("%s:%s: %w", s.graph.relative(source.Path), strings.Join(path, "."), err)
		}
		s.originals[expression] = true
		for _, name := range sortedNames(behaviors) {
			found[name] = true
			if err := s.add(name, occurrence{File: s.graph.relative(source.Path), Path: strings.Join(path, "."), Expression: expression, Behavior: behaviors[name]}, source.Sources); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *scanner) collectEffective(value any, path []string) {
	switch value := value.(type) {
	case map[string]any:
		for _, key := range sortedNames(value) {
			child := value[key]
			childPath := appendPath(path, key)
			if passthroughContainer(path) {
				s.effectivePass[strings.Join(childPath, ".")+"\x00"+key] = true
			}
			if len(path) == 2 && (path[0] == "configs" || path[0] == "secrets") && key == "environment" {
				if name, ok := child.(string); ok {
					s.effectivePass[strings.Join(childPath, ".")+"\x00"+name] = true
				}
			}
			if key == "env_file" && len(path) == 2 && path[0] == "services" {
				s.detectEnvPassthrough(child)
			}
			s.collectEffective(child, childPath)
		}
	case []any:
		for i, child := range value {
			if passthroughContainer(path) {
				if text, ok := child.(string); ok {
					name, _, _ := strings.Cut(text, "=")
					s.effectivePass[strings.Join(path, ".")+"."+name+"\x00"+name] = true
				}
			}
			s.collectEffective(child, appendPath(path, strconv.Itoa(i)))
		}
	case string:
		if len(path) > 0 && passthroughContainer(path[:len(path)-1]) {
			if _, err := strconv.Atoi(path[len(path)-1]); err == nil {
				if _, expression, assigned := strings.Cut(value, "="); assigned {
					value = expression
				}
			}
		}
		// Values introduced by dotenv resolution are never used as source expressions.
		if s.originals[value] {
			s.effectiveExpressions[value] = true
		}
	}
}

func (s *scanner) detectEnvPassthrough(value any) {
	switch value := value.(type) {
	case string:
		if absolutePath(s.graph.target.Stack.Dir, value) == filepathEnv(s.graph.target.Stack.Dir) {
			s.passthrough = true
		}
	case []any:
		for _, child := range value {
			s.detectEnvPassthrough(child)
		}
	case map[string]any:
		s.detectEnvPassthrough(value["path"])
	}
}

func filepathEnv(dir string) string           { return absolutePath(dir, ".env") }
func passthroughKey(path, name string) string { return path + "\x00" + name }

func (s *scanner) comments(node *yaml.Node, source sourceFile) {
	s.commentText(node.HeadComment, source)
	s.commentText(node.LineComment, source)
}

func (s *scanner) commentText(comment string, source sourceFile) {
	for _, line := range strings.Split(comment, "\n") {
		kind, name, text, ok := markers.Parse(line)
		if !ok {
			continue
		}
		s.annotations = append(s.annotations, annotation{Name: name, Kind: kind, Text: text, File: s.graph.relative(source.Path)})
	}
}

func (s *scanner) applyAnnotations() error {
	for _, mark := range s.annotations {
		if mark.Kind == "invalid" {
			return fmt.Errorf("invalid @env annotation in %s", mark.File)
		}
		item := s.byName[mark.Name]
		if item == nil {
			return fmt.Errorf("annotation for %s in %s has no matching input", mark.Name, mark.File)
		}
		switch mark.Kind {
		case "@env":
			if !slices.Contains(item.Descriptions, mark.Text) {
				item.Descriptions = append(item.Descriptions, mark.Text)
			}
		case "@env-source":
			if item.Source != "" && item.Source != mark.Text {
				return fmt.Errorf("conflicting @env-source providers for %s", mark.Name)
			}
			if len(item.Occurrences) > 0 && item.Occurrences[0].Source != "" {
				return fmt.Errorf("input %s has both wiring and @env-source providers", mark.Name)
			}
			item.Source = mark.Text
			item.SourcedFromMarker = true
		case "@env-example":
			if item.Example != "" && item.Example != mark.Text {
				return fmt.Errorf("conflicting @env-example values for %s", mark.Name)
			}
			item.Example = mark.Text
		}
	}
	if item := s.byName["COMPOSE_PROJECT_NAME"]; item != nil {
		if item.Source != "" {
			return fmt.Errorf("COMPOSE_PROJECT_NAME is supplied by Docker Compose and cannot declare @env-source")
		}
		item.Source = "Docker Compose project name"
	}
	for _, item := range s.inputs {
		if item.Source == "" && len(item.Occurrences) > 0 {
			item.Source = item.Occurrences[0].Source
		}
	}
	for _, item := range s.inputs {
		if item.Example == "" {
			continue
		}
		if item.Source != "" && !item.SourcedFromMarker {
			return fmt.Errorf("@env-example for %s is shadowed: the variable is already provided by %s", item.Name, item.Source)
		}
		allowed := false
		for _, occ := range item.Occurrences {
			// Wiring dependencies legitimately need example values even when
			// the wiring expression itself carries no required behavior.
			if occ.Path == "include.env_file" || occ.Behavior != nil && occ.Behavior.Required {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("@env-example for %s is stale: no required expression or wiring dependency needs an example value", item.Name)
		}
	}
	// Preamble anchors are validated after source backfill: the anchor must
	// render assignment entries, so externally supplied inputs cannot anchor.
	for _, mark := range s.annotations {
		if mark.Kind != "@env-preamble" {
			continue
		}
		item := s.byName[mark.Name]
		if item.Source != "" {
			return fmt.Errorf("@env-preamble for %s in %s anchors an input with no assignment entry", mark.Name, mark.File)
		}
		if !slices.Contains(item.Preambles, mark.Text) {
			item.Preambles = append(item.Preambles, mark.Text)
		}
	}
	return nil
}

func uniqueStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}
