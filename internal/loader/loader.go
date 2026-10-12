package loader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/wiring"
)

// StackRender is the loaded and rendered state of a single stack.
type StackRender struct {
	Stack discover.Stack
	// Project is the interpolated, normalized model.
	Project *types.Project
	// Raw is the uninterpolated model: the merged YAML with expressions preserved.
	Raw map[string]any
	// SourceFiles are the Compose files actually referenced, include-expanded, in load order.
	SourceFiles []string
}

// Render loads and renders one stack's base combination. The render
// environment is the combination's own template (dotenv channel: live values
// for exempted, hand-maintained templates; missing files are skipped) plus
// @env-example marker values from the include graph (marker channel, which
// wins). The process environment and project .env are never read. All
// profiles are enabled.
func Render(ctx context.Context, cfg *config.Config, stack discover.Stack) (*StackRender, error) {
	return RenderCombination(ctx, cfg, stack, []string{stack.ComposeFile}, filepath.Join(stack.Dir, cfg.Env.OutputOrDefault()))
}

// RenderCombination renders an explicit ordered Compose file list for a stack,
// using the combination's templatePath as the dotenv render-input channel.
func RenderCombination(ctx context.Context, cfg *config.Config, stack discover.Stack, files []string, templatePath string) (*StackRender, error) {
	env, err := renderEnv(stack, files, templatePath)
	if err != nil {
		return nil, err
	}

	project, err := loadProject(ctx, stack, files, env, true)
	if err != nil {
		return nil, err
	}
	raw, err := loadRawModel(ctx, stack, files, env)
	if err != nil {
		return nil, fmt.Errorf("load uninterpolated view of %s: %w", stack.RelPath, err)
	}
	return &StackRender{
		Stack:       stack,
		Project:     project,
		Raw:         raw,
		SourceFiles: append([]string(nil), project.ComposeFiles...),
	}, nil
}

// renderEnv builds the flattened interpolation environment: the
// combination's own template via dotenv first (missing templates are skipped),
// then @env-example marker values, which take precedence.
func renderEnv(stack discover.Stack, files []string, templatePath string) (map[string]string, error) {
	env := map[string]string{}
	data, err := os.ReadFile(templatePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read env file %s: %w", templatePath, err)
	}
	if err == nil {
		parsed, err := dotenv.Parse(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parse env file %s: %w", templatePath, err)
		}
		for k, v := range parsed {
			env[k] = v
		}
	}
	examples, err := wiring.ExampleValues(files, stack.Dir)
	if err != nil {
		return nil, err
	}
	for k, v := range examples {
		env[k] = v
	}
	return env, nil
}


func baseOptions(stack discover.Stack, env map[string]string, interpolate bool) []composecli.ProjectOptionsFn {
	return []composecli.ProjectOptionsFn{
		composecli.WithWorkingDirectory(stack.Dir),
		composecli.WithEnv(mapping(env)),
		composecli.WithInterpolation(interpolate),
		composecli.WithProfiles([]string{"*"}),
		composecli.WithDiscardEnvFile,
	}
}

func loadProject(ctx context.Context, stack discover.Stack, files []string, env map[string]string, interpolate bool) (*types.Project, error) {
	opts, err := composecli.NewProjectOptions(files, baseOptions(stack, env, interpolate)...)
	if err != nil {
		return nil, fmt.Errorf("project options for %s: %w", stack.RelPath, err)
	}
	project, err := opts.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("render stack %s: %w", stack.RelPath, err)
	}
	return project, nil
}

// loadRawModel produces the uninterpolated view as a merged YAML dictionary so
// raw expressions stay intact without triggering typed decoding validation.
func loadRawModel(ctx context.Context, stack discover.Stack, files []string, env map[string]string) (map[string]any, error) {
	configDetails, err := loader.LoadConfigFiles(ctx, files, stack.Dir)
	if err != nil {
		return nil, err
	}
	configDetails.Environment = types.NewMapping(mapping(env))
	model, err := loader.LoadModelWithContext(ctx, *configDetails,
		func(o *loader.Options) {
			o.SkipInterpolation = true
			o.SkipValidation = true
			o.Profiles = []string{"*"}
		},
	)
	if err != nil {
		return nil, err
	}
	return model, nil
}

func mapping(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
