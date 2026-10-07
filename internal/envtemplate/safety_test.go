package envtemplate_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/envtemplate"
)

func TestGenerationRejectsReplacingAnInputFile(t *testing.T) {
	root, cfg, target := setup(t, "name: app\nservices: {app: {image: busybox}}\n")
	target.Output = target.Files[0]
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, nil)
	if err == nil || !strings.Contains(err.Error(), "input file") {
		t.Fatalf("source can be overwritten: %v", err)
	}
}

func TestMalformedInputShapesFail(t *testing.T) {
	for _, compose := range []string{
		"name: app\nservices: {app: {image: busybox, environment: true}}\n",
		"name: app\nservices: {app: {image: busybox, build: {args: true}}}\n",
		"name: app\nservices: {app: {image: busybox, environment: [null]}}\n",
	} {
		t.Run(compose, func(t *testing.T) {
			root, cfg, target := setup(t, compose)
			_, err := envtemplate.Generate(context.Background(), cfg, root, target, nil)
			if err == nil {
				t.Fatal("unsupported shape was accepted")
			}
		})
	}
}

func TestSourceSymlinksFail(t *testing.T) {
	root, cfg, target := setup(t, "name: app\nservices: {app: {image: busybox}}\n")
	actual := write(t, root, "real-compose.yaml", "name: app\nservices: {app: {image: busybox}}\n")
	if err := os.Remove(target.Files[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, target.Files[0]); err != nil {
		t.Fatal(err)
	}
	_, err := envtemplate.Generate(context.Background(), cfg, root, target, nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("got %v", err)
	}
}
