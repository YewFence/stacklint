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

// Render loads and renders one stack's base combination. Render inputs are
// flattened into a single interpolation environment with no precedence tiers:
// render.env_files (relative to the stack directory) and all registered
// modules' CI env files (relative to each module's Compose directory). Missing
// files are skipped. The process environment and project .env are never read.
// All profiles are enabled.
func Render(ctx context.Context, cfg *config.Config, repoRoot string, stack discover.Stack) (*StackRender, error) {
	return RenderCombination(ctx, cfg, repoRoot, stack, []string{stack.ComposeFile}, nil)
}

// RenderCombination renders an explicit ordered Compose file list for a stack,
// appending extraEnvFiles (repository-relative CI inputs, which must exist) to
// the flattened render environment.
func RenderCombination(ctx context.Context, cfg *config.Config, repoRoot string, stack discover.Stack, files []string, extraEnvFiles []string) (*StackRender, error) {
	env, err := renderEnv(cfg, repoRoot, stack, extraEnvFiles)
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

func renderEnv(cfg *config.Config, repoRoot string, stack discover.Stack, extraEnvFiles []string) (map[string]string, error) {
	var files []string
	for _, rel := range cfg.Render.EnvFiles {
		files = append(files, filepath.Join(stack.Dir, rel))
	}
	for _, m := range cfg.Modules {
		if m.CIEnvFile != "" {
			files = append(files, filepath.Join(repoRoot, filepath.Dir(m.Compose), m.CIEnvFile))
		}
	}
	for _, rel := range extraEnvFiles {
		files = append(files, filepath.Join(repoRoot, rel))
	}
	return readEnvFiles(files, len(extraEnvFiles))
}

// readEnvFiles loads the flattened render environment. Missing files are
// skipped, except the final requiredExtra entries (combination CI inputs),
// which are explicitly registered and must exist.
func readEnvFiles(files []string, requiredExtra int) (map[string]string, error) {
	env := map[string]string{}
	for i, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) && i < len(files)-requiredExtra {
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
