package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// unlinkRun is the Run handler for `tm unlink <from> <to>`.
//
// Removes a concept→concept prerequisite edge. Reserve endpoints are accepted
// (consistent with link after #89). Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (non-concept endpoint, edge not found)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func unlinkRun(ctx *Context) int {
	from := ctx.Positionals[0]
	to := ctx.Positionals[1]

	usageLine := FindCommand("unlink").Usage()

	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g, true)

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

		// Find and remove the edge.
		idx := -1
		for i, e := range g.Edges {
			if e.From == from && e.To == to {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("no edge from %s to %s", from, to),
				Exit: 1,
			}
		}

		newG := *g
		newEdges := make([]*graph.Edge, 0, len(g.Edges)-1)
		for i, e := range g.Edges {
			if i != idx {
				newEdges = append(newEdges, e)
			}
		}
		newG.Edges = newEdges

		row := eventlog.NewRow("unlink", map[string]any{
			"from": from,
			"to":   to,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
