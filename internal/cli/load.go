package cli

import (
	"fmt"
	"path/filepath"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// loadStateCtx resolves the graph file and loads the state for read-only
// commands. On success it sets ctx.GraphFile and returns (s, file, 0). On
// failure it writes err:/fix: to ctx.ErrOut, sets ctx.ErrMsg, and returns
// (nil, "", 3).
func loadStateCtx(ctx *Context) (*state.State, string, int) {
	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return nil, "", 3
	}
	ctx.GraphFile = file

	cfg := state.ConfigFromEnv()
	s, loadErr := state.Load(file, cfg)
	if loadErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return nil, "", 3
	}
	return s, file, 0
}

// loadRun is the Run handler for `tm load <file> [--src-root <dir>] [--local]`.
//
// Makes an existing graph active by writing the `file` pointer (and
// `src-root` when given) into the user config, or into .tmconfig with
// --local, and appending a load event. Output: the chained status output, identical to running
// `tm status --file <file>` (§6 command table, §1 chaining rule).
//
// load is a read-only operation on the graph: it does not take the lock and
// does not mutate the graph file. A real mutation later will refuse via the
// ops engine if the graph fails lint.
//
// Exit codes:
//
//	0  ok
//	3  file missing, parse error, or config write error
func loadRun(ctx *Context) int {
	file := ctx.Positionals[0]
	ctx.GraphFile = file

	// Validate: the file must exist and parse successfully.
	cfg := state.ConfigFromEnv()
	s, loadErr := state.Load(file, cfg)
	if loadErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Format check (§4.6): refuse a graph whose format differs from CurrentFormat.
	// Done here (on the positional file) rather than in the central dispatcher,
	// since at load time there is no configured pointer yet to resolve.
	if n := s.Graph().FormatN(); n != graph.CurrentFormat {
		base := filepath.Base(file)
		errMsg := formatMismatchMsg(base, n)
		var fixMsg string
		if n < graph.CurrentFormat {
			fixMsg = fmt.Sprintf("tm migrate %s", file)
		} else {
			fixMsg = "upgrade tm"
		}
		ctx.ErrMsg = errMsg
		ctx.FixMsg = fixMsg
		writeErrFix(ctx.ErrOut, errMsg, fixMsg)
		return 1
	}

	// Write the pointer only on success, so a failed load changes nothing.
	if werr := writeActivePointer(ctx, file); werr != nil {
		ctx.ErrMsg = werr.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Append the load event to the event log (§10).
	evlog := eventlog.New(eventlog.Path(file), errlog.RealClock)
	evlog.Append(eventlog.NewRow("load", map[string]any{"file": file}))

	// Chain the status output: set FileFlag so statusRun resolves the graph
	// directly without touching the config or $TM_FILE (§1 chaining rule).
	ctx.FileFlag = file
	return statusRun(ctx)
}
