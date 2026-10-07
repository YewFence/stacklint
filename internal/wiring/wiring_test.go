package wiring_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/wiring"
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

func TestSourceFilesFollowsIncludes(t *testing.T) {
	root := t.TempDir()
	main := write(t, root, "stacks/app/compose.yaml", "name: app\ninclude:\n  - ../compose/mod/compose.yaml\nservices: {}\n")
	module := write(t, root, "stacks/compose/mod/compose.yaml", "include:\n  - path: nested/compose.yaml\nservices: {}\n")
	nested := write(t, root, "stacks/compose/mod/nested/compose.yaml", "services: {}\n")

	sources, err := wiring.SourceFiles([]string{main}, filepath.Dir(main))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 || sources[0] != main || sources[1] != module || sources[2] != nested {
		t.Fatalf("unexpected source files: %v", sources)
	}
}

func TestSourceFilesProjectDirectory(t *testing.T) {
	root := t.TempDir()
	// path resolves against the stack working directory; project_directory
	// only becomes the working directory of the included file's own includes.
	write(t, root, "modules/mod/compose.yaml", "include:\n  - nested/compose.yaml\nservices: {}\n")
	nested := write(t, root, "modules/mod/nested/compose.yaml", "services: {}\n")
	main := write(t, root, "stacks/app/compose.yaml", "name: app\ninclude:\n  - path: ../../modules/mod/compose.yaml\n    project_directory: ../../modules/mod\nservices: {}\n")

	sources, err := wiring.SourceFiles([]string{main}, filepath.Dir(main))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "modules/mod/compose.yaml")
	if len(sources) != 3 || sources[1] != want || sources[2] != nested {
		t.Fatalf("unexpected source files: %v", sources)
	}
}

func TestIncludedModules(t *testing.T) {
	root := t.TempDir()
	main := write(t, root, "stacks/app/compose.yaml", "name: app\ninclude:\n  - ../compose/mod/compose.yaml\nservices: {}\n")
	write(t, root, "stacks/compose/mod/compose.yaml", "services: {}\n")
	write(t, root, "stacks/compose/other/compose.yaml", "services: {}\n")
	cfg := &config.Config{Modules: []config.ModuleConfig{
		{Compose: "stacks/compose/mod/compose.yaml"},
		{Compose: "stacks/compose/other/compose.yaml"},
	}}

	modules, err := wiring.IncludedModules(cfg, root, []string{main}, filepath.Dir(main))
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 1 || modules[0].Compose != "stacks/compose/mod/compose.yaml" {
		t.Fatalf("unexpected modules: %v", modules)
	}
}

func TestVariables(t *testing.T) {
	root := t.TempDir()
	path := write(t, root, "vars.env", "A=1\nB=${C}\n# comment\n")
	names, err := wiring.Variables(path)
	if err != nil {
		t.Fatal(err)
	}
	if !names["A"] || !names["B"] || names["C"] {
		t.Fatalf("unexpected variables: %v", names)
	}

	missing, err := wiring.Variables(filepath.Join(root, "missing.env"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file should yield empty set: %v %v", missing, err)
	}
}
