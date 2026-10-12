package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the repository policy loaded from stacklint.toml.
type Config struct {
	Repository  RepositoryConfig  `toml:"repository"`
	Modules     []ModuleConfig    `toml:"module"`
	Ports       PortsConfig       `toml:"ports"`
	BindAddress BindAddressConfig `toml:"bind_address"`
	Env         EnvConfig         `toml:"env"`
	Images      ImagesConfig      `toml:"images"`
}

type RepositoryConfig struct {
	Root             string   `toml:"root"`
	DeploymentRoots  []string `toml:"deployment_roots"`
	ModuleRoots      []string `toml:"module_roots"`
	ComposeFilenames []string `toml:"compose_filenames"`
}

type ModuleConfig struct {
	Compose                 string                     `toml:"compose"`
	WiringFile              string                     `toml:"wiring_file"`
	WiringTemplate          string                     `toml:"wiring_template"`
	EnvGroup                string                     `toml:"env_group"`
	OptionalWiringVariables []string                   `toml:"optional_wiring_variables"`
	RequiredEnvironment     *RequiredEnvironmentConfig `toml:"required_environment"`
}

type RequiredEnvironmentConfig struct {
	Service string `toml:"service"`
	Prefix  string `toml:"prefix"`
}

type PortsConfig struct {
	SharedRoots []string `toml:"shared_roots"`
}

type BindAddressConfig struct {
	DefaultVariable string `toml:"default_variable"`
}

type EnvConfig struct {
	Output          string       `toml:"output"`
	IncludeEnvFiles []string     `toml:"include_env_files"`
	Variants        []EnvVariant `toml:"variant"`
}

// EnvVariant registers a complete Compose file combination for a stack.
type EnvVariant struct {
	Stack  string   `toml:"stack"`
	Name   string   `toml:"name"`
	Files  []string `toml:"files"`
	Output string   `toml:"output"`
}

type ImagesConfig struct {
	FloatingTags           []string `toml:"floating_tags"`
	PrivateRepositories    []string `toml:"private_repositories"`
	Concurrency            int      `toml:"concurrency"`
	PerRegistryConcurrency int      `toml:"per_registry_concurrency"`
	Timeout                string   `toml:"timeout"`
	Retries                int      `toml:"retries"`
}

// Defaults for unset images settings, matching the original Python tooling.
const (
	DefaultImagesConcurrency            = 8
	DefaultImagesPerRegistryConcurrency = 4
	DefaultImagesTimeout                = 30 * time.Second
	DefaultImagesRetries                = 2
)

// ConcurrencyOrDefault returns the global registry query concurrency.
func (c ImagesConfig) ConcurrencyOrDefault() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return DefaultImagesConcurrency
}

// PerRegistryConcurrencyOrDefault returns the per-registry query concurrency.
func (c ImagesConfig) PerRegistryConcurrencyOrDefault() int {
	if c.PerRegistryConcurrency > 0 {
		return c.PerRegistryConcurrency
	}
	return DefaultImagesPerRegistryConcurrency
}

// TimeoutOrDefault returns the per-query timeout.
func (c ImagesConfig) TimeoutOrDefault() time.Duration {
	if c.Timeout == "" {
		return DefaultImagesTimeout
	}
	duration, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return DefaultImagesTimeout
	}
	return duration
}

// RetriesOrDefault returns the retry count for retryable registry failures.
func (c ImagesConfig) RetriesOrDefault() int {
	if c.Retries > 0 {
		return c.Retries
	}
	return DefaultImagesRetries
}

var (
	ErrLoad     = errors.New("stacklint configuration load failed")
	ErrValidate = errors.New("stacklint configuration invalid")
)

// Load reads and validates stacklint.toml from the given configuration directory.
// Unknown keys, wrong types, and duplicate entries fail. Missing optional sections are allowed.
func Load(configDir string) (*Config, error) {
	path := filepath.Join(configDir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrLoad, path, err)
	}
	var cfg Config
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrLoad, path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%w: %s: unknown keys: %v", ErrValidate, path, keys)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrValidate, path, err)
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if err := requireNoDuplicates("repository.deployment_roots", c.Repository.DeploymentRoots); err != nil {
		return err
	}
	if err := requireNoDuplicates("repository.module_roots", c.Repository.ModuleRoots); err != nil {
		return err
	}
	if err := requireNoDuplicates("repository.compose_filenames", c.Repository.ComposeFilenames); err != nil {
		return err
	}
	if err := requireNoDuplicates("ports.shared_roots", c.Ports.SharedRoots); err != nil {
		return err
	}
	if err := requireNoDuplicates("images.floating_tags", c.Images.FloatingTags); err != nil {
		return err
	}
	if err := requireNoDuplicates("images.private_repositories", c.Images.PrivateRepositories); err != nil {
		return err
	}
	if c.Images.Timeout != "" {
		if _, err := time.ParseDuration(c.Images.Timeout); err != nil {
			return fmt.Errorf("images.timeout: %q is not a valid duration: %v", c.Images.Timeout, err)
		}
	}
	for i, m := range c.Modules {
		if m.Compose == "" {
			return fmt.Errorf("module[%d]: compose is required", i)
		}
		if err := requireNoDuplicates(fmt.Sprintf("module[%d].optional_wiring_variables", i), m.OptionalWiringVariables); err != nil {
			return err
		}
		if m.RequiredEnvironment != nil && (m.RequiredEnvironment.Service == "" || m.RequiredEnvironment.Prefix == "") {
			return fmt.Errorf("module[%d].required_environment: service and prefix are required", i)
		}
	}
	return c.validateEnv()
}

func (c *Config) validateEnv() error {
	if c.Env.Output == "" {
		c.Env.Output = ".env.example"
	}
	if err := outputName(c.Env.Output); err != nil {
		return fmt.Errorf("env.output: %w", err)
	}
	if err := requireNoDuplicates("env.include_env_files", c.Env.IncludeEnvFiles); err != nil {
		return err
	}
	for _, path := range c.Env.IncludeEnvFiles {
		if !filepath.IsLocal(path) {
			return fmt.Errorf("env.include_env_files: %q must be repository-relative", path)
		}
	}
	names := map[string]bool{}
	outputs := map[string]bool{}
	for i, v := range c.Env.Variants {
		if !filepath.IsLocal(v.Stack) || v.Name == "" || v.Name == "base" {
			return fmt.Errorf("env.variant[%d]: repository-relative stack and nonempty name other than base are required", i)
		}
		if strings.ContainsAny(v.Name, "/\\") || strings.Contains(v.Name, "..") {
			return fmt.Errorf("env.variant[%d]: name %q must be a plain token (it derives the CI artifact filename)", i, v.Name)
		}
		if len(v.Files) == 0 {
			return fmt.Errorf("env.variant[%d]: files must not be empty", i)
		}
		if err := outputName(v.Output); err != nil {
			return fmt.Errorf("env.variant[%d].output: %w", i, err)
		}
		stack := filepath.ToSlash(filepath.Clean(v.Stack))
		key := stack + "\x00" + v.Name
		out := stack + "\x00" + v.Output
		if names[key] || outputs[out] || v.Output == c.Env.Output {
			return fmt.Errorf("env.variant[%d]: duplicate variant name or output in stack %s", i, stack)
		}
		names[key], outputs[out] = true, true
		for _, list := range []struct {
			name  string
			paths []string
		}{{"files", v.Files}} {
			if err := requireNoDuplicates("env.variant."+list.name, list.paths); err != nil {
				return err
			}
			for _, path := range list.paths {
				if !filepath.IsLocal(path) {
					return fmt.Errorf("env.variant[%d].%s: %q must be repository-relative", i, list.name, path)
				}
			}
		}
	}
	return nil
}

func outputName(name string) error {
	if name == "" || name == "." || name == ".." || name == ".env" || name == ".env.secrets" || filepath.Base(name) != name || !filepath.IsLocal(name) {
		return fmt.Errorf("%q must be a filename inside the stack", name)
	}
	return nil
}

func requireNoDuplicates(field string, values []string) error {
	seen := map[string]struct{}{}
	for _, v := range values {
		if _, dup := seen[v]; dup {
			return fmt.Errorf("%s: duplicate entry %q", field, v)
		}
		seen[v] = struct{}{}
	}
	return nil
}

// RepoRoot resolves the repository root: repository.root relative to configDir,
// defaulting to configDir itself.
func (c *Config) RepoRoot(configDir string) string {
	if c.Repository.Root != "" {
		if filepath.IsAbs(c.Repository.Root) {
			return filepath.Clean(c.Repository.Root)
		}
		return filepath.Clean(filepath.Join(configDir, c.Repository.Root))
	}
	return filepath.Clean(configDir)
}

// OutputOrDefault returns the base template filename, mirroring the default
// applied by validation for hand-built configs.
func (c EnvConfig) OutputOrDefault() string {
	if c.Output != "" {
		return c.Output
	}
	return ".env.example"
}

// IsSharedRoot reports whether the deployment root is a shared port root.
func (c *Config) IsSharedRoot(root string) bool {
	for _, shared := range c.Ports.SharedRoots {
		if filepath.ToSlash(filepath.Clean(shared)) == root {
			return true
		}
	}
	return false
}

// PortGroupRoots returns the non-shared deployment roots; each forms a port
// group together with the shared roots.
func (c *Config) PortGroupRoots() []string {
	var roots []string
	for _, root := range c.Repository.DeploymentRoots {
		root = filepath.ToSlash(filepath.Clean(root))
		if !c.IsSharedRoot(root) {
			roots = append(roots, root)
		}
	}
	slices.Sort(roots)
	return roots
}

// PortGroupName renders a group as "<root>+<shared>", matching the original
// tooling's convention of "<root>+edge" for one shared root.
func (c *Config) PortGroupName(root string) string {
	parts := []string{filepath.Base(root)}
	for _, shared := range c.Ports.SharedRoots {
		parts = append(parts, filepath.Base(filepath.Clean(shared)))
	}
	return strings.Join(parts, "+")
}

// DeploymentRootOf maps a repository-relative path to its non-shared
// deployment root, or "" when the path lives under a shared root.
func (c *Config) DeploymentRootOf(relPath string) string {
	for _, root := range c.Repository.DeploymentRoots {
		root = filepath.ToSlash(filepath.Clean(root))
		if relPath != root && !strings.HasPrefix(relPath, root+"/") {
			continue
		}
		if c.IsSharedRoot(root) {
			return ""
		}
		return root
	}
	return ""
}
