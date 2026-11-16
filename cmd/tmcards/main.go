// Package main is the entry point for tmcards, the flashcard review tool
// (spec §17.5).
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/reithan/teach-me/internal/review"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := review.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
