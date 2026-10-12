package envtemplate_test

import (
	"strings"
	"testing"
)

func TestDescriptionsRetainCommentSourceOrder(t *testing.T) {
	root, cfg, target := setup(t, `# @env VALUE: first
name: app
services:
  app:
    image: busybox
    # @env VALUE: second
    environment:
      VALUE: ${VALUE:-one} # @env VALUE: third

# @env VALUE: fourth
`)
	result := generate(t, root, cfg, target)
	text := string(result.Content)
	previous := -1
	for _, word := range []string{"first", "second", "third", "fourth"} {
		at := strings.Index(text, "## "+word+"\n")
		if at < previous || at < 0 {
			t.Fatalf("wrong description order for %s:\n%s", word, text)
		}
		previous = at
	}
}
