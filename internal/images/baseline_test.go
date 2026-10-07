package images

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const baselineConfig = `
[repository]
deployment_roots = ["stacks"]

[images]
floating_tags = ["latest", "main"]
concurrency = 8
`

const baselineApprovals = `
[[image]]
compose = "stacks/app/compose.yaml"
service = "app"
repository = "ghcr.io/example/app"
rule = "exact-tag"
tag = "1.0"
reason = "upstream has no digests"
`

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func TestPolicyChangedAtRefUnchanged(t *testing.T) {
	current, baseline := t.TempDir(), t.TempDir()
	writeFile(t, current, "stacklint.toml", baselineConfig)
	writeFile(t, current, "approvals.toml", baselineApprovals)
	writeFile(t, baseline, "stacklint.toml", "# comment shuffle\n"+baselineConfig)
	writeFile(t, baseline, "approvals.toml", baselineApprovals+"\n# trailing comment\n")

	changed, err := PolicyChangedAtRef(current, baseline)
	require.NoError(t, err)
	require.False(t, changed, "comments must not count as policy changes")
}

func TestPolicyChangedAtRefImagesSectionChanged(t *testing.T) {
	current, baseline := t.TempDir(), t.TempDir()
	writeFile(t, current, "stacklint.toml", baselineConfig)
	writeFile(t, baseline, "stacklint.toml", baselineConfig+"per_registry_concurrency = 4\n")

	changed, err := PolicyChangedAtRef(current, baseline)
	require.NoError(t, err)
	require.True(t, changed)
}

func TestPolicyChangedAtRefImageApprovalsChanged(t *testing.T) {
	current, baseline := t.TempDir(), t.TempDir()
	writeFile(t, current, "stacklint.toml", baselineConfig)
	writeFile(t, current, "approvals.toml", baselineApprovals)
	writeFile(t, baseline, "stacklint.toml", baselineConfig)

	changed, err := PolicyChangedAtRef(current, baseline)
	require.NoError(t, err)
	require.True(t, changed, "a removed [[image]] approval must trigger a full check")
}

func TestPolicyChangedAtRefIgnoresUnrelatedSections(t *testing.T) {
	current, baseline := t.TempDir(), t.TempDir()
	writeFile(t, current, "stacklint.toml", baselineConfig)
	writeFile(t, baseline, "stacklint.toml", baselineConfig+"\n[ports]\nshared_roots = [\"stacks\"]\n")

	changed, err := PolicyChangedAtRef(current, baseline)
	require.NoError(t, err)
	require.False(t, changed, "unrelated sections must not trigger a full check")
}

func TestPolicyChangedAtRefMissingBaselineConfigCountsAsChanged(t *testing.T) {
	current, baseline := t.TempDir(), t.TempDir()
	writeFile(t, current, "stacklint.toml", baselineConfig)

	changed, err := PolicyChangedAtRef(current, baseline)
	require.NoError(t, err)
	require.True(t, changed)
}

func TestPolicyChangedAtRefMissingCurrentConfigFails(t *testing.T) {
	baseline := t.TempDir()
	writeFile(t, baseline, "stacklint.toml", baselineConfig)

	_, err := PolicyChangedAtRef(t.TempDir(), baseline)
	require.Error(t, err)
}
