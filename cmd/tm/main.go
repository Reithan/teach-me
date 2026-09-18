// Package main is the entry point for the tm CLI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/lint"
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
	case "lint":
		runLint(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "err: unknown command %q\n", args[0])
		fmt.Fprintf(os.Stderr, "fix: tm --version | tm version | tm lint\n")
		os.Exit(3)
	}
}

// runLint implements the `tm lint [<file>]` subcommand.
func runLint(args []string) {
	// Resolve the graph file path: positional arg, then $TM_FILE.
	var file string
	if len(args) > 0 {
		file = args[0]
	} else {
		file = os.Getenv("TM_FILE")
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "err: no graph file")
		fmt.Fprintln(os.Stderr, "fix: tm lint <file>")
		os.Exit(3)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "err: cannot read %s: %v\n", file, err)
		os.Exit(3)
	}

	cfg := lint.Config{
		SrcRoot:  cite.SrcRoot(filepath.Dir(file)),
		ProbeMin: envIntOr("TM_PROBE_MIN", 2),
		ProbeMax: envIntOr("TM_PROBE_MAX", 5),
		TeachMin: envIntOr("TM_TEACH_MIN", 1),
		TeachMax: envIntOr("TM_TEACH_MAX", 3),
	}

	viols := lint.Check(data, cfg)
	if len(viols) == 0 {
		fmt.Println("ok")
		os.Exit(0)
	}

	for _, v := range viols {
		if v.Line > 0 {
			fmt.Printf("line %d: %s\n", v.Line, v.Msg)
		} else {
			fmt.Println(v.Msg)
		}
	}
	os.Exit(2)
}

// envIntOr returns the integer value of the environment variable key, or def
// when the variable is unset or its value cannot be parsed.
func envIntOr(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: tm <command> [args]")
	fmt.Fprintln(os.Stderr, "  tm --version   print version")
	fmt.Fprintln(os.Stderr, "  tm version     print version")
	fmt.Fprintln(os.Stderr, "  tm lint        validate the graph file")
}
