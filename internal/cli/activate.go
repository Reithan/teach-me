package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// activateRun is the Run handler for `tm activate <concept>`.
//
// Moves a reserve concept to untested. Its own reserve parents stay parked.
// For every child of the concept that currently has a gate line, clears the
// gate with via=activate. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (not in reserve)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func activateRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	usageLine := FindCommand("activate").Usage()

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g, true)

		// Unknown ID → exit 3.
		if !ns.AllNodes[concept] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Not in reserve → exit 1.
		if !ns.ReserveSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not in reserve",
				Exit: 1,
			}
		}

		var targetNode *graph.ConceptNode
		for _, c := range g.ReserveConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		newNode := *targetNode
		newNode.Block = graph.BlockUntested

		newG := *g

		newReserve := make([]*graph.ConceptNode, 0, len(g.ReserveConcepts)-1)
		for _, c := range g.ReserveConcepts {
			if c.ID != concept {
				newReserve = append(newReserve, c)
			}
		}
		newG.ReserveConcepts = newReserve

		// Prepend activated node to untested (highest priority).
		newUntested := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)+1)
		newUntested = append(newUntested, &newNode)
		newUntested = append(newUntested, g.UntestedConcepts...)
		newG.UntestedConcepts = newUntested

		// Clear gates on direct concept-children of this concept.
		// unblocked collects IDs in edge declaration order (file order).
		var unblocked []string
		rows := make([]eventlog.Row, 0)
		currentG := &newG
		for _, e := range g.Edges {
			if e.From != concept || !ns.AllConcepts[e.To] {
				continue
			}
			childID := e.To
			if clearedG, gateRow, ok := ops.ClearGate(currentG, s, childID, "activate", ""); ok {
				currentG = clearedG
				rows = append(rows, gateRow)
				unblocked = append(unblocked, childID)
			}
		}
		if unblocked == nil {
			unblocked = []string{}
		}

		activateRow := eventlog.NewRow("activate", map[string]any{
			"concept":   concept,
			"unblocked": unblocked,
		})
		// Activate event first, then gate events.
		allRows := make([]eventlog.Row, 0, len(rows)+1)
		allRows = append(allRows, activateRow)
		allRows = append(allRows, rows...)

		return currentG, allRows, nil
	}

	return runMutation(ctx, apply)
}
