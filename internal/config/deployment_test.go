package config

import (
	"errors"
	"testing"
)

func TestDeploymentAlternativesConfiguration(t *testing.T) {
	cfg, err := Load(writeConfig(t, `[[deployment.alternatives]]
name = "proxy"
stacks = ["stacks/edge/proxy", "stacks/nas/proxy"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Deployment.CanCoexist("stacks/edge/proxy", "stacks/nas/proxy") || cfg.Deployment.CanCoexist("stacks/nas/proxy", "stacks/edge/proxy") {
		t.Fatal("alternative relationship must be symmetric")
	}
	if !cfg.Deployment.CanCoexist("stacks/edge/proxy", "stacks/edge/proxy") || !cfg.Deployment.CanCoexist("stacks/edge/proxy", "stacks/other") {
		t.Fatal("internal and unrelated checks must remain enabled")
	}
}

func TestInvalidDeploymentAlternatives(t *testing.T) {
	for _, content := range []string{
		"name = ''\nstacks = ['stacks/a', 'stacks/b']",
		"name = 'proxy'\nstacks = ['stacks/a']",
		"name = 'proxy'\nstacks = ['stacks/a', 'stacks/a']",
		"name = 'proxy'\nstacks = ['../a', 'stacks/b']",
		"name = 'proxy'\nstacks = ['/stacks/a', 'stacks/b']",
		"name = 'proxy'\nstacks = ['stacks/./a', 'stacks/b']",
		"name = 'proxy'\nstacks = ['stacks/a', 'stacks/b']\nunknown = true",
		"name = 'proxy'\nstacks = ['stacks/a', 'stacks/b']\n[[deployment.alternatives]]\nname = 'proxy'\nstacks = ['stacks/c', 'stacks/d']",
		"name = 'proxy'\nstacks = ['stacks/a', 'stacks/b']\n[[deployment.alternatives]]\nname = 'other'\nstacks = ['stacks/b', 'stacks/c']",
	} {
		t.Run(content, func(t *testing.T) {
			_, err := Load(writeConfig(t, "[[deployment.alternatives]]\n"+content+"\n"))
			if !errors.Is(err, ErrValidate) {
				t.Fatalf("expected invalid alternatives to fail, got %v", err)
			}
		})
	}
}
