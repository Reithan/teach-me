package cli

import (
	"fmt"
	"path/filepath"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

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
