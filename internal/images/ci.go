package images

import (
	"fmt"
	"strings"

	"github.com/YewFence/stacklint/internal/refload"
)

// CIBase is the incremental-check decision for a CI run: either a base ref or
// a forced full check.
type CIBase struct {
	Ref  string // empty means full check
	Full bool
}

// gitOps abstracts the git queries CI base resolution needs, for testing.
type gitOps interface {
	hasCommit(ref string) bool
	isAncestor(ancestor, descendant string) bool
}

type repoGit struct{ repoRoot string }

func (g repoGit) hasCommit(ref string) bool {
	_, err := refload.Resolve(g.repoRoot, ref)
	return err == nil
}

func (g repoGit) isAncestor(ancestor, descendant string) bool {
	ok, err := refload.IsAncestor(g.repoRoot, ancestor, descendant)
	return err == nil && ok
}

// ResolveCIBase mirrors the Crow CI mode selection of the original
// images-digest-check tooling:
//   - pull_request(_edited): base is the remote ref of CI_COMMIT_TARGET_BRANCH.
//   - push: full when the previous pipeline did not succeed; otherwise base is
//     CI_PREV_COMMIT_SHA, which must exist and be an ancestor of HEAD.
//   - cron and manual: full.
//
// Missing or unresolvable bases fail the run instead of silently degrading.
func ResolveCIBase(provider string, getenv func(string) string, repoRoot string) (CIBase, error) {
	if provider != "crow" {
		return CIBase{}, fmt.Errorf("unsupported CI provider %q (supported: crow)", provider)
	}
	return resolveCrowBase(getenv, repoGit{repoRoot})
}

func resolveCrowBase(getenv func(string) string, git gitOps) (CIBase, error) {
	switch event := getenv("CI_PIPELINE_EVENT"); event {
	case "pull_request", "pull_request_edited":
		branch := getenv("CI_COMMIT_TARGET_BRANCH")
		if branch == "" {
			return CIBase{}, fmt.Errorf("PR check requires CI_COMMIT_TARGET_BRANCH")
		}
		ref := "refs/remotes/origin/" + branch
		if !git.hasCommit(ref) {
			return CIBase{}, fmt.Errorf("PR check base is unavailable: %s", ref)
		}
		return CIBase{Ref: ref}, nil
	case "push":
		if !strings.EqualFold(getenv("CI_PREV_PIPELINE_STATUS"), "success") {
			return CIBase{Full: true}, nil
		}
		base := getenv("CI_PREV_COMMIT_SHA")
		if base == "" || !git.hasCommit(base) {
			return CIBase{}, fmt.Errorf("push check base CI_PREV_COMMIT_SHA is unavailable")
		}
		if !git.isAncestor(base, "HEAD") {
			return CIBase{}, fmt.Errorf("push check base is not an ancestor of HEAD")
		}
		return CIBase{Ref: base}, nil
	case "cron", "manual":
		return CIBase{Full: true}, nil
	default:
		return CIBase{}, fmt.Errorf("unsupported CI_PIPELINE_EVENT: %q", event)
	}
}
