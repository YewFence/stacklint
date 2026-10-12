package envtemplate_test

import (
	"strings"
	"testing"
)

func TestRequiredWiringDependencyIsNotHiddenByCI(t *testing.T) {
	root, cfg, target := setup(t, `name: app
include:
  - path: ../../module/compose.yaml
    env_file: fixed.env
services:
  app:
    image: busybox
`)
	write(t, root, "module/compose.yaml", "services:\n  module:\n    image: busybox\n    environment:\n      DB_PATH: ${DB_PATH:?required}\n")
	write(t, root, "stacks/app/fixed.env", "DB_PATH=${DATA_ROOT:?provide root}/app.db\n")
	write(t, root, "stacks/app/.env.ci", "DATA_ROOT=/synthetic-placeholder\n")
	cfg.Env.IncludeEnvFiles = []string{"stacks/app/fixed.env"}
	result := generate(t, root, cfg, target)
	if !strings.Contains(string(result.Content), "## Required\nDATA_ROOT=\n") || strings.Contains(string(result.Content), "/synthetic-placeholder") {
		t.Fatalf("required dependency lost:\n%s", result.Content)
	}
}
