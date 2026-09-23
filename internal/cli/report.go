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
//	tm report [<concept>] [--hops N] [--fulltext] [--passed-only]
//
// Read-only. Emits a Markdown report on stdout.
//
// With a concept: walks parent (prerequisite) edges from that concept. hops
// defaults to unbounded for outline mode, 2 for --fulltext. --hops 0 returns
// only the start concept.
//
// Without a concept: emits the whole graph, starting from roots (concepts with
// no prerequisites), following child edges. Default depth limit is 5.
// --hops N overrides (--hops 0 = roots only).
//
// Exit codes:
//
//	0  ok
//	3  unknown concept, bad --hops value, or file error
func reportRun(ctx *Context) int {
	hasConcept := len(ctx.Positionals) > 0
	var conceptID string
	if hasConcept {
		conceptID = ctx.Positionals[0]
	}

	fulltext := len(ctx.Flags["fulltext"]) > 0
	passedOnly := len(ctx.Flags["passed-only"]) > 0

	// hops/depth: sentinel -1 means "use default for the chosen mode".
	// With concept: default -1 (unbounded) for outline, 2 for --fulltext.
	// Without concept: default -1 (WalkAll substitutes 5 internally).
	hops := -1
	if hasConcept && fulltext {
		hops = 2
	}
	if v := ctx.Flags["hops"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			ctx.ErrMsg = fmt.Sprintf("--hops must be a non-negative integer, got %q", v[0])
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		hops = n
	}

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

	var concepts []report.ConceptInfo
	if hasConcept {
		concepts, err = report.Walk(g, s, conceptID, hops)
		if err != nil {
			ctx.ErrMsg = err.Error()
			writeErrFix(ctx.ErrOut, ctx.ErrMsg,
				"tm status to see known concept ids")
			return 3
		}
	} else {
		concepts = report.WalkAll(g, s, hops)
	}

	opts := report.Options{
		Fulltext:   fulltext,
		PassedOnly: passedOnly,
		SrcRoot:    srcRoot,
	}

	// reader wraps readCiteText and cite.CheckDrift so the report package
	// goes through the CLI's single read funnel rather than calling cite
	// primitives directly (same pair show.go uses for drift annotation).
	var reader report.TextReader
	if fulltext {
		reader = func(citeStr, root string) (string, bool, error) {
			text, err := readCiteText(citeStr, root)
			if err != nil {
				return "", false, err
			}
			drifted, _ := checkCiteDrift(citeStr, root)
			return text, drifted, nil
		}
	}

	out := report.Render(concepts, opts, reader)
	_, _ = fmt.Fprint(ctx.Out, out)
	return 0
}
