package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YewFence/stacklint/internal/config"
)

func repo(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("name: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testConfig() *config.Config {
	return &config.Config{
		Repository: config.RepositoryConfig{
			DeploymentRoots:  []string{"stacks/edge", "stacks/yewyard"},
			ComposeFilenames: []string{"compose.yaml", "compose.yml"},
		},
	}
}

func TestFindAll(t *testing.T) {
	root := repo(t,
		"stacks/edge/web/compose.yaml",
		"stacks/yewyard/ntfy/compose.yml",
		"stacks/yewyard/forgejo/compose.yaml",
		"stacks/yewyard/not-compose/other.txt",
	)
	stacks, err := FindAll(testConfig(), root)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(stacks) != 3 {
		t.Fatalf("expected 3 stacks, got %d: %v", len(stacks), stacks)
	}
	want := []string{"stacks/edge/web", "stacks/yewyard/forgejo", "stacks/yewyard/ntfy"}
	for i, s := range stacks {
		if s.RelPath != want[i] {
			t.Errorf("stacks[%d].RelPath = %q, want %q", i, s.RelPath, want[i])
		}
	}
}

func TestFindAllMissingRootFails(t *testing.T) {
	root := repo(t, "stacks/edge/web/compose.yaml")
	cfg := testConfig()
	cfg.Repository.DeploymentRoots = append(cfg.Repository.DeploymentRoots, "stacks/missing")
	if _, err := FindAll(cfg, root); err == nil {
		t.Fatal("missing deployment root should fail")
	}
}

func TestMatchTarget(t *testing.T) {
	root := repo(t,
		"stacks/edge/web/compose.yaml",
		"stacks/yewyard/ntfy/compose.yaml",
		"stacks/yewyard/forgejo/compose.yaml",
	)
	stacks, err := FindAll(testConfig(), root)
	if err != nil {
		t.Fatal(err)
	}

	byDir, err := MatchTarget(filepath.Join(root, "stacks/yewyard/ntfy"), stacks, root)
	if err != nil || byDir.RelPath != "stacks/yewyard/ntfy" {
		t.Errorf("match by dir: %v %v", byDir.RelPath, err)
	}

	byFile, err := MatchTarget(filepath.Join(root, "stacks/yewyard/ntfy/compose.yaml"), stacks, root)
	if err != nil || byFile.RelPath != "stacks/yewyard/ntfy" {
		t.Errorf("match by file: %v %v", byFile.RelPath, err)
	}

	byFragment, err := MatchTarget("forgejo", stacks, root)
	if err != nil || byFragment.RelPath != "stacks/yewyard/forgejo" {
		t.Errorf("match by unique fragment: %v %v", byFragment.RelPath, err)
	}

	if _, err := MatchTarget("yewyard", stacks, root); err == nil {
		t.Error("ambiguous fragment should fail")
	}
	if _, err := MatchTarget("nonexistent", stacks, root); err == nil {
		t.Error("no match should fail")
	}
}
