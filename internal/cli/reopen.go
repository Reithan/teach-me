package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// reopenApply is the shared reopen logic used by both reopenRun and
// gradeDriftRun. It moves concept from passed to untested, optionally updates
// the citation with srcCite, emits a "reopen" event, and clears gates on
// gated direct children.
func reopenApply(
	g *graph.Graph,
	s *state.State,
	concept, gap, srcCite, usageLine string,
) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
	ns := graphNodeSets(g, false)

	// Unknown ID → exit 3.
	if !ns.AllNodes[concept] {
		return nil, nil, &ops.Refusal{
			Err:  fmt.Sprintf("unknown ID %q", concept),
			Fix:  usageLine,
			Exit: 3,
		}
	}

	// Not a concept (q or a node) → exit 1.
	if !ns.AllConcepts[concept] {
		return nil, nil, &ops.Refusal{
			Err:  concept + " is not a concept",
			Exit: 1,
		}
	}

	// Not in passed → exit 1.
	if !ns.PassedSet[concept] {
		return nil, nil, &ops.Refusal{
			Err:  concept + " is not passed",
			Exit: 1,
		}
	}

	var targetNode *graph.ConceptNode
	for _, c := range g.PassedConcepts {
		if c.ID == concept {
			targetNode = c
			break
		}
	}

	newNode := *targetNode
	newNode.Block = graph.BlockUntested
	newNode.GAP = gap

	// --src: re-point the concept citation in the same operation.
	srcBefore := ""
	srcAfter := ""
	if srcCite != "" {
		srcRoot := s.Cfg().SrcRoot
		hashedSrc, hashErr := hashCiteText(srcCite, srcRoot)
		if hashErr != nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot hash --src %q: %v", srcCite, hashErr),
				Fix:  usageLine,
				Exit: 3,
			}
		}
		// Replace the first citation (or the only one).
		if len(newNode.Cites) > 0 {
			srcBefore = newNode.Cites[0]
			newCites := make([]string, len(newNode.Cites))
			copy(newCites, newNode.Cites)
			newCites[0] = hashedSrc
			newNode.Cites = newCites
		} else {
			newNode.Cites = []string{hashedSrc}
		}
		srcAfter = hashedSrc
	}

	newG := *g

	// New passed list without the reopened concept.
	newPassed := make([]*graph.ConceptNode, 0, len(g.PassedConcepts)-1)
	for _, c := range g.PassedConcepts {
		if c.ID != concept {
			newPassed = append(newPassed, c)
		}
	}
	newG.PassedConcepts = newPassed

	// Prepend reopened node to untested (top = highest priority open question).
	newUntested := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)+1)
	newUntested = append(newUntested, &newNode)
	newUntested = append(newUntested, g.UntestedConcepts...)
	newG.UntestedConcepts = newUntested

	eventFields := map[string]any{
		"concept": concept,
		"gap":     gap,
	}
	if srcBefore != "" {
		eventFields["src_before"] = srcBefore
	}
	if srcAfter != "" {
		eventFields["src_after"] = srcAfter
	}
	row := eventlog.NewRow("reopen", eventFields)

	// Gate clearing for gated direct children of the reopened concept (Q2 /
	// spec §7 line 278). Reopening P re-blocks its direct children via the
	// frontier rule; if any direct child C is currently gated, write the gate
	// line + gate event for C via=reopen.
	rows := []eventlog.Row{row}
	currentG := &newG
	for _, e := range g.Edges {
		if e.From != concept {
			continue
		}
		childID := e.To
		if !ns.AllConcepts[childID] {
			continue
		}
		if clearedG, gateRow, ok := ops.ClearGate(currentG, s, childID, "reopen", ""); ok {
			currentG = clearedG
			rows = append(rows, gateRow)
		}
	}
	return currentG, rows, nil
}

// reopenRun is the Run handler for `tm reopen <concept> "<gap>"`.
//
// Moves a passed concept back to untested (top of the untested list) with a
// GAP field set. Descendants that depend on it stay passed. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (not a concept, not passed)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func reopenRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	gap := ctx.Positionals[1]

	srcCite := ""
	if v := ctx.Flags["src"]; len(v) > 0 {
		srcCite = v[0]
	}

	usageLine := FindCommand("reopen").Usage()

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		return reopenApply(g, s, concept, gap, srcCite, usageLine)
	}

	return runMutation(ctx, apply)
}
