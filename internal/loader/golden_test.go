package loader

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/discover"
)

// Golden tests catch behavior changes in the embedded compose-go library for
// covered scenarios, even when no stacklint code changes.
// Regenerate deliberately with: go test ./internal/loader -update

var update = flag.Bool("update", false, "regenerate golden files")

const goldenComposeBase = `name: golden
services:
  web:
    image: nginx:${NGINX_TAG:-1.27}
    profiles: ["${WEB_PROFILE:-web}"]
    ports:
      - target: 80
        published: "${WEB_PORT:-8080}"
        host_ip: ${BIND_ADDRESS:-127.0.0.1}
        protocol: tcp
    environment:
      GREETING: ${GREETING:-hi}
      REQUIRED: ${REQUIRED_VAR:?must be set}
      NESTED: ${URL:-https://${DOMAIN:-example.com}}
      LITERAL_DOLLAR: "$${NOT_INTERPOLATED}"
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost/health || exit 1"]
    labels:
      - "app=golden"
      - "host=${DOMAIN:-example.com}"
  worker:
    image: busybox:latest
    profiles: ["worker"]
    command: ["sh", "-c", "echo $$WORKER_READY"]
    volumes:
      - data:/data
volumes:
  data:
    name: golden_data
networks:
  default:
    name: golden_default
`

const goldenComposeOverlay = `services:
  web:
    environment:
      GREETING: overridden
      EXTRA: ${EXTRA_VAR:-added-by-overlay}
`

func writeGoldenRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	stackDir := filepath.Join(repo, "stacks", "edge", "golden")
	writeFile(t, filepath.Join(stackDir, "compose.yaml"), goldenComposeBase)
	writeFile(t, filepath.Join(stackDir, "compose.overlay.yaml"), goldenComposeOverlay)
	writeFile(t, filepath.Join(stackDir, ".env.example"), "GREETING=from-env-file\nREQUIRED_VAR=present\n")
	return repo, stackDir
}

func TestRenderGoldenInterpolated(t *testing.T) {
	repo, stackDir := writeGoldenRepo(t)
	cfg := baseConfig()
	stack := stackAt(stackDir, "stacks/edge/golden")

	sr, err := Render(context.Background(), cfg, repo, stack)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	assertGolden(t, "render_interpolated", sr.Project)
}

func TestRenderGoldenUninterpolated(t *testing.T) {
	repo, stackDir := writeGoldenRepo(t)
	cfg := baseConfig()
	stack := stackAt(stackDir, "stacks/edge/golden")

	sr, err := Render(context.Background(), cfg, repo, stack)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	assertGolden(t, "render_uninterpolated", sr.Raw)
}

func TestRenderGoldenMergeOrder(t *testing.T) {
	repo, stackDir := writeGoldenRepo(t)
	cfg := baseConfig()
	stack := stackAt(stackDir, "stacks/edge/golden")

	sr, err := RenderCombination(context.Background(), cfg, repo, stack,
		[]string{
			filepath.Join(stackDir, "compose.yaml"),
			filepath.Join(stackDir, "compose.overlay.yaml"),
		}, nil)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	assertGolden(t, "render_merged", sr.Project)
	assertGolden(t, "render_merged_uninterpolated", sr.Raw)
}

func TestRenderGoldenIncludeAndAnchors(t *testing.T) {
	repo := t.TempDir()
	stackDir := filepath.Join(repo, "stacks", "edge", "golden")
	writeFile(t, filepath.Join(stackDir, "compose.yaml"), `name: golden
include:
  - path: ../../../modules/compose.yaml
    project_directory: ../../../module-project
    env_file: module.env
services:
  web:
    image: nginx:1.27
    environment:
      PROJECT_VALUE: ${PROJECT_VALUE:?project input required}
`)
	writeFile(t, filepath.Join(stackDir, ".env.example"), "PROJECT_VALUE=from-project\n")
	writeFile(t, filepath.Join(stackDir, "module.env"), "MODULE_VALUE=from-include-env\nPROJECT_VALUE=from-include-env\n")
	writeFile(t, filepath.Join(repo, "modules", "compose.yaml"), `include:
  - nested/compose.yaml
x-environment: &environment
  ANCHORED: ${ANCHOR_VALUE:-anchored-default}
  OVERRIDDEN: anchor-value
services:
  module:
    image: busybox:latest
    environment:
      <<: *environment
      OVERRIDDEN: service-value
      MODULE_VALUE: ${MODULE_VALUE:?module input required}
      PROJECT_VALUE: ${PROJECT_VALUE:?project input required}
`)
	writeFile(t, filepath.Join(repo, "module-project", "nested", "compose.yaml"), `services:
  nested:
    image: alpine:3.21
    environment:
      NESTED: ${NESTED_VALUE:-nested-default}
      MODULE_VALUE: ${MODULE_VALUE:?nested module input required}
`)

	sr, err := Render(context.Background(), baseConfig(), repo, stackAt(stackDir, "stacks/edge/golden"))
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	assertGolden(t, "render_include_interpolated", sr.Project)
	assertGolden(t, "render_include_uninterpolated", sr.Raw)
}

func stackAt(dir, rel string) (s discover.Stack) {
	s.Dir = dir
	s.ComposeFile = filepath.Join(dir, "compose.yaml")
	s.RelPath = rel
	return s
}

func assertGolden(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	data = append(data, '\n')
	golden := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", golden, err)
	}
	if string(want) != string(data) {
		var b strings.Builder
		b.WriteString("render output diverged from golden; if this compose-go upgrade is intentional, regenerate with: go test ./internal/loader -update\n")
		wantLines := strings.Split(string(want), "\n")
		gotLines := strings.Split(string(data), "\n")
		for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
			var w, g string
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if w != g {
				fmt.Fprintf(&b, "first divergence at line %d:\n- %s\n+ %s\n", i+1, w, g)
				break
			}
		}
		t.Error(b.String())
	}
}
