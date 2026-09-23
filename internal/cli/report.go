package cli

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/report"
	"github.com/reithan/teach-me/internal/state"
)

// reportRun is the Run handler for
//
//	tm report <concept> [--hops N] [--fulltext] [--passed-only]
//
// Read-only. Walks parent edges from concept up to N hops (default unbounded
// for outline, 2 for --fulltext) and emits a Markdown report on stdout.
//
// Exit codes:
//
//	0  ok
//	3  unknown concept, bad hops value, or file error
func reportRun(ctx *Context) int {
	conceptID := ctx.Positionals[0]

	// Parse --hops.
	hops := 0 // unbounded
	fulltext := len(ctx.Flags["fulltext"]) > 0
	if v := ctx.Flags["hops"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			ctx.ErrMsg = fmt.Sprintf("--hops must be a non-negative integer, got %q", v[0])
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		hops = n
	} else if fulltext {
		// Default hops for --fulltext is 2 when not explicitly set.
		hops = 2
	}

	passedOnly := len(ctx.Flags["passed-only"]) > 0

	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	cfg := state.ConfigFromEnv()
	s, loadErr := state.Load(file, cfg)
	if loadErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	g := s.Graph()
	srcRoot := cite.SrcRoot(filepath.Dir(file))

	concepts, walkErr := report.Walk(g, s, conceptID, hops)
	if walkErr != nil {
		ctx.ErrMsg = walkErr.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg,
			fmt.Sprintf("tm status to see known concept ids"))
		return 3
	}

	opts := report.Options{
		Fulltext:   fulltext,
		PassedOnly: passedOnly,
		SrcRoot:    srcRoot,
	}

	var reader report.TextReader
	if fulltext {
		reader = func(citeStr, root string) (string, error) {
			return readCiteText(citeStr, root)
		}
	}

	out := report.Render(concepts, opts, reader)
	_, _ = fmt.Fprint(ctx.Out, out)
	return 0
}
