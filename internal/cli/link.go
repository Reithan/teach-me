package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// linkRun is the Run handler for `tm link <from> <to> "<rel>"`.
//
// Adds an edge between two existing concepts.
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (non-concept endpoint, cycle, duplicate edge)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func linkRun(ctx *Context) int {
	from := ctx.Positionals[0]
	to := ctx.Positionals[1]
	rel := ctx.Positionals[2]

	usageLine := FindCommand("link").Usage()

	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g)

		// Unknown ID → exit 3.
		if !ns.AllNodes[from] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", from),
				Fix:  usageLine,
				Exit: 3,
			}
		}
		if !ns.AllNodes[to] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", to),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Non-concept endpoint → exit 1.
		if !ns.AllConcepts[from] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s is not a concept", from),
				Exit: 1,
			}
		}
		if !ns.AllConcepts[to] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s is not a concept", to),
				Exit: 1,
			}
		}

		// Refuse duplicate identical edge (friendly exit 1).
		for _, e := range g.Edges {
			if e.From == from && e.To == to && e.Label == rel {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("edge %s --%q--> %s already exists", from, rel, to),
					Exit: 1,
				}
			}
		}

		// Build tentative graph with the new edge to check for cycles.
		newG := *g
		newEdges := append([]*graph.Edge{}, g.Edges...)
		newEdges = append(newEdges, &graph.Edge{From: from, To: to, Label: rel})
		newG.Edges = newEdges

		if graph.HasConceptCycle(&newG) {
			return nil, nil, &ops.Refusal{
				Err:  "edge would close a cycle",
				Exit: 1,
			}
		}

		row := eventlog.NewRow("link", map[string]any{
			"from": from,
			"to":   to,
			"rel":  rel,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
