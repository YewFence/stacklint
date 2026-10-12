package envtemplate_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
	"github.com/YewFence/stacklint/internal/envtemplate"
	"github.com/compose-spec/compose-go/v2/dotenv"
)

func write(t *testing.T, root, path, content string) string {
	t.Helper()
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return absolute
}

func setup(t *testing.T, compose string) (string, *config.Config, envtemplate.Target) {
	t.Helper()
	root := t.TempDir()
	main := write(t, root, "stacks/app/compose.yaml", compose)
	stack := discover.Stack{Dir: filepath.Dir(main), ComposeFile: main, RelPath: "stacks/app"}
	cfg := &config.Config{Env: config.EnvConfig{Output: ".env.example"}, Render: config.RenderConfig{EnvFiles: []string{".env.example", ".env.ci"}}}
	target := envtemplate.Target{Stack: stack, Variant: "base", Files: []string{main}, Output: filepath.Join(stack.Dir, ".env.example")}
	return root, cfg, target
}

func generate(t *testing.T, root string, cfg *config.Config, target envtemplate.Target) *envtemplate.Result {
	t.Helper()
	result, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExpressionSemantics(t *testing.T) {
	tests := []struct{ name, expression, want string }{
		{"soft default", "${VALUE:-default}", "# VALUE=default\n"},
		{"hard default", "${VALUE-default}", "## Default applies only when unset, not when empty.\n# VALUE=default\n"},
		{"required nonempty", "${VALUE:?required}", "## Required\nVALUE=\n"},
		{"required empty allowed", "${VALUE?required}", "## Required; may be empty\nVALUE=\n"},
		{"plain", "${VALUE}", "# VALUE=\n"},
		{"unbraced", "$VALUE", "# VALUE=\n"},
		{"empty default", "${VALUE:-}", "# VALUE=\n"},
		{"alternate", "${VALUE:+alternate}", "## Expression at services.app.environment.TEST:\n## ${VALUE:+alternate}\n# VALUE=\n"},
		{"set alternate", "${VALUE+alternate}", "## Expression at services.app.environment.TEST:\n## ${VALUE+alternate}\n# VALUE=\n"},
		{"repeated", "${VALUE:-one}/${VALUE:-two}", "## ${VALUE:-one}/${VALUE:-two}\n# VALUE=\n"},
		{"nested required", "${VALUE:-https://${DOMAIN:?required}}", "# DOMAIN=\n"},
		{"conditional required", "${ENABLE:+${VALUE:?required}}", "# VALUE=\n"},
		{"required message dependency", "${VALUE:?${MESSAGE:?provide message}}", "# MESSAGE=\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cfg, target := setup(t, "name: app\nservices:\n  app:\n    image: busybox\n    environment:\n      TEST: '"+tt.expression+"'\n")
			result := generate(t, root, cfg, target)
			if !strings.Contains(string(result.Content), tt.want) {
				t.Fatalf("missing %q in:\n%s", tt.want, result.Content)
			}
			parsed, err := dotenv.Parse(bytes.NewReader(result.Content))
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "nested required" || tt.name == "conditional required" || tt.name == "required message dependency" {
				if len(parsed) != 0 {
					t.Fatalf("conditional inputs became unconditional: %v", parsed)
				}
			}
		})
	}
}

func TestAssignmentsRoundTrip(t *testing.T) {
	tests := []struct{ fallback, value string }{
		{"text with spaces", "text with spaces"}, {"#hash", "#hash"}, {"it's quoted", "it's quoted"},
		{`C:\path\with\slashes`, `C:\path\with\slashes`}, {"$$NOT_INPUT", "$NOT_INPUT"}, {"trailing$$", "trailing$"}, {"two$$$$", "two$$"}, {"^/?(a|b)$$", "^/?(a|b)$"},
	}
	for _, tt := range tests {
		t.Run(tt.fallback, func(t *testing.T) {
			expression := "${VALUE:-" + tt.fallback + "}"
			quoted := "'" + strings.ReplaceAll(expression, "'", "''") + "'"
			root, cfg, target := setup(t, "name: app\nservices:\n  app:\n    image: busybox\n    environment:\n      VALUE: "+quoted+"\n")
			result := generate(t, root, cfg, target)
			var assignments []string
			for _, line := range strings.Split(string(result.Content), "\n") {
				if strings.HasPrefix(line, "# VALUE=") {
					assignments = append(assignments, strings.TrimPrefix(line, "# "))
				}
			}
			if len(assignments) != 1 {
				t.Fatalf("expected one assignment, got %v in %s", assignments, result.Content)
			}
			parsed, err := dotenv.ParseWithLookup(strings.NewReader(assignments[0]+"\n"), func(string) (string, bool) { return "", false })
			if err != nil || parsed["VALUE"] != tt.value {
				t.Fatalf("round trip: got %q, err %v; want %q", parsed["VALUE"], err, tt.value)
			}
		})
	}
}

func TestSourceOrderAnnotationsAndPassthrough(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    # @env ZED: Last alphabetically, first in source.
    user: "${ZED:-1000}:${ALPHA:-1000}"
    # @env-source MACHINE: deployment readiness
    hostname: ${MACHINE:?required}
    environment:
      # @env OPTIONAL: First explanation.
      # @env OPTIONAL: First explanation.
      # @env OPTIONAL: Second explanation.
      OPTIONAL:
      INLINE: ${INLINE:-yes} # @env INLINE: Inline description.
    build:
      context: .
      args:
        ARG:
  other:
    image: busybox
    environment:
      - LIST_OPTIONAL
      - ASSIGNED=${ASSIGNED:-default}
configs:
  app:
    environment: CONFIG_INPUT
secrets:
  token:
    environment: SECRET_INPUT
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	ordered := []string{"# ALPHA=1000", "# ZED=1000", "# OPTIONAL=", "# INLINE=yes", "# ARG=", "# LIST_OPTIONAL=", "# ASSIGNED=default", "CONFIG_INPUT=", "SECRET_INPUT="}
	previous := -1
	for _, entry := range ordered {
		at := strings.Index(text, entry)
		if at < 0 || at <= previous {
			t.Fatalf("wrong order for %s:\n%s", entry, text)
		}
		previous = at
	}
	if strings.Count(text, "First explanation.") != 1 || strings.Count(text, "Second explanation.") != 1 {
		t.Fatalf("description dedup failed:\n%s", text)
	}
	if strings.Contains(text, "MACHINE=") || !strings.Contains(text, "## MACHINE: deployment readiness") {
		t.Fatalf("source reference failed:\n%s", text)
	}
}

func TestOverlayAndLoadingInputs(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    ports:
      - "${BIND:-127.0.0.1}:8000:80"
    environment:
      LEVEL: ${LEVEL:-info}
      DATABASE: ${OLD_DATABASE:?must be set}
      REMOVE: ${REMOVED:-old}
`)
	overlay := write(t, root, "stacks/app/compose.local.yaml", `services:
  app:
    ports: !reset []
    environment: !override
      LEVEL: ${LEVEL:-debug}
`)
	target.Variant = "local"
	target.Files = append(target.Files, overlay)
	target.Output = filepath.Join(target.Stack.Dir, ".env.local.example")
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	if !strings.Contains(text, "# LEVEL=debug") || strings.Contains(text, "# LEVEL=info") {
		t.Fatalf("effective default incorrect:\n%s", text)
	}
	if !strings.Contains(text, "## Loading-stage inputs") || !strings.Contains(text, "OLD_DATABASE=\n") || !strings.Contains(text, "# REMOVED=\n") {
		t.Fatalf("loading inputs lost:\n%s", text)
	}
	if strings.Count(text, "# LEVEL=") != 1 {
		t.Fatalf("duplicate entry:\n%s", text)
	}
}

func TestInconsistentEffectiveDefaultsFail(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      ONE: ${SHARED:-one}
      TWO: ${SHARED:-two}
`)
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err == nil || !strings.Contains(err.Error(), "inconsistent default") {
		t.Fatalf("got %v", err)
	}
}

func TestIncludeWiringAndImplicitDotenvWarning(t *testing.T) {
	root, cfg, target := setup(t, `name: app
include:
  - path: ../../modules/compose.yaml
    env_file: fixed.env
  - ../../nested/compose.yaml
services:
  app:
    image: busybox
    environment:
      LOCAL: ${LOCAL:-one}
`)
	write(t, root, "modules/compose.yaml", `services:
  sidecar:
    image: busybox
    container_name: ${COMPOSE_PROJECT_NAME}-sidecar
    environment:
      WIRED_PATH: ${WIRED_PATH:?path required}
      MODULE_TOKEN: ${MODULE_TOKEN:?token required}
`)
	write(t, root, "nested/compose.yaml", `services:
  nested:
    image: busybox
    environment:
      - IMPLICIT_VALUE
      - IMPLICIT_EXPRESSION=${IMPLICIT_EXPRESSION:-source-default}
`)
	sentinel := "synthetic-do-not-print-bf7c192"
	write(t, root, "nested/.env", "IMPLICIT_VALUE="+sentinel+"\nIMPLICIT_EXPRESSION="+sentinel+"\n")
	write(t, root, "stacks/app/fixed.env", "WIRED_PATH=/data/app.db\n")
	write(t, root, "stacks/app/.env.ci", "MODULE_TOKEN="+sentinel+"\n")
	write(t, root, "stacks/app/.env", "LOCAL="+sentinel+"\n")
	cfg.Modules = []config.ModuleConfig{{Compose: "modules/compose.yaml", WiringFile: "fixed.env", EnvGroup: "MODULE_"}}
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	if strings.Contains(text, sentinel) || strings.Contains(strings.Join(result.Warnings, "\n"), sentinel) {
		t.Fatalf("implicit/CI value leaked:\n%s\n%v", text, result.Warnings)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "nested/.env") {
		t.Fatalf("missing warning: %v", result.Warnings)
	}
	if strings.Contains(text, "WIRED_PATH=") || !strings.Contains(text, "## WIRED_PATH: stacks/app/fixed.env") {
		t.Fatalf("wiring entry not reference-only:\n%s", text)
	}
	if !strings.Contains(text, "MODULE_TOKEN=\n") || !strings.Contains(text, "# IMPLICIT_VALUE=\n") || !strings.Contains(text, "# IMPLICIT_EXPRESSION=source-default") {
		t.Fatalf("deployment inputs lost:\n%s", text)
	}
	if strings.Contains(text, "COMPOSE_PROJECT_NAME=") {
		t.Fatalf("project name gained entry:\n%s", text)
	}
	again := generate(t, root, cfg, target)
	if !bytes.Equal(result.Content, again.Content) {
		t.Fatal("generation is not byte-deterministic")
	}
}

func TestDerivedWiringUsesPlaceholderButDocumentsDependency(t *testing.T) {
	root, cfg, target := setup(t, `name: app
include:
  - path: ../../modules/compose.yaml
    env_file: fixed.env
services:
  app:
    image: busybox
`)
	write(t, root, "modules/compose.yaml", "services:\n  sidecar:\n    image: busybox\n    environment:\n      DB_PATH: ${DB_PATH:?required}\n")
	write(t, root, "stacks/app/fixed.env", "DB_PATH=${DATA_ROOT}/app.db\n")
	write(t, root, "stacks/app/.env.ci", "DATA_ROOT=/synthetic-ci-root\n")
	cfg.Env.IncludeEnvFiles = []string{"stacks/app/fixed.env"}
	result := generate(t, root, cfg, target)
	if !strings.Contains(string(result.Content), "# DATA_ROOT=\n") || strings.Contains(string(result.Content), "/synthetic-ci-root") {
		t.Fatalf("dependency lost or placeholder leaked:\n%s", result.Content)
	}
}

func TestMergeKeysAndAliases(t *testing.T) {
	root, cfg, target := setup(t, `name: app
x-env: &env
  SHARED: ${SHARED:-one}
services:
  app:
    image: busybox
    environment:
      <<: *env
      LOCAL: ${LOCAL:-two}
`)
	result := generate(t, root, cfg, target)
	if strings.Count(string(result.Content), "# SHARED=one") != 1 || !strings.Contains(string(result.Content), "# LOCAL=two") {
		t.Fatalf("merge inputs incorrect:\n%s", result.Content)
	}
}

func TestUnsupportedAndIncompleteSourcesFail(t *testing.T) {
	tests := []struct{ name, compose, want string }{
		{"extends", "services: {app: {extends: {file: other.yaml, service: base}}}\n", "extends"},
		{"unknown tag", "services: {app: {environment: !unknown {KEY: value}}}\n", "unsupported YAML tag"},
		{"bad annotation", "# @env WRONG without colon\nservices: {app: {image: busybox}}\n", "invalid @env"},
		{"stale annotation", "# @env WRONG: stale\nservices: {app: {image: busybox}}\n", "no matching input"},
		{"bad variable name", "services: {app: {image: busybox, environment: ['NOT VALID']}}\n", "unsupported input name"},
		{"multiple documents", "services: {}\n---\nservices: {}\n", "one mapping document"},
		{"dynamic include", "include: ['${MODULE}/compose.yaml']\nservices: {}\n", "dynamic file graph"},
		{"remote include", "include: ['https://example.invalid/compose.yaml']\nservices: {}\n", "remote Compose"},
		{"duplicate yaml keys", "services: {}\nservices: {}\n", "cannot decode"},
		{"recursive alias", "x-recursive: &x {key: *x}\nservices: {}\n", "cannot decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cfg, target := setup(t, tt.compose)
			_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %s, got %v", tt.want, err)
			}
		})
	}
}

func TestIncludeAuthorizationAndSourceConflicts(t *testing.T) {
	root, cfg, target := setup(t, `name: app
include:
  - path: ../../module-a/compose.yaml
    env_file: a.env
  - path: ../../module-b/compose.yaml
    env_file: b.env
services:
  app:
    image: busybox
`)
	write(t, root, "module-a/compose.yaml", "services:\n  a:\n    image: busybox\n    environment:\n      REGION: ${REGION:?required}\n")
	write(t, root, "module-b/compose.yaml", "services:\n  b:\n    image: busybox\n    environment:\n      REGION: ${REGION:?required}\n")
	write(t, root, "stacks/app/a.env", "REGION=a\n")
	write(t, root, "stacks/app/b.env", "REGION=b\n")
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, nil)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("got %v", err)
	}
	cfg.Env.IncludeEnvFiles = []string{"stacks/app/a.env", "stacks/app/b.env"}
	_, err = envtemplate.Generate(context.Background(), cfg, root, target, nil)
	if err == nil || !strings.Contains(err.Error(), "inconsistent sources") {
		t.Fatalf("got %v", err)
	}
}
