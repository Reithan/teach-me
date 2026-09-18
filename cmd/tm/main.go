// Package main is the entry point for the tm CLI.
package main

import (
	"os"

	"github.com/reithan/teach-me/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
