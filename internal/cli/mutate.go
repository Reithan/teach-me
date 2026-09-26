package cli

import (
	"fmt"
	"path/filepath"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// nodeSets holds the ID membership sets that apply closures check against.
type nodeSets struct {
	AllConcepts map[string]bool // passed + untested (+ reserve when withReserve)
	PassedSet   map[string]bool // passed concepts only
	UntestedSet map[string]bool // untested concepts only
	ReserveSet  map[string]bool // reserve concepts only
	AllNodes    map[string]bool // AllConcepts + Q/A testing items
}

// graphNodeSets builds every nodeSets field from g. Activate, prune, and
// reserve operate on reserve concepts, and add and link may attach edges to
// them; every other command must treat a reserve ID as unknown, so they pass
// withReserve=false.
func graphNodeSets(g *graph.Graph, withReserve bool) nodeSets {
	nc := len(g.PassedConcepts) + len(g.UntestedConcepts) + len(g.ReserveConcepts)
	allConcepts := make(map[string]bool, nc)
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	untestedSet := make(map[string]bool, len(g.UntestedConcepts))
	reserveSet := make(map[string]bool, len(g.ReserveConcepts))

	for _, c := range g.PassedConcepts {
		allConcepts[c.ID] = true
		passedSet[c.ID] = true
	}
	for _, c := range g.UntestedConcepts {
		allConcepts[c.ID] = true
		untestedSet[c.ID] = true
	}
	for _, c := range g.ReserveConcepts {
		if withReserve {
			allConcepts[c.ID] = true
		}
		reserveSet[c.ID] = true
	}

	allNodes := make(map[string]bool, nc+2*len(g.TestingItems))
	for id := range allConcepts {
		allNodes[id] = true
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			allNodes[item.Q.ID] = true
		}
		if item.A != nil {
			allNodes[item.A.ID] = true
		}
	}

	return nodeSets{
		AllConcepts: allConcepts,
		PassedSet:   passedSet,
		UntestedSet: untestedSet,
		ReserveSet:  reserveSet,
		AllNodes:    allNodes,
	}
}

// runMutation is a shared helper for mutating commands. It resolves the graph
// file via §3 precedence, calls ops.Mutate with the caller-supplied Apply
// function, and handles all Refusal and error cases uniformly.
//
// On success it prints "ok\n" to ctx.Out and returns 0.
// On refusal it sets ctx.ErrMsg/FixMsg, writes err:/fix: to ctx.ErrOut, and
// returns refusal.Exit.
// On engine error (lock failure, read/write error) it sets ctx.ErrMsg, writes
// err: to ctx.ErrOut, and returns 3.
func runMutation(ctx *Context, apply ops.Apply) int {
	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	stateCfg := state.ConfigFromEnv()
	lintCfg := buildLintConfig(file)

	_, refusal, engErr := ops.Mutate(file, stateCfg, lintCfg, errlog.RealClock, apply)
	if engErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), engErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if refusal != nil {
		ctx.ErrMsg = refusal.Err
		ctx.FixMsg = refusal.Fix
		writeErrFix(ctx.ErrOut, refusal.Err, refusal.Fix)
		return refusal.Exit
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// runMutationWithFile is like runMutation but accepts an already-resolved file
// path, avoiding a second call to state.ResolveFile.
func runMutationWithFile(ctx *Context, file string, apply ops.Apply) int {
	stateCfg := state.ConfigFromEnv()
	lintCfg := buildLintConfig(file)

	_, refusal, engErr := ops.Mutate(file, stateCfg, lintCfg, errlog.RealClock, apply)
	if engErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), engErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if refusal != nil {
		ctx.ErrMsg = refusal.Err
		ctx.FixMsg = refusal.Fix
		writeErrFix(ctx.ErrOut, refusal.Err, refusal.Fix)
		return refusal.Exit
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}
