package refload

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

func commitFiles(t *testing.T, files map[string]string) (repoDir string) {
	t.Helper()
	repoDir = t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	require.NoError(t, err)
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	for name, content := range files {
		path := filepath.Join(repoDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		_, err := worktree.Add(name)
		require.NoError(t, err)
	}
	_, err = worktree.Commit("fixture", &git.CommitOptions{
		Author: &object.Signature{Name: "fixture", Email: "fixture@example.com", When: time.Unix(1700000000, 0)},
	})
	require.NoError(t, err)
	return repoDir
}

func TestMaterializeExtractsTreeAtRef(t *testing.T) {
	repoDir := commitFiles(t, map[string]string{
		"stacklint.toml":                 "[repository]\n",
		"stacks/edge/app/compose.yaml":   "name: app\n",
		"stacks/edge/app/.env.example":   "# A=1\n",
		"stacks/compose/mod/compose.yaml": "name: mod\n",
	})

	dir, err := Materialize(repoDir, "HEAD")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	for name, want := range map[string]string{
		"stacklint.toml":                 "[repository]\n",
		"stacks/edge/app/compose.yaml":   "name: app\n",
		"stacks/edge/app/.env.example":   "# A=1\n",
		"stacks/compose/mod/compose.yaml": "name: mod\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		require.NoError(t, err, name)
		require.Equal(t, want, string(got), name)
	}

	// The working tree must be untouched: removing the materialized directory
	// leaves the repository exactly as it was.
	require.NoError(t, os.RemoveAll(dir))
	_, err = os.Stat(filepath.Join(repoDir, "stacks/edge/app/compose.yaml"))
	require.NoError(t, err)
}

func TestResolveAndIsAncestor(t *testing.T) {
	repoDir := commitFiles(t, map[string]string{"compose.yaml": "name: one\n"})
	first, err := Resolve(repoDir, "HEAD")
	require.NoError(t, err)

	repo, err := git.PlainOpen(repoDir)
	require.NoError(t, err)
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "compose.yaml"), []byte("name: two\n"), 0o644))
	_, err = worktree.Add("compose.yaml")
	require.NoError(t, err)
	_, err = worktree.Commit("second", &git.CommitOptions{
		Author: &object.Signature{Name: "fixture", Email: "fixture@example.com", When: time.Unix(1700000060, 0)},
	})
	require.NoError(t, err)

	second, err := Resolve(repoDir, "HEAD")
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	ok, err := IsAncestor(repoDir, first, "HEAD")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = IsAncestor(repoDir, second, first)
	require.NoError(t, err)
	require.False(t, ok)

	_, err = Resolve(repoDir, "does-not-exist")
	require.Error(t, err)
}

func TestMaterializeRejectsUnknownRef(t *testing.T) {
	repoDir := commitFiles(t, map[string]string{"compose.yaml": "name: app\n"})
	_, err := Materialize(repoDir, "does-not-exist")
	require.ErrorContains(t, err, "resolve revision")
}
