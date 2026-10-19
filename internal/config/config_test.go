package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFull(t *testing.T) {
	dir := writeConfig(t, `
[repository]
root = "."
deployment_roots = ["stacks/edge", "stacks/yewyard"]
module_roots = ["stacks/compose"]
compose_filenames = ["compose.yaml", "compose.yml"]

[[module]]
compose = "stacks/compose/litestream/compose.yaml"
wiring_file = ".env.litestream"
wiring_template = ".env.litestream.example"
env_group = "LITESTREAM_"
optional_wiring_variables = ["LITESTREAM_REPLICA_SCOPE"]
required_environment = { service = "litestream", prefix = "LITESTREAM_" }

[ports]
shared_roots = ["stacks/edge"]

[bind_address]
default_variable = "BIND_ADDRESS"

[env]
output = ".env.example"

[images]
floating_tags = ["latest", "main"]
private_repositories = ["ghcr.io/yewfence/infisical"]
concurrency = 8
per_registry_concurrency = 4
timeout = "30s"
retries = 2
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Repository.DeploymentRoots; len(got) != 2 {
		t.Errorf("deployment_roots: got %v", got)
	}
	if len(cfg.Modules) != 1 || cfg.Modules[0].EnvGroup != "LITESTREAM_" {
		t.Errorf("modules: got %+v", cfg.Modules)
	}
	if cfg.Modules[0].RequiredEnvironment == nil || cfg.Modules[0].RequiredEnvironment.Prefix != "LITESTREAM_" {
		t.Errorf("required_environment: got %+v", cfg.Modules[0].RequiredEnvironment)
	}
	if cfg.Images.Concurrency != 8 {
		t.Errorf("images.concurrency: got %d", cfg.Images.Concurrency)
	}
}

func TestLoadMinimal(t *testing.T) {
	dir := writeConfig(t, `
[repository]
deployment_roots = ["stacks"]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("missing optional sections must be allowed: %v", err)
	}
	if cfg.BindAddress.DefaultVariable != "" {
		t.Errorf("empty bind_address should leave default_variable empty")
	}
	if got := cfg.ResourceOwnership.VolumesModeOrDefault(); got != ResourceOwnershipHostOwn {
		t.Errorf("default volume ownership mode: got %q", got)
	}
	if got := cfg.ResourceOwnership.NetworksModeOrDefault(); got != ResourceOwnershipHostOwn {
		t.Errorf("default network ownership mode: got %q", got)
	}
}

func TestLoadResourceOwnershipModes(t *testing.T) {
	for _, field := range []string{"volumes_mode", "networks_mode"} {
		for _, mode := range []string{ResourceOwnershipHostOwn, ResourceOwnershipStackOwn, ResourceOwnershipEither} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				dir := writeConfig(t, "[resource_ownership]\n"+field+" = \""+mode+"\"\n")
				cfg, err := Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				wantVolumes, wantNetworks := ResourceOwnershipHostOwn, ResourceOwnershipHostOwn
				if field == "volumes_mode" {
					wantVolumes = mode
				} else {
					wantNetworks = mode
				}
				if got := cfg.ResourceOwnership.VolumesModeOrDefault(); got != wantVolumes {
					t.Errorf("volume mode: got %q, want %q", got, wantVolumes)
				}
				if got := cfg.ResourceOwnership.NetworksModeOrDefault(); got != wantNetworks {
					t.Errorf("network mode: got %q, want %q", got, wantNetworks)
				}
			})
		}
	}
}

func TestLoadRejectsInvalidResourceOwnershipMode(t *testing.T) {
	for _, field := range []string{"volumes_mode", "networks_mode"} {
		t.Run(field, func(t *testing.T) {
			dir := writeConfig(t, "[resource_ownership]\n"+field+" = \"unknown\"\n")
			_, err := Load(dir)
			if !errors.Is(err, ErrValidate) || !strings.Contains(err.Error(), "resource_ownership."+field) {
				t.Fatalf("invalid mode should identify the rejected field, got %v", err)
			}
		})
	}
}

func TestLoadRejectsResourceOwnershipModeKey(t *testing.T) {
	dir := writeConfig(t, "[resource_ownership]\nmode = \"host-own\"\n")
	_, err := Load(dir)
	if !errors.Is(err, ErrValidate) || !strings.Contains(err.Error(), "unknown keys: [resource_ownership.mode]") {
		t.Fatalf("mode must be rejected as an unknown key, got %v", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := writeConfig(t, `
[repository]
deployment_roots = ["stacks"]
unknown_field = true
`)
	_, err := Load(dir)
	if !errors.Is(err, ErrValidate) {
		t.Fatalf("unknown key should fail validation, got %v", err)
	}
}

func TestLoadRejectsWrongType(t *testing.T) {
	dir := writeConfig(t, `
[repository]
deployment_roots = "stacks"
`)
	_, err := Load(dir)
	if !errors.Is(err, ErrLoad) {
		t.Fatalf("wrong type should fail load, got %v", err)
	}
}

func TestLoadRejectsDuplicates(t *testing.T) {
	dir := writeConfig(t, `
[repository]
deployment_roots = ["stacks", "stacks"]
`)
	_, err := Load(dir)
	if !errors.Is(err, ErrValidate) {
		t.Fatalf("duplicate entries should fail validation, got %v", err)
	}
}

func TestLoadRejectsModuleWithoutCompose(t *testing.T) {
	dir := writeConfig(t, `
[[module]]
wiring_file = ".env.litestream"
`)
	_, err := Load(dir)
	if !errors.Is(err, ErrValidate) {
		t.Fatalf("module without compose should fail, got %v", err)
	}
}

func TestRepoRoot(t *testing.T) {
	parent := t.TempDir()
	configDir := filepath.Join(parent, "cfg")

	cfg := &Config{}
	if got := cfg.RepoRoot(configDir); got != configDir {
		t.Errorf("default root should be config dir itself, got %q", got)
	}

	cfg = &Config{Repository: RepositoryConfig{Root: "subdir"}}
	want := filepath.Join(configDir, "subdir")
	if got := cfg.RepoRoot(configDir); got != want {
		t.Errorf("relative root resolves against config dir: got %q, want %q", got, want)
	}

	abs := filepath.Join(parent, "elsewhere")
	cfg = &Config{Repository: RepositoryConfig{Root: abs}}
	if got := cfg.RepoRoot(configDir); got != abs {
		t.Errorf("absolute root used as-is: got %q", got)
	}
}
