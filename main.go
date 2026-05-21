package main

import (
	"fmt"
	"os"

	"github.com/dorukardahan/nole/internal/cli"
	"github.com/dorukardahan/nole/internal/safeerr"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, safeerr.Message(err))
		os.Exit(1)
	}
}
