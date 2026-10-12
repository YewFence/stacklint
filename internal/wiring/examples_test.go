package wiring

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExampleRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	stackDir := filepath.Join(repo, "stacks", "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := `name: app
include:
  - ../module/compose.yaml
services:
  app:
    image: busybox
    # @env-example TOKEN: from-stack
    environment:
      TOKEN: ${TOKEN:?required}
`
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	module := `services:
  module:
    image: busybox
    environment:
      # @env-example MODULE_VALUE: from-module
      MODULE_VALUE: ${MODULE_VALUE:?required}
`
	if err := os.MkdirAll(filepath.Join(repo, "stacks", "module"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "stacks", "module", "compose.yaml"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, stackDir
}

func TestExampleValuesCoversIncludeGraph(t *testing.T) {
	_, stackDir := writeExampleRepo(t)
	values, err := ExampleValues([]string{filepath.Join(stackDir, "compose.yaml")}, stackDir)
	if err != nil {
		t.Fatal(err)
	}
	if values["TOKEN"] != "from-stack" || values["MODULE_VALUE"] != "from-module" {
		t.Fatalf("got %v", values)
	}
}

func TestExampleValuesConflictFails(t *testing.T) {
	repo := t.TempDir()
	stackDir := filepath.Join(repo, "stacks", "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := `name: app
services:
  app:
    image: busybox
    # @env-example TOKEN: one
    environment:
      TOKEN: ${TOKEN:?required}
  init:
    # @env-example TOKEN: two
    image: busybox
`
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ExampleValues([]string{filepath.Join(stackDir, "compose.yaml")}, stackDir)
	if err == nil {
		t.Fatal("conflicting examples accepted")
	}
}

func TestExampleValuesToleratesBrokenGraph(t *testing.T) {
	repo := t.TempDir()
	stackDir := filepath.Join(repo, "stacks", "app")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "name: app\ninclude:\n  - ../missing/compose.yaml\nservices: {}\n"
	if err := os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	values, err := ExampleValues([]string{filepath.Join(stackDir, "compose.yaml")}, stackDir)
	if err != nil || len(values) != 0 {
		t.Fatalf("broken graph should yield empty best-effort result: %v %v", values, err)
	}
}
