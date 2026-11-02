package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyApprovalsFileRequiresMigration(t *testing.T) {
	dir := writeConfig(t, "[repository]\ndeployment_roots = ['stacks']\n")
	if err := os.WriteFile(filepath.Join(dir, "approvals.toml"), []byte("[[image]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if !errors.Is(err, ErrValidate) || !strings.Contains(err.Error(), "[[approvals.<type>]]") {
		t.Fatalf("expected explicit migration error, got %v", err)
	}
}

func TestApprovalNamespaceRejectsUnknownKeys(t *testing.T) {
	for _, content := range []string{
		"[approvals]\nunknown = true\n",
		"[[approvals.image]]\nunknown = true\n",
	} {
		_, err := Load(writeConfig(t, content))
		if !errors.Is(err, ErrValidate) {
			t.Fatalf("expected unknown approval key to fail, got %v", err)
		}
	}
}
