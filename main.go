package main

import (
	"os"

	"github.com/dorukardahan/searchmcp/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
