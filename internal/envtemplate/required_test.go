package envtemplate_test

import (
	"strings"
	"testing"
)

func TestUnconditionalRequiredDominatesPassthroughAndPlainUses(t *testing.T) {
	root, cfg, target := setup(t, `name: app
services:
  app:
    image: busybox
    environment:
      TOKEN:
      MACHINE: ${MACHINE:?required}
      RESOURCE: resource=${MACHINE}
  init:
    image: busybox
    environment:
      TOKEN: ${TOKEN:?required}
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	for _, name := range []string{"TOKEN", "MACHINE"} {
		if strings.Count(text, "\n"+name+"=\n") != 1 || !strings.Contains(text, "# Required; must not be empty.\n"+name+"=") {
			t.Fatalf("required constraint not combined:\n%s", text)
		}
	}
}
