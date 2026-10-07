// Package refload materializes the repository tree at a given Git revision
// into a temporary directory, so stacks can be discovered and rendered as of
// that revision. Only repository objects are read through go-git; the working
// tree is never touched. jj repositories are read through their git backend.
package refload

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Materialize extracts the full tree at ref into a fresh temporary directory
// and returns its path. The caller removes it (os.RemoveAll) when done.
// The ref accepts anything git rev-parse would: branch, tag, hash, or
// relative expressions like "main~2". Any file shape other than a regular
// file or symlink (e.g. a gitlink submodule entry) fails the materialization
// rather than producing an incomplete tree.
func Materialize(repoRoot, ref string) (string, error) {
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("open git repository at %s: %w", repoRoot, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("resolve revision %q: %w", ref, err)
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return "", fmt.Errorf("revision %q does not resolve to a commit: %w", ref, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("read tree of %q: %w", ref, err)
	}

	dir, err := os.MkdirTemp("", "stacklint-ref-*")
	if err != nil {
		return "", fmt.Errorf("create materialization directory: %w", err)
	}
	// The tree is written whole; a partial tree would silently corrupt every
	// repository-relative resolution downstream.
	if err := writeTree(dir, tree); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("materialize %q: %w", ref, err)
	}
	return dir, nil
}

// Resolve reports whether ref resolves to a commit, returning its hash.
func Resolve(repoRoot, ref string) (string, error) {
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("open git repository at %s: %w", repoRoot, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("resolve revision %q: %w", ref, err)
	}
	if _, err := repo.CommitObject(*hash); err != nil {
		return "", fmt.Errorf("revision %q does not resolve to a commit: %w", ref, err)
	}
	return hash.String(), nil
}

// IsAncestor reports whether the ancestor ref is an ancestor of the
// descendant ref (by merge base).
func IsAncestor(repoRoot, ancestor, descendant string) (bool, error) {
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return false, fmt.Errorf("open git repository at %s: %w", repoRoot, err)
	}
	ancestorHash, err := repo.ResolveRevision(plumbing.Revision(ancestor))
	if err != nil {
		return false, fmt.Errorf("resolve revision %q: %w", ancestor, err)
	}
	descendantHash, err := repo.ResolveRevision(plumbing.Revision(descendant))
	if err != nil {
		return false, fmt.Errorf("resolve revision %q: %w", descendant, err)
	}
	ancestorCommit, err := repo.CommitObject(*ancestorHash)
	if err != nil {
		return false, fmt.Errorf("revision %q does not resolve to a commit: %w", ancestor, err)
	}
	descendantCommit, err := repo.CommitObject(*descendantHash)
	if err != nil {
		return false, fmt.Errorf("revision %q does not resolve to a commit: %w", descendant, err)
	}
	return ancestorCommit.IsAncestor(descendantCommit)
}

func writeTree(dir string, tree *object.Tree) error {
	return tree.Files().ForEach(func(f *object.File) error {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		if !filepath.IsLocal(f.Name) {
			return fmt.Errorf("tree entry %q escapes the materialization directory", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		reader, err := f.Reader()
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		content, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		switch f.Mode {
		case filemode.Symlink:
			return os.Symlink(string(content), path)
		case filemode.Dir, filemode.Submodule:
			return fmt.Errorf("tree entry %q has unsupported mode %s", f.Name, f.Mode)
		default:
			return os.WriteFile(path, content, 0o644)
		}
	})
}
