package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
	"github.com/reithan/teach-me/internal/lockfile"
	"github.com/reithan/teach-me/internal/state"
)

// rehashChange records one updated citation for logging and output.
type rehashChange struct {
	nodeID string // concept or question ID that owns the citation
	before string // old (hashless) citation string
	after  string // new (hashed) citation string
}

// rehashRun is the Run handler for `tm rehash [<file>]`.
//
// It resolves and hashes all hashless citations in the graph file, lints the
// rewritten graph, writes the file, and logs one "rehash" event per changed
// citation. Output is one line per change ("<id> <before> -> <after>") followed
// by "ok". If all citations are already hashed, prints "ok" with no file change.
// Errors from all citations are collected before refusing (exit 3).
func rehashRun(ctx *Context) int {
	flagFile := ctx.FileFlag
	if flagFile == "" && len(ctx.Positionals) > 0 {
		flagFile = ctx.Positionals[0]
	}

	file, err := state.ResolveFile(flagFile)
	if err != nil {
		errMsg := "no graph file"
		fixMsg := "tm rehash <file>"
		writeErrFix(ctx.ErrOut, errMsg, fixMsg)
		ctx.ErrMsg = errMsg
		ctx.FixMsg = fixMsg
		return 3
	}

	ctx.GraphFile = file

	// Acquire lock (same serialization semantics as Mutate).
	lk, lockErr := lockfile.Acquire(file, errlog.RealClock)
	if lockErr != nil {
		errMsg := fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), lockErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}
	defer func() { _ = lk.Release() }()

	data, readErr := os.ReadFile(file)
	if readErr != nil {
		errMsg := fmt.Sprintf("cannot read %s: %v", filepath.Base(file), readErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		errMsg := fmt.Sprintf("cannot parse %s: %v", filepath.Base(file), parseErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	srcRoot := cite.SrcRoot(filepath.Dir(file))

	// Helper: hash one citation string. Returns (hashed, changed, err).
	hashOne := func(citeStr string) (string, bool, error) {
		p, pErr := cite.Parse(citeStr)
		if pErr != nil {
			return citeStr, false, pErr
		}
		// Already hashed: skip.
		if p.Hash != "" {
			return citeStr, false, nil
		}
		hashed, hErr := cite.HashCitation(citeStr, srcRoot)
		if hErr != nil {
			return citeStr, false, hErr
		}
		return hashed, true, nil
	}

	// Collect all changes and errors — do not stop at the first error.
	var changes []rehashChange
	var errs []string

	// Walk passed concepts.
	for _, c := range g.PassedConcepts {
		for i, citeStr := range c.Cites {
			hashed, didChange, hErr := hashOne(citeStr)
			if hErr != nil {
				errs = append(errs, fmt.Sprintf("citation %q: %v", citeStr, hErr))
				continue
			}
			if didChange {
				c.Cites[i] = hashed
				changes = append(changes, rehashChange{nodeID: c.ID, before: citeStr, after: hashed})
			}
		}
	}
	// Walk untested concepts.
	for _, c := range g.UntestedConcepts {
		for i, citeStr := range c.Cites {
			hashed, didChange, hErr := hashOne(citeStr)
			if hErr != nil {
				errs = append(errs, fmt.Sprintf("citation %q: %v", citeStr, hErr))
				continue
			}
			if didChange {
				c.Cites[i] = hashed
				changes = append(changes, rehashChange{nodeID: c.ID, before: citeStr, after: hashed})
			}
		}
	}
	// Walk question citations.
	for _, item := range g.TestingItems {
		if item.Q == nil || item.Q.Cite == "" {
			continue
		}
		hashed, didChange, hErr := hashOne(item.Q.Cite)
		if hErr != nil {
			errs = append(errs, fmt.Sprintf("citation %q: %v", item.Q.Cite, hErr))
			continue
		}
		if didChange {
			changes = append(changes, rehashChange{nodeID: item.Q.ID, before: item.Q.Cite, after: hashed})
			item.Q.Cite = hashed
		}
	}

	// Report all hash errors collected above, then refuse.
	if len(errs) > 0 {
		for _, e := range errs {
			writeErrFix(ctx.ErrOut, e, "")
		}
		ctx.ErrMsg = errs[0]
		return 3
	}

	if len(changes) == 0 {
		_, _ = fmt.Fprintln(ctx.Out, "ok")
		return 0
	}

	// Lint the rewritten graph (skip input lint — input is a legacy graph).
	outBytes := graph.Write(g)
	lintCfg := buildLintConfig(file)
	if viols := lint.Check(outBytes, lintCfg); len(viols) > 0 {
		errMsg := fmt.Sprintf("rewritten graph fails lint: %s", viols[0].Msg)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	// Write the updated graph back.
	if writeErr := lockfile.WriteTempAndRename(file, outBytes); writeErr != nil {
		errMsg := fmt.Sprintf("cannot write %s: %v", filepath.Base(file), writeErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	// Log one "rehash" event per changed citation per §10.
	el := eventlog.New(eventlog.Path(file), errlog.RealClock)
	for _, ch := range changes {
		row := eventlog.NewRow("rehash", map[string]any{
			"id":     ch.nodeID,
			"before": ch.before,
			"after":  ch.after,
		})
		el.Append(row)
	}

	// Print one line per updated citation, then ok.
	for _, ch := range changes {
		_, _ = fmt.Fprintf(ctx.Out, "%s %s -> %s\n", ch.nodeID, ch.before, ch.after)
	}
	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}
