package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/YewFence/stacklint/internal/cli"
)

func executeRender(t *testing.T, root string, args ...string) (int, []byte) {
	t.Helper()
	command := cli.NewRootCommand("test")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(append([]string{"--config-dir", root, "render"}, args...))
	err := command.Execute()
	if err != nil {
		// Mirror cli.Execute, which prints unreported errors to stderr.
		output.WriteString(err.Error() + "\n")
	}
	return cli.ExitCode(err), output.Bytes()
}

func TestRenderTargetSelection(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "stacks/other/compose.yaml", "name: other\nservices:\n  svc:\n    image: busybox\n")

	code, output := executeRender(t, root)
	if code != 0 {
		t.Fatalf("full render failed: %d %s", code, output)
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(output, &full); err != nil {
		t.Fatalf("invalid JSON: %s", output)
	}
	if len(full) != 2 {
		t.Fatalf("expected both stacks, got %v", full)
	}
	if _, ok := full["stacks/app"]; !ok {
		t.Fatalf("missing stacks/app key: %s", output)
	}

	code, output = executeRender(t, root, "app")
	if code != 0 {
		t.Fatalf("single render failed: %d %s", code, output)
	}
	var single map[string]json.RawMessage
	if err := json.Unmarshal(output, &single); err != nil {
		t.Fatalf("invalid JSON: %s", output)
	}
	if len(single) != 1 || single["stacks/app"] == nil {
		t.Fatalf("expected only stacks/app: %s", output)
	}
	if string(single["stacks/app"]) != string(full["stacks/app"]) {
		t.Fatalf("single and full views disagree")
	}

	code, output = executeRender(t, root, "app", "other", "--no-interpolate")
	if code != 0 {
		t.Fatalf("multi render failed: %d %s", code, output)
	}
	var multi map[string]json.RawMessage
	if err := json.Unmarshal(output, &multi); err != nil {
		t.Fatalf("invalid JSON: %s", output)
	}
	if len(multi) != 2 {
		t.Fatalf("expected both stacks: %s", output)
	}

	code, _ = executeRender(t, root, "missing")
	if code != 2 {
		t.Fatalf("unknown target accepted: %d", code)
	}
}

func TestRenderFailsClosedOnBrokenStack(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "stacks/broken/compose.yaml", "name: broken\nservices:\n  svc:\n    image: busybox\n    environment:\n      VALUE: ${VALUE:?required}\n")

	code, output := executeRender(t, root)
	if code != 2 {
		t.Fatalf("broken stack accepted: %d %s", code, output)
	}
	if !bytes.Contains(output, []byte("stacks/broken")) {
		t.Fatalf("error missing stack name: %s", output)
	}
	var partial map[string]json.RawMessage
	if err := json.Unmarshal(output, &partial); err == nil {
		t.Fatalf("partial JSON emitted: %s", output)
	}
}
