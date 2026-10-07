package images

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeGit struct {
	commits   map[string]bool
	ancestors map[string]bool // "ancestor\x00descendant" pairs that hold
}

func (g fakeGit) hasCommit(ref string) bool { return g.commits[ref] }

func (g fakeGit) isAncestor(ancestor, descendant string) bool {
	return g.ancestors[ancestor+"\x00"+descendant]
}

func getenvWith(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestResolveCrowBasePullRequest(t *testing.T) {
	git := fakeGit{commits: map[string]bool{"refs/remotes/origin/main": true}}
	base, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":        "pull_request",
		"CI_COMMIT_TARGET_BRANCH": "main",
	}), git)
	require.NoError(t, err)
	require.Equal(t, CIBase{Ref: "refs/remotes/origin/main"}, base)
}

func TestResolveCrowBasePullRequestMissingBranch(t *testing.T) {
	_, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT": "pull_request",
	}), fakeGit{})
	require.ErrorContains(t, err, "CI_COMMIT_TARGET_BRANCH")
}

func TestResolveCrowBasePullRequestUnavailableBase(t *testing.T) {
	_, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":        "pull_request",
		"CI_COMMIT_TARGET_BRANCH": "main",
	}), fakeGit{})
	require.ErrorContains(t, err, "base is unavailable")
}

func TestResolveCrowBasePushWithSuccessfulPreviousPipeline(t *testing.T) {
	git := fakeGit{
		commits:   map[string]bool{"abc123": true},
		ancestors: map[string]bool{"abc123\x00HEAD": true},
	}
	base, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":       "push",
		"CI_PREV_PIPELINE_STATUS": "success",
		"CI_PREV_COMMIT_SHA":      "abc123",
	}), git)
	require.NoError(t, err)
	require.Equal(t, CIBase{Ref: "abc123"}, base)
}

func TestResolveCrowBasePushWithFailedPreviousPipeline(t *testing.T) {
	base, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":       "push",
		"CI_PREV_PIPELINE_STATUS": "failure",
	}), fakeGit{})
	require.NoError(t, err)
	require.Equal(t, CIBase{Full: true}, base)
}

func TestResolveCrowBasePushWithUnavailableBase(t *testing.T) {
	_, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":       "push",
		"CI_PREV_PIPELINE_STATUS": "success",
	}), fakeGit{})
	require.ErrorContains(t, err, "CI_PREV_COMMIT_SHA is unavailable")
}

func TestResolveCrowBasePushWithNonAncestorBase(t *testing.T) {
	git := fakeGit{commits: map[string]bool{"abc123": true}}
	_, err := resolveCrowBase(getenvWith(map[string]string{
		"CI_PIPELINE_EVENT":       "push",
		"CI_PREV_PIPELINE_STATUS": "success",
		"CI_PREV_COMMIT_SHA":      "abc123",
	}), git)
	require.ErrorContains(t, err, "not an ancestor of HEAD")
}

func TestResolveCrowBaseCronAndManualAreFull(t *testing.T) {
	for _, event := range []string{"cron", "manual"} {
		base, err := resolveCrowBase(getenvWith(map[string]string{"CI_PIPELINE_EVENT": event}), fakeGit{})
		require.NoError(t, err, event)
		require.Equal(t, CIBase{Full: true}, base, event)
	}
}

func TestResolveCrowBaseUnknownEvent(t *testing.T) {
	_, err := resolveCrowBase(getenvWith(map[string]string{}), fakeGit{})
	require.ErrorContains(t, err, "unsupported CI_PIPELINE_EVENT")
}

func TestResolveCIBaseRejectsUnknownProvider(t *testing.T) {
	_, err := ResolveCIBase("github", getenvWith(map[string]string{}), t.TempDir())
	require.ErrorContains(t, err, "unsupported CI provider")
}
