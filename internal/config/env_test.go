package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvVariantConfiguration(t *testing.T) {
	dir := writeConfig(t, `[env]
output = ".env.example"
include_env_files = ["shared/wiring.env"]
[[env.variant]]
stack = "stacks/app"
name = "local"
files = ["stacks/app/compose.yaml", "stacks/app/compose.local.yaml"]
output = ".env.local.example"
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Env.Variants) != 1 || cfg.Env.Variants[0].Name != "local" || len(cfg.Env.Variants[0].Files) != 2 {
		t.Fatalf("got %+v", cfg.Env)
	}
	if len(cfg.Env.IncludeEnvFiles) != 1 {
		t.Fatalf("include env files lost: %+v", cfg.Env)
	}
}

func TestEnvFixedFilesConfiguration(t *testing.T) {
	cfg, err := Load(writeConfig(t, "[env]\nfixed_files = ['.env.conf', 'conf/extra.env']\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Env.FixedFiles) != 2 || cfg.Env.FixedFiles[0] != ".env.conf" {
		t.Fatalf("fixed files lost: %+v", cfg.Env)
	}
}

func TestInvalidEnvConfiguration(t *testing.T) {
	tests := []string{
		"[env]\noutput = '../escape.env'\n",
		"[env]\ninclude_env_files = ['../wiring.env']\n",
		"[env]\nfixed_files = ['.env.conf', '.env.conf']\n",
		"[env]\nfixed_files = ['../outside.env']\n",
		"[env]\nfixed_files = ['/etc/app.env']\n",
		"[env]\nfixed_files = ['.env']\n",
		"[env]\nfixed_files = ['.env.secrets']\n",
		"[env]\nfixed_files = ['conf/.env']\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'base'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.local.example'\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = []\noutput = '.env.local.example'\n",
		"[[env.variant]]\nstack = '../app'\nname = 'local'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.local.example'\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = ['../compose.yaml']\noutput = '.env.local.example'\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.example'\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.local.example'\nunknown = true\n",
		"[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.local.example'\n[[env.variant]]\nstack = 'stacks/app'\nname = 'local'\nfiles = ['stacks/app/compose.yaml']\noutput = '.env.other.example'\n",
	}
	for _, content := range tests {
		t.Run(content, func(t *testing.T) {
			_, err := Load(writeConfig(t, content))
			if !errors.Is(err, ErrValidate) {
				t.Fatalf("expected validation failure, got %v", err)
			}
		})
	}
}

func TestEnvExemptionValidation(t *testing.T) {
	tests := []struct {
		name, content string
		valid         bool
	}{
		{"pending", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'pending'\n", true},
		{"upstream", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'upstream'\nreason = 'upstream interface'\nsource = 'https://example.invalid/env'\n", true},
		{"passthrough", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'env-file-passthrough'\nreason = 'application interface'\n", true},
		{"missing reason", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'env-file-passthrough'\n", false},
		{"missing source", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'upstream'\nreason = 'interface'\n", false},
		{"unknown kind", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'unknown'\n", false},
		{"unknown key", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'pending'\nunknown = 'bad'\n", false},
		{"duplicate stack", "[[env_exemption]]\nstack = 'stacks/app'\nkind = 'pending'\n[[env_exemption]]\nstack = 'stacks/app'\nkind = 'pending'\n", false},
		{"other rules preserved", "[[image]]\ncompose = 'stacks/app/compose.yaml'\nservice = 'app'\nrepository = 'busybox'\nrule = 'floating-tag'\nreason = 'approved'\n[[published_port]]\nport = 80\nprotocol = 'tcp'\nreason = 'shared port'\nbindings = [{compose='stacks/app/compose.yaml', service='app', host_ip='*'}]\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "approvals.toml"), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadEnvExemptions(dir)
			if tt.valid && err != nil {
				t.Fatal(err)
			}
			if !tt.valid && !errors.Is(err, ErrValidate) {
				t.Fatalf("expected validation failure, got %v", err)
			}
		})
	}
}
