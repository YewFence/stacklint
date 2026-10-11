package envtemplate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/envtemplate"
)

func TestPreambleRendersBeforeAnchorEntry(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      # @env-preamble OPENAI_API_KEY: Optional AI group; disabled when unset.
      # @env-preamble OPENAI_API_KEY: See upstream docs.
      # @env OPENAI_API_KEY: OpenAI compatible API key.
      OPENAI_API_KEY:
      OLLAMA_BASE_URL:
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	want := "# Optional AI group; disabled when unset.\n# See upstream docs.\n\n# OpenAI compatible API key.\n# OPENAI_API_KEY=\n"
	if !strings.Contains(text, want) {
		t.Fatalf("missing preamble block %q in:\n%s", want, text)
	}
	anchor := strings.Index(text, want)
	if neighbor := strings.Index(text, "# OLLAMA_BASE_URL=\n"); neighbor < anchor {
		t.Fatalf("group members not adjacent after anchor:\n%s", text)
	}
}

func TestPreambleOrderDedupeAndEmptyLines(t *testing.T) {
	root, cfg, target := setup(t, `# @env-preamble VALUE: first
name: app
services:
  app:
    image: busybox
    # @env-preamble VALUE: second
    environment:
      # @env-preamble VALUE: first
      # @env-preamble VALUE:
      VALUE: ${VALUE:-x}
`)
	result := generate(t, root, cfg, target)
	want := "# first\n# second\n#\n\n# VALUE=x\n"
	if !strings.Contains(string(result.Content), want) {
		t.Fatalf("missing ordered preamble block %q in:\n%s", want, result.Content)
	}
}

func TestPreambleFailures(t *testing.T) {
	tests := []struct{ name, compose, want string }{
		{"stale anchor", "# @env-preamble WRONG: stale\nservices: {app: {image: busybox}}\n", "no matching input"},
		{"missing colon", "# @env-preamble WRONG without colon\nservices: {app: {image: busybox}}\n", "invalid @env"},
		{"bad name", "# @env-preamble NOT VALID: text\nservices: {app: {image: busybox}}\n", "invalid @env"},
		{"env-source anchor", "services:\n  app:\n    image: busybox\n    # @env-source MACHINE: deployment readiness\n    # @env-preamble MACHINE: group text\n    environment:\n      MACHINE: ${MACHINE}\n", "no assignment entry"},
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
