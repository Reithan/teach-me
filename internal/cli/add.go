package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// addRun is the Run handler for:
//
//	tm add <id> <cite> "<scope>" [--parent <id>:"<rel>"]... [--child <id>:"<rel>"]...
//
// Adds a new concept to the untested block. --parent P:"rel" inserts an edge
// P→<id> (P is a prerequisite above the new concept). --child C:"rel" inserts
// an edge <id>→C (the new concept is a prerequisite above the existing C).
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (id exists, cycle, etc.)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID, bad citation
func addRun(ctx *Context) int {
	id := ctx.Positionals[0]
	citeStr := ctx.Positionals[1]
	scope := ctx.Positionals[2]

	usageLine := FindCommand("add").Usage()

	// ── Pre-Mutate validation (exit 3) ───────────────────────────────────────

	// Validate concept ID.
	if !graph.ValidConceptID(id) {
		ctx.ErrMsg = fmt.Sprintf("%q is not a valid concept ID", id)
		ctx.FixMsg = usageLine
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	// Resolve the graph file now so we can validate the citation against SrcRoot.
	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	// Hash the citation: resolve, compute SHA-256 prefix, return hashed form.
	resolver, resolverErr := source.NewResolver(filepath.Dir(file))
	if resolverErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	hashedCite, citeMeta, hashErr := resolver.HashCitation(citeStr)
	if hashErr != nil {
		return citeHashError(ctx, citeStr, aidRefusalWithID(hashErr, id), usageLine)
	}
	citeStr = hashedCite

	// Parse --parent and --child flags.
	parents, childPairs, flagErr := parseEndpointFlags(ctx.Flags["parent"], ctx.Flags["child"])
	if flagErr != "" {
		ctx.ErrMsg = flagErr
		ctx.FixMsg = usageLine
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	// ── Apply closure ────────────────────────────────────────────────────────
	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Build the set of all existing concepts for lookup.
		existsInPassed := false
		existsInUntested := false
		for _, c := range g.PassedConcepts {
			if c.ID == id {
				existsInPassed = true
				break
			}
		}
		for _, c := range g.UntestedConcepts {
			if c.ID == id {
				existsInUntested = true
				break
			}
		}

		if existsInPassed {
			refusal := &ops.Refusal{
				Err:  id + " already exists",
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\"", id),
				Exit: 1,
			}
			return nil, nil, refusal
		}
		if existsInUntested {
			return nil, nil, &ops.Refusal{
				Err:  id + " already exists",
				Exit: 1,
			}
		}

		// Validate that named parents/children exist as concepts.
		allConcepts := make(map[string]bool)
		for _, c := range g.PassedConcepts {
			allConcepts[c.ID] = true
		}
		for _, c := range g.UntestedConcepts {
			allConcepts[c.ID] = true
		}

		for _, p := range parents {
			if !allConcepts[p.endpointID] {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("unknown ID %q", p.endpointID),
					Fix:  usageLine,
					Exit: 3,
				}
			}
		}
		for _, ch := range childPairs {
			if !allConcepts[ch.endpointID] {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("unknown ID %q", ch.endpointID),
					Fix:  usageLine,
					Exit: 3,
				}
			}
		}

		// Build the new graph with the new node and edges added tentatively.
		newG := *g // shallow copy
		newNode := &graph.ConceptNode{
			ID:    id,
			Scope: scope,
			Cites: []string{citeStr},
			Block: graph.BlockUntested,
		}
		// Copy concept slices so we don't mutate the originals.
		newG.UntestedConcepts = append(append([]*graph.ConceptNode{}, g.UntestedConcepts...), newNode)

		// Copy edges slice and append new edges.
		newEdges := append([]*graph.Edge{}, g.Edges...)
		for _, p := range parents {
			newEdges = append(newEdges, &graph.Edge{From: p.endpointID, To: id, Label: p.rel})
		}
		for _, ch := range childPairs {
			newEdges = append(newEdges, &graph.Edge{From: id, To: ch.endpointID, Label: ch.rel})
		}
		newG.Edges = newEdges

		// Cycle check on the tentative graph.
		if graph.HasConceptCycle(&newG) {
			return nil, nil, &ops.Refusal{
				Err:  "edge would close a cycle",
				Exit: 1,
			}
		}

		// Collect parent/child IDs for the event log (§10).
		parentIDs := make([]string, len(parents))
		for i, p := range parents {
			parentIDs[i] = p.endpointID
		}
		childIDs := make([]string, len(childPairs))
		for i, ch := range childPairs {
			childIDs[i] = ch.endpointID
		}

		rowFields := map[string]any{
			"id":       id,
			"scope":    scope,
			"src":      citeStr,
			"parents":  parentIDs,
			"children": childIDs,
		}
		source.ApplyMeta(rowFields, citeMeta)
		row := eventlog.NewRow("add", rowFields)

		// Gate clearing for gated children (Q2 / spec §7 line 278).
		// When --child C is gated, write the gate meta + gate event for C via=add.
		rows := []eventlog.Row{row}
		currentG := &newG
		for _, ch := range childPairs {
			if clearedG, gateRow, ok := ops.ClearGate(currentG, s, ch.endpointID, "add", ""); ok {
				currentG = clearedG
				rows = append(rows, gateRow)
			}
		}
		return currentG, rows, nil
	}

	// ctx.GraphFile already set above; runMutation will re-resolve, but we've
	// already validated. Use a slim variant that skips the second ResolveFile.
	return runMutationWithFile(ctx, file, apply)
}

// endpointPair holds a parsed <id>:"<rel>" value.
type endpointPair struct {
	endpointID string
	rel        string
}

// parseEndpointFlags parses repeated --parent and --child flag values of the
// form <id>:<rel> (the shell strips outer quotes, so the value is id:rel text).
// Returns two slices and a non-empty error message if any value is malformed.
func parseEndpointFlags(parentVals, childVals []string) (parents, children []endpointPair, errMsg string) {
	for _, v := range parentVals {
		p, err := parseEndpointValue(v)
		if err != "" {
			return nil, nil, err
		}
		parents = append(parents, p)
	}
	for _, v := range childVals {
		p, err := parseEndpointValue(v)
		if err != "" {
			return nil, nil, err
		}
		children = append(children, p)
	}
	return parents, children, ""
}

// parseEndpointValue parses a single <id>:<rel> value, splitting on the first
// colon. Returns a non-empty error string when no colon is present.
func parseEndpointValue(v string) (endpointPair, string) {
	idx := strings.Index(v, ":")
	if idx < 0 {
		return endpointPair{}, fmt.Sprintf("flag value %q must be <id>:<rel>", v)
	}
	return endpointPair{endpointID: v[:idx], rel: v[idx+1:]}, ""
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
