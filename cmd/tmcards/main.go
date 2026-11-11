// Package main is the entry point for tmcards, the flashcard review tool
// (spec §17.5).
package main

import (
	"os"

	"github.com/reithan/teach-me/internal/review"
)

func main() {
	os.Exit(review.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
