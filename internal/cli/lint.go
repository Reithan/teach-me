package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// lintRun is the Run handler for `tm lint [<file>]`.
//
// Exact output/exit behavior preserved from the original cmd/tm/main.go so
// that M3 lint tests continue to pass:
//
//	Success:    prints "ok", exit 0.
//	Violations: prints each violation ("line N: msg" or bare "msg"), exit 2.
//	No file:    err/fix to ctx.ErrOut, exit 3.
//	Read error: err to ctx.ErrOut, exit 3.
//
// On exit 2 (violations), ctx.Violations and ctx.ErrMsg are populated so the
// centralized errlog in the dispatcher can include them in the ERRORS.jsonl row.
//
// On exit 3 (file errors), ctx.ErrMsg and ctx.FixMsg are populated for
// the same reason.
func lintRun(ctx *Context) int {
	// Resolve the graph file via §3 precedence. The global --file flag (stored
	// in ctx.FileFlag by the dispatcher) wins; the optional positional serves
	// as the next fallback; state.ResolveFile falls back to $TM_FILE then .tmconfig.
	flagFile := ctx.FileFlag
	if flagFile == "" && len(ctx.Positionals) > 0 {
		flagFile = ctx.Positionals[0]
	}

	file, err := state.ResolveFile(flagFile)
	if err != nil {
		errMsg := "no graph file"
		fixMsg := "tm lint <file>"
		writeErrFix(ctx.ErrOut, errMsg, fixMsg)
		ctx.ErrMsg = errMsg
		ctx.FixMsg = fixMsg
		return 3
	}

	data, readErr := readFile(file)
	if readErr != nil {
		errMsg := fmt.Sprintf("cannot read %s: %v", file, readErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		ctx.GraphFile = file
		return 3
	}

	ctx.GraphFile = file

	// --drift mode: resolve citations and report hash mismatches.
	if _, ok := ctx.Flags["drift"]; ok {
		return lintDrift(ctx, data, file)
	}

	cfg := buildLintConfig(file)
	viols := lint.Check(data, cfg)
	if len(viols) == 0 {
		_, _ = fmt.Fprintln(ctx.Out, "ok")
		return 0
	}

	// Emit violations to stdout and collect strings for the errlog row.
	violStrs := make([]string, 0, len(viols))
	for _, v := range viols {
		var line string
		if v.Line > 0 {
			line = fmt.Sprintf("line %d: %s", v.Line, v.Msg)
		} else {
			line = v.Msg
		}
		_, _ = fmt.Fprintln(ctx.Out, line)
		violStrs = append(violStrs, line)
	}

	ctx.Violations = violStrs
	// ErrMsg is left empty: lint exit 2 prints violations directly to stdout,
	// not an err: line. The Violations field carries the printed text for the
	// ERRORS.jsonl row.
	return 2
}

// lintDrift checks all local citations for content drift.
// It prints "DRIFT <cite>" for hash mismatches and "err: <msg>" for read
// failures, collecting all problems before exiting. Exits 1 if any drift or
// read errors were found; exits 0 if all citations match.
func lintDrift(ctx *Context, data []byte, file string) int {
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		errMsg := fmt.Sprintf("cannot parse %s: %v", filepath.Base(file), parseErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	resolver, resolverErr := source.NewResolver(filepath.Dir(file))
	if resolverErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	anyProblem := false

	// Collect all citations: concept Cites and question Cite fields.
	var citations []string
	for _, c := range g.PassedConcepts {
		citations = append(citations, c.Cites...)
	}
	for _, c := range g.UntestedConcepts {
		citations = append(citations, c.Cites...)
	}
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.Cite != "" {
			citations = append(citations, item.Q.Cite)
		}
	}

	// Check every citation; continue past read errors so all problems are reported.
	for _, citeStr := range citations {
		drifted, _, err := resolver.CheckDrift(citeStr)
		if err != nil {
			errMsg := fmt.Sprintf("citation %q: %v", citeStr, err)
			writeErrFix(ctx.ErrOut, errMsg, "")
			anyProblem = true
			continue
		}
		if drifted {
			_, _ = fmt.Fprintf(ctx.Out, "DRIFT %s\n", citeStr)
			anyProblem = true
		}
	}

	if anyProblem {
		return 1
	}
	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}

// buildLintConfig constructs a lint.Config from §13 environment variables,
// mirroring state.ConfigFromEnv() for the subset of fields lint.Config needs.
func buildLintConfig(file string) lint.Config {
	sc := state.ConfigFromEnv()
	graphDir := filepath.Dir(file)
	// Load source config to get aids-dir for check 16.
	srcCfg, _ := source.LoadConfig()
	return lint.Config{
		SrcRoot:  cite.SrcRoot(graphDir),
		AidsDir:  source.AidsDir(srcCfg, graphDir),
		ProbeMin: sc.ProbeMin,
		ProbeMax: sc.ProbeMax,
		TeachMin: sc.TeachMin,
		TeachMax: sc.TeachMax,
	}
}

// readFile reads the named file using the standard library.
func readFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
