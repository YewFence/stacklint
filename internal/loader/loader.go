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

// Render loads and renders one stack. Render inputs are flattened into a single
// interpolation environment with no precedence tiers: render.env_files (relative
// to the stack directory) and all registered modules' CI env files (relative to
// each module's Compose directory). Missing files are skipped. The process
// environment and project .env are never read. All profiles are enabled.
func Render(ctx context.Context, cfg *config.Config, repoRoot string, stack discover.Stack) (*StackRender, error) {
	env, err := renderEnv(cfg, repoRoot, stack)
	if err != nil {
		return nil, err
	}

	project, err := loadProject(ctx, stack, env, true)
	if err != nil {
		return nil, err
	}
	raw, err := loadRawModel(ctx, stack, env)
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

func renderEnv(cfg *config.Config, repoRoot string, stack discover.Stack) (map[string]string, error) {
	var files []string
	for _, rel := range cfg.Render.EnvFiles {
		files = append(files, filepath.Join(stack.Dir, rel))
	}
	for _, m := range cfg.Modules {
		if m.CIEnvFile != "" {
			files = append(files, filepath.Join(repoRoot, filepath.Dir(m.Compose), m.CIEnvFile))
		}
	}
	return readEnvFiles(files)
}

func readEnvFiles(files []string) (map[string]string, error) {
	env := map[string]string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read env file %s: %w", file, err)
		}
		parsed, err := dotenv.Parse(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("parse env file %s: %w", file, err)
		}
		for k, v := range parsed {
			env[k] = v
		}
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

func loadProject(ctx context.Context, stack discover.Stack, env map[string]string, interpolate bool) (*types.Project, error) {
	opts, err := composecli.NewProjectOptions([]string{stack.ComposeFile}, baseOptions(stack, env, interpolate)...)
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
func loadRawModel(ctx context.Context, stack discover.Stack, env map[string]string) (map[string]any, error) {
	configDetails, err := loader.LoadConfigFiles(ctx, []string{stack.ComposeFile}, stack.Dir)
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
