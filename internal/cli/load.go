package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/state"
)

// loadRun is the Run handler for `tm load <file>`.
//
// Makes an existing graph active by writing .tmconfig and appending a load
// event. Output: the chained status output, identical to running
// `tm status --file <file>` (§6 command table, §1 chaining rule).
//
// load is a read-only operation on the graph: it does not take the lock and
// does not mutate the graph file. A real mutation later will refuse via the
// ops engine if the graph fails lint.
//
// Exit codes:
//
//	0  ok
//	3  file missing, parse error, or .tmconfig write error
func loadRun(ctx *Context) int {
	file := ctx.Positionals[0]
	ctx.GraphFile = file

	// Validate: the file must exist and parse successfully.
	cfg := state.ConfigFromEnv()
	if _, loadErr := state.Load(file, cfg); loadErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Write .tmconfig only on success, so a failed load leaves no sidecar.
	if werr := writeTMConfig(file); werr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot write .tmconfig: %v", werr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Append the load event to the event log (§10).
	evlog := eventlog.New(eventlog.Path(file), errlog.RealClock)
	evlog.Append(eventlog.NewRow("load", map[string]any{"file": file}))

	// Chain the status output: set FileFlag so statusRun resolves the graph
	// directly without touching .tmconfig or $TM_FILE (§1 chaining rule).
	ctx.FileFlag = file
	return statusRun(ctx)
}
