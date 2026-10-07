package loader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const composeYAML = `name: web
services:
  app:
    image: nginx:${NGINX_TAG:-1.27}
    ports:
      - target: 80
        published: 8080
        host_ip: ${BIND_ADDRESS:-127.0.0.1}
    environment:
      GREETING: ${GREETING:-hi}
      FIXED: always
`

func setupStack(t *testing.T) (string, discover.Stack) {
	t.Helper()
	repo := t.TempDir()
	composePath := filepath.Join(repo, "stacks", "edge", "web", "compose.yaml")
	writeFile(t, composePath, composeYAML)
	return repo, discover.Stack{
		Dir:         filepath.Dir(composePath),
		ComposeFile: composePath,
		RelPath:     "stacks/edge/web",
	}
}

func baseConfig() *config.Config {
	return &config.Config{
		Render: config.RenderConfig{EnvFiles: []string{".env.example", ".env.ci"}},
	}
}

func TestRenderInterpolatesWithEnvFiles(t *testing.T) {
	repo, stack := setupStack(t)
	writeFile(t, filepath.Join(stack.Dir, ".env.example"), "NGINX_TAG=1.27\nGREETING=hello\n")

	sr, err := Render(context.Background(), baseConfig(), repo, stack)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	svc := sr.Project.Services["app"]
	if svc.Image != "nginx:1.27" {
		t.Errorf("interpolated image: got %q", svc.Image)
	}
	if svc.Environment["GREETING"] == nil || *svc.Environment["GREETING"] != "hello" {
		t.Errorf("interpolated GREETING: got %v", svc.Environment["GREETING"])
	}
}

func TestRenderMissingEnvFilesSkipped(t *testing.T) {
	repo, stack := setupStack(t)
	// No env files written; defaults in compose must apply.
	sr, err := Render(context.Background(), baseConfig(), repo, stack)
	if err != nil {
		t.Fatalf("missing env files should be skipped: %v", err)
	}
	if sr.Project.Services["app"].Image != "nginx:1.27" {
		t.Errorf("default interpolation: got %q", sr.Project.Services["app"].Image)
	}
}

func TestRenderRawViewPreservesExpressions(t *testing.T) {
	repo, stack := setupStack(t)
	writeFile(t, filepath.Join(stack.Dir, ".env.example"), "GREETING=hello\n")

	sr, err := Render(context.Background(), baseConfig(), repo, stack)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	services, ok := sr.Raw["services"].(map[string]any)
	if !ok {
		t.Fatalf("raw services not a map: %T", sr.Raw["services"])
	}
	app, ok := services["app"].(map[string]any)
	if !ok {
		t.Fatalf("raw app not a map: %T", services["app"])
	}
	if app["image"] != "nginx:${NGINX_TAG:-1.27}" {
		t.Errorf("raw image should preserve expression, got %q", app["image"])
	}
	env, ok := app["environment"].(map[string]any)
	if !ok {
		t.Fatalf("raw environment not a map: %T", app["environment"])
	}
	if env["GREETING"] != "${GREETING:-hi}" {
		t.Errorf("raw GREETING should preserve expression, got %v", env["GREETING"])
	}
}

func TestRenderModuleCIEnvFlattened(t *testing.T) {
	repo, stack := setupStack(t)
	cfg := baseConfig()
	cfg.Modules = []config.ModuleConfig{
		{Compose: "stacks/compose/litestream/compose.yaml", CIEnvFile: ".env.ci.litestream"},
	}
	writeFile(t, filepath.Join(repo, "stacks", "compose", "litestream", ".env.ci.litestream"), "GREETING=from-module\n")

	sr, err := Render(context.Background(), cfg, repo, stack)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if got := sr.Project.Services["app"].Environment["GREETING"]; got == nil || *got != "from-module" {
		t.Errorf("module CI env should flatten into render inputs, got %v", got)
	}
}
