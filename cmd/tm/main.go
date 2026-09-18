// Package main is the entry point for the tm CLI.
package main

import (
	"fmt"
	"os"

	"github.com/reithan/teach-me/internal/version"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(3)
	}

	switch args[0] {
	case "--version", "-version":
		fmt.Println(version.Version())
	case "version":
		fmt.Println(version.Version())
	case "--help", "-help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "err: unknown command %q\n", args[0])
		fmt.Fprintf(os.Stderr, "fix: tm --version | tm version\n")
		os.Exit(3)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: tm <command> [args]")
	fmt.Fprintln(os.Stderr, "  tm --version   print version")
	fmt.Fprintln(os.Stderr, "  tm version     print version")
}
