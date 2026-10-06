package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommand(t *testing.T) {
	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(nil)

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := buffer.String()
	if !strings.Contains(got, "Available Commands:") {
		t.Fatalf("output missing help text:\n%s", got)
	}
}

func TestRootCommandCompletion(t *testing.T) {
	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"completion", "bash"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := buffer.String(); !strings.Contains(got, "# bash completion V2 for stacklint") {
		t.Fatalf("completion output missing CLI name:\n%s", got)
	}
}

func TestVersionCommand(t *testing.T) {
	command := NewRootCommand("test")
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := buffer.String(); got != "stacklint test\n" {
		t.Fatalf("output = %q, want version", got)
	}
}
