package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lockfile"
	"github.com/reithan/teach-me/internal/state"
)

// rehashRun is the Run handler for `tm rehash [<file>]`.
//
// It resolves and hashes all hashless citations in the graph file, rewrites the
// file, and logs one "rehash" event per changed citation. If all citations are
// already hashed, it prints "ok" and exits 0 with no file change. Exits 3 on
// any file or citation error.
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
	var changed []string // old→new pairs, but we track new hashed forms

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

	// Walk all concepts (passed + untested).
	for _, c := range g.PassedConcepts {
		for i, citeStr := range c.Cites {
			hashed, didChange, hErr := hashOne(citeStr)
			if hErr != nil {
				errMsg := fmt.Sprintf("citation %q: %v", citeStr, hErr)
				writeErrFix(ctx.ErrOut, errMsg, "")
				ctx.ErrMsg = errMsg
				return 3
			}
			if didChange {
				c.Cites[i] = hashed
				changed = append(changed, hashed)
			}
		}
	}
	for _, c := range g.UntestedConcepts {
		for i, citeStr := range c.Cites {
			hashed, didChange, hErr := hashOne(citeStr)
			if hErr != nil {
				errMsg := fmt.Sprintf("citation %q: %v", citeStr, hErr)
				writeErrFix(ctx.ErrOut, errMsg, "")
				ctx.ErrMsg = errMsg
				return 3
			}
			if didChange {
				c.Cites[i] = hashed
				changed = append(changed, hashed)
			}
		}
	}
	for _, item := range g.TestingItems {
		if item.Q == nil || item.Q.Cite == "" {
			continue
		}
		hashed, didChange, hErr := hashOne(item.Q.Cite)
		if hErr != nil {
			errMsg := fmt.Sprintf("citation %q: %v", item.Q.Cite, hErr)
			writeErrFix(ctx.ErrOut, errMsg, "")
			ctx.ErrMsg = errMsg
			return 3
		}
		if didChange {
			item.Q.Cite = hashed
			changed = append(changed, hashed)
		}
	}

	if len(changed) == 0 {
		_, _ = fmt.Fprintln(ctx.Out, "ok")
		return 0
	}

	// Write the updated graph back.
	outBytes := graph.Write(g)
	if writeErr := lockfile.WriteTempAndRename(file, outBytes); writeErr != nil {
		errMsg := fmt.Sprintf("cannot write %s: %v", filepath.Base(file), writeErr)
		writeErrFix(ctx.ErrOut, errMsg, "")
		ctx.ErrMsg = errMsg
		return 3
	}

	// Log one "rehash" event per changed citation.
	el := eventlog.New(eventlog.Path(file), errlog.RealClock)
	for _, hashed := range changed {
		row := eventlog.NewRow("rehash", map[string]any{"src": hashed})
		el.Append(row)
	}

	_, _ = fmt.Fprintln(ctx.Out, "ok")
	return 0
}
