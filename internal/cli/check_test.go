package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/YewFence/stacklint/internal/cli"
)

func executeCheck(t *testing.T, root string, args ...string) (int, []byte) {
	t.Helper()
	command := cli.NewRootCommand("test")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(append([]string{"--config-dir", root, "check"}, args...))
	err := command.Execute()
	return cli.ExitCode(err), output.Bytes()
}

func TestCheckCommandExitCodes(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "stacklint.toml", `[repository]
deployment_roots = ["stacks"]
compose_filenames = ["compose.yaml"]
`)
	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox\n")

	code, output := executeCheck(t, root, "--format", "json")
	if code != 1 {
		t.Fatalf("unpinned image should fail: %d %s", code, output)
	}
	var report struct {
		Results     []map[string]any `json:"results"`
		Diagnostics []map[string]any `json:"diagnostics"`
		ExitCode    int              `json:"exit_code"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid JSON report: %v\n%s", err, output)
	}
	if report.ExitCode != 1 || len(report.Diagnostics) == 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Diagnostics[0]["rule"] != "image" {
		t.Fatalf("expected image rule, got %v", report.Diagnostics[0])
	}

	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n")
	code, output = executeCheck(t, root)
	if code != 0 {
		t.Fatalf("pinned image should pass: %d %s", code, output)
	}
}
