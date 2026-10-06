package main

import "github.com/YewFence/stacklint/internal/cli"

var version = "dev"

func main() {
	cli.Execute(version)
}
