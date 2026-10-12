package envtemplate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/envtemplate"
	"github.com/compose-spec/compose-go/v2/dotenv"
)

func TestMissingExampleFails(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      TOKEN: ${TOKEN:?required}
      MACHINE: ${MACHINE:?required}
      OPTIONAL: ${OPTIONAL:-default}
`)
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err == nil || !strings.Contains(err.Error(), "MACHINE") || !strings.Contains(err.Error(), "TOKEN") || !strings.Contains(err.Error(), "@env-example") {
		t.Fatalf("missing examples not reported in full: %v", err)
	}
	if strings.Contains(err.Error(), "OPTIONAL") {
		t.Fatalf("optional input misreported: %v", err)
	}
}

func TestStaleExampleFails(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      # @env-example VALUE: 3004
      VALUE: ${VALUE:-default}
`)
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale example accepted: %v", err)
	}
}

func TestExampleInMultiVariableConfigContent(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    configs:
      - source: app-config
        target: /etc/app/config.json
configs:
  # @env-example TOKEN: s3cret
  app-config:
    content: |
      {
        "level": "${LEVEL:-info}",
        "password": "${TOKEN:?required}"
      }
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	if !strings.Contains(text, "## Required; Example: s3cret") {
		t.Fatalf("example missing for required variable in multi-variable config content:\n%s", text)
	}
}

func TestExampleOnSourcedInputAllowed(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    # @env-source MACHINE: mise run ready
    # @env-example MACHINE: ci-node
    hostname: ${MACHINE:?required}
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	if strings.Contains(text, "MACHINE=") || !strings.Contains(text, "## MACHINE: mise run ready") {
		t.Fatalf("sourced input with example must stay reference-only in template:\n%s", text)
	}
}

func TestExampleOnWiringVariableFails(t *testing.T) {
	root, cfg, target := setup(t, `name: app
include:
  - path: ../../module/compose.yaml
    env_file: fixed.env
services:
  app:
    image: busybox
    # @env-example DB_PATH: /shadowed.db
`)
	write(t, root, "module/compose.yaml", "services:\n  module:\n    image: busybox\n    environment:\n      DB_PATH: ${DB_PATH:?required}\n")
	write(t, root, "stacks/app/fixed.env", "DB_PATH=/real.db\n")
	cfg.Env.IncludeEnvFiles = []string{"stacks/app/fixed.env"}
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err == nil || !strings.Contains(err.Error(), "shadowed") {
		t.Fatalf("example on wiring variable accepted: %v", err)
	}
}

func TestExampleOnEmptyAllowedRequired(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      # @env-example VALUE: fallback
      VALUE: ${VALUE?required}
`)
	result := generate(t, root, cfg, target)
	if !strings.Contains(string(result.Content), "## Required; may be empty; Example: fallback\nVALUE=\n") {
		t.Fatalf("example not emitted for empty-allowed required:\n%s", result.Content)
	}
}

func TestExampleEncodingRoundTrip(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      # @env-example VALUE: it's a "quoted" value
      VALUE: ${VALUE:?required}
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	marker := "## Required; Example: "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("example missing:\n%s", text)
	}
	line := text[at+len(marker) : strings.Index(text[at:], "\n")+at]
	parsed, err := dotenv.ParseWithLookup(strings.NewReader("VALUE="+line+"\n"), func(string) (string, bool) { return "", false })
	if err != nil || parsed["VALUE"] != `it's a "quoted" value` {
		t.Fatalf("example encoding does not round trip: %q, %v", line, err)
	}
}

func TestConflictingExamplesFail(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    # @env-example VALUE: one
    environment:
      VALUE: ${VALUE:?required}
  init:
    # @env-example VALUE: two
    image: busybox
    environment:
      VALUE: ${VALUE:?required}
`)
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, []string{target.Output})
	if err == nil || !strings.Contains(err.Error(), "conflicting @env-example") {
		t.Fatalf("conflicting examples accepted: %v", err)
	}
}
