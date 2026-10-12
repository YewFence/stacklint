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
      # @env-example TOKEN: token-example
      TOKEN:
      # @env-example MACHINE: machine-example
      MACHINE: ${MACHINE:?required}
      RESOURCE: resource=${MACHINE}
  init:
    image: busybox
    environment:
      TOKEN: ${TOKEN:?required}
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	for name, example := range map[string]string{"TOKEN": "token-example", "MACHINE": "machine-example"} {
		if strings.Count(text, "\n"+name+"=\n") != 1 || !strings.Contains(text, "## Required; Example: "+example+"\n"+name+"=") {
			t.Fatalf("required constraint not combined:\n%s", text)
		}
	}
}
