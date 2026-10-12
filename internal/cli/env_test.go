package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/cli"
)

func writeEnvFile(t *testing.T, root, path, content string) string {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func repository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeEnvFile(t, root, "stacklint.toml", `[repository]
deployment_roots = ["stacks"]
compose_filenames = ["compose.yaml"]
`)
	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox\n    environment:\n      VALUE: ${VALUE:-one}\n      OPTIONAL:\n")
	return root
}

func executeEnv(t *testing.T, root string, args ...string) (int, []byte) {
	t.Helper()
	command := cli.NewRootCommand("test")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(append([]string{"--config-dir", root, "env"}, args...))
	err := command.Execute()
	return cli.ExitCode(err), output.Bytes()
}

func TestEnvGenerateCheckAndDrift(t *testing.T) {
	root := repository(t)
	template := filepath.Join(root, "stacks/app/.env.example")
	code, output := executeEnv(t, root, "check", "app", "--format", "json")
	if code != 1 {
		t.Fatalf("missing template should be drift: %d %s", code, output)
	}
	if _, err := os.Stat(template); !os.IsNotExist(err) {
		t.Fatalf("check wrote output: %v", err)
	}
	code, output = executeEnv(t, root, "generate", "app", "--format", "json")
	if code != 0 {
		t.Fatalf("generation: %d %s", code, output)
	}
	generated, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	code, output = executeEnv(t, root, "check", "app", "--format", "json")
	if code != 0 {
		t.Fatalf("check: %d %s", code, output)
	}
	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox\n    environment:\n      VALUE: ${VALUE:-two}\n      OPTIONAL:\n")
	code, output = executeEnv(t, root, "check", "--format", "json")
	if code != 1 {
		t.Fatalf("expected drift: %d %s", code, output)
	}
	unchanged, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, unchanged) {
		t.Fatal("check modified template")
	}
	code, output = executeEnv(t, root, "generate", "app")
	if code != 0 {
		t.Fatalf("regeneration: %d %s", code, output)
	}
	updated, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("# VALUE=two\n")) {
		t.Fatalf("default did not follow source: %s", updated)
	}
}

func TestEnvGenerateIsAllPreparedBeforeWriting(t *testing.T) {
	root := repository(t)
	existing := []byte("existing hand-maintained template\n")
	writeEnvFile(t, root, "stacks/app/.env.example", string(existing))
	writeEnvFile(t, root, "stacks/broken/compose.yaml", "name: broken\nservices:\n  app:\n    extends: {file: missing.yaml, service: app}\n")
	code, output := executeEnv(t, root, "generate", "app", "broken", "--format", "json")
	if code != 2 {
		t.Fatalf("expected incomplete analysis: %d %s", code, output)
	}
	actual, err := os.ReadFile(filepath.Join(root, "stacks/app/.env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(existing, actual) {
		t.Fatal("partial generation modified prior template")
	}
	if _, err := os.Stat(filepath.Join(root, "stacks/broken/.env.example")); !os.IsNotExist(err) {
		t.Fatal("failed target created template")
	}
	code, output = executeEnv(t, root, "check", "--format", "json")
	if code != 2 || !bytes.Contains(output, []byte("env.drift")) || !bytes.Contains(output, []byte("env.analysis")) {
		t.Fatalf("check failed to aggregate targets: %d %s", code, output)
	}
}

func TestEnvVariantsSelectionAndStaticInputs(t *testing.T) {
	root := repository(t)
	configuration, err := os.ReadFile(filepath.Join(root, "stacklint.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeEnvFile(t, root, "stacklint.toml", string(configuration)+`
[[env.variant]]
stack = "stacks/app"
name = "local"
files = ["stacks/app/compose.yaml", "stacks/app/compose.local.yaml"]
output = ".env.local.example"
`)
	writeEnvFile(t, root, "stacks/app/compose.local.yaml", "services:\n  app:\n    environment:\n      VALUE: ${VALUE:-local}\n")
	code, output := executeEnv(t, root, "generate", "app", "--format", "json")
	if code != 0 {
		t.Fatalf("expected both variants: %d %s", code, output)
	}
	for _, test := range []struct{ file, want string }{{".env.example", "# VALUE=one"}, {".env.local.example", "# VALUE=local"}} {
		data, err := os.ReadFile(filepath.Join(root, "stacks/app", test.file))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(test.want)) {
			t.Fatalf("bad variant %s: %s", test.file, data)
		}
	}
	code, output = executeEnv(t, root, "check", "app", "--variant", "local", "--format", "json")
	if code != 0 || bytes.Contains(output, []byte(`"variant":"base"`)) {
		t.Fatalf("selector: %d %s", code, output)
	}
	code, output = executeEnv(t, root, "check", "app", "--variant", "unknown", "--format", "json")
	if code != 2 {
		t.Fatalf("missing selector should fail: %d %s", code, output)
	}
}

func TestEnvExemptions(t *testing.T) {
	for _, kind := range []string{"pending", "upstream", "env-file-passthrough"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			extra := ""
			if kind != "pending" {
				extra += "reason = \"hand-maintained interface\"\n"
			}
			if kind == "upstream" {
				extra += "source = \"https://example.invalid/template\"\n"
			}
			writeEnvFile(t, root, "approvals.toml", "[[env_exemption]]\nstack = \"stacks/app\"\nkind = \""+kind+"\"\n"+extra)
			if kind == "env-file-passthrough" {
				writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox\n    env_file: .env\n")
			}
			writeEnvFile(t, root, "stacks/app/.env.example", "hand-maintained content\n")
			code, output := executeEnv(t, root, "check", "--format", "json")
			if code != 0 || !bytes.Contains(output, []byte(`"status":"exempt"`)) {
				t.Fatalf("exemption: %d %s", code, output)
			}
			code, output = executeEnv(t, root, "generate", "app", "--format", "json")
			if code != 2 {
				t.Fatalf("exempt generation accepted: %d %s", code, output)
			}
			writeEnvFile(t, root, "stacks/app/.env.example", "# Generated by stacklint; do not edit.\n")
			code, output = executeEnv(t, root, "check", "--format", "json")
			if code != 1 || !bytes.Contains(output, []byte("env.exemption-generated")) {
				t.Fatalf("generated exemption accepted: %d %s", code, output)
			}
		})
	}
}

func TestStaleEnvExemptionsRespectScope(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "approvals.toml", "[[env_exemption]]\nstack = \"stacks/missing\"\nkind = \"pending\"\n")
	code, output := executeEnv(t, root, "generate", "app")
	if code != 0 {
		t.Fatalf("unrelated stale exemption blocks local generation: %d %s", code, output)
	}
	code, output = executeEnv(t, root, "check", "app", "--format", "json")
	if code != 0 {
		t.Fatalf("unrelated stale exemption affects scoped check: %d %s", code, output)
	}
	code, output = executeEnv(t, root, "check", "--format", "json")
	if code != 1 || !bytes.Contains(output, []byte("env.exemption-stale")) {
		t.Fatalf("stale target not detected: %d %s", code, output)
	}
	writeEnvFile(t, root, "approvals.toml", "[[env_exemption]]\nstack = \"stacks/app\"\nkind = \"env-file-passthrough\"\nreason = \"passthrough interface\"\n")
	writeEnvFile(t, root, "stacks/app/.env.example", "manual\n")
	code, output = executeEnv(t, root, "check", "app", "--format", "json")
	if code != 1 || !bytes.Contains(output, []byte("no service-level")) {
		t.Fatalf("stale precondition not detected: %d %s", code, output)
	}
}

func TestSymlinkOutputNotReadOrReplaced(t *testing.T) {
	root := repository(t)
	secret := writeEnvFile(t, root, "private-values", "fake-private-marker-629af4\n")
	path := filepath.Join(root, "stacks/app/.env.example")
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"check", "generate"} {
		code, output := executeEnv(t, root, action, "app", "--format", "json")
		if code != 2 || bytes.Contains(output, []byte("fake-private-marker")) {
			t.Fatalf("symlink unsafe: %d %s", code, output)
		}
	}
	unchanged, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "fake-private-marker-629af4\n" {
		t.Fatal("symlink target modified")
	}
}

func TestEnvUsageAndConfigErrors(t *testing.T) {
	root := repository(t)
	for _, args := range [][]string{{"generate"}, {"check", "missing"}, {"check", "app", "--format", "yaml"}} {
		code, _ := executeEnv(t, root, args...)
		if code != 2 {
			t.Fatalf("usage %v returned %d", args, code)
		}
	}
	writeEnvFile(t, root, "stacklint.toml", "[env]\nunknown = true\n")
	code, output := executeEnv(t, root, "check", "--format", "json")
	if code != 2 {
		t.Fatalf("bad config: %d %s", code, output)
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid JSON error output: %s", output)
	}
}

func TestEnvJSONWarningDoesNotLeakIncludeValues(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\ninclude: [../../module/compose.yaml]\nservices:\n  app:\n    image: busybox\n")
	writeEnvFile(t, root, "module/compose.yaml", "services:\n  module:\n    image: busybox\n    environment:\n      - IMPLICIT_INPUT\n")
	writeEnvFile(t, root, "module/.env", "IMPLICIT_INPUT=fake-secret-do-not-output-a792f\n")
	code, output := executeEnv(t, root, "generate", "app", "--format", "json")
	if code != 0 || bytes.Contains(output, []byte("fake-secret")) || !bytes.Contains(output, []byte("env.include-dotenv")) {
		t.Fatalf("warning/value behavior: %d %s", code, output)
	}
	content, err := os.ReadFile(filepath.Join(root, "stacks/app/.env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "fake-secret") || !strings.Contains(string(content), "# IMPLICIT_INPUT=\n") {
		t.Fatalf("implicit input unsafe: %s", content)
	}
}

func TestEnvCICommand(t *testing.T) {
	root := repository(t)
	writeEnvFile(t, root, "stacks/app/compose.yaml", "name: app\nservices:\n  app:\n    image: busybox\n    environment:\n      # @env-example TOKEN: s3cret value\n      TOKEN: ${TOKEN:?required}\n      VALUE: ${VALUE:-one}\n")
	artifact := filepath.Join(root, "stacks", "app", ".env.ci")

	code, output := executeEnv(t, root, "ci", "app", "--format", "json")
	if code != 0 {
		t.Fatalf("ci generation: %d %s", code, output)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Generated by stacklint from @env-example markers; do not edit.\nTOKEN='s3cret value'\n"
	if string(data) != want {
		t.Fatalf("artifact content:\n%s", data)
	}
	if code, output := executeEnv(t, root, "generate", "app"); code != 0 {
		t.Fatalf("template generation: %d %s", code, output)
	}
	if code, output := executeEnv(t, root, "check", "--format", "json"); code != 0 {
		t.Fatalf("fresh artifact should pass check: %d %s", code, output)
	}

	writeEnvFile(t, root, "stacks/app/.env.ci", "TOKEN=stale\n")
	code, output = executeEnv(t, root, "check", "--format", "json")
	if code != 1 || !bytes.Contains(output, []byte("env.ci-drift")) {
		t.Fatalf("expected ci drift: %d %s", code, output)
	}

	if err := os.Remove(artifact); err != nil {
		t.Fatal(err)
	}
	if code, output := executeEnv(t, root, "check", "--format", "json"); code != 0 {
		t.Fatalf("absent artifact is the consumer's choice and must pass: %d %s", code, output)
	}
}

func TestEnvCICommandVariant(t *testing.T) {
	root := repository(t)
	configuration, err := os.ReadFile(filepath.Join(root, "stacklint.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeEnvFile(t, root, "stacklint.toml", string(configuration)+`
[[env.variant]]
stack = "stacks/app"
name = "local"
files = ["stacks/app/compose.yaml", "stacks/app/compose.local.yaml"]
output = ".env.local.example"
`)
	writeEnvFile(t, root, "stacks/app/compose.local.yaml", "services:\n  app:\n    environment:\n      # @env-example EXTRA: variant-only\n      EXTRA: ${EXTRA:?required}\n")
	code, output := executeEnv(t, root, "ci", "app", "--variant", "local")
	if code != 0 {
		t.Fatalf("variant ci generation: %d %s", code, output)
	}
	data, err := os.ReadFile(filepath.Join(root, "stacks", "app", ".env.ci.local"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("EXTRA=variant-only\n")) {
		t.Fatalf("variant artifact content:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, "stacks", "app", ".env.ci")); !os.IsNotExist(err) {
		t.Fatalf("base artifact should not be written by variant run: %v", err)
	}
}
