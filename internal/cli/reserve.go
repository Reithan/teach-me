package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// reserveRun is the Run handler for `tm reserve <concept>`.
//
// Moves an untested concept with no questions to the reserve block. Its edges
// stay; its children stop being blocked by it. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (not untested, has questions)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func reserveRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	usageLine := FindCommand("reserve").Usage()

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

		// Not in untested → exit 1.
		if !ns.UntestedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not in untested",
				Exit: 1,
			}
		}

		// Has questions → exit 1.
		for _, item := range g.TestingItems {
			if item.Q == nil {
				continue
			}
			cid, ok := s.ConceptOf(item.Q.ID)
			if ok && cid == concept {
				return nil, nil, &ops.Refusal{
					Err:  concept + " has questions",
					Exit: 1,
				}
			}
		}

		var targetNode *graph.ConceptNode
		for _, c := range g.UntestedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		newNode := *targetNode
		newNode.Block = graph.BlockReserve

		newG := *g

		newUntested := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)-1)
		for _, c := range g.UntestedConcepts {
			if c.ID != concept {
				newUntested = append(newUntested, c)
			}
		}
		newG.UntestedConcepts = newUntested

		newReserve := make([]*graph.ConceptNode, len(g.ReserveConcepts)+1)
		copy(newReserve, g.ReserveConcepts)
		newReserve[len(g.ReserveConcepts)] = &newNode
		newG.ReserveConcepts = newReserve

		row := eventlog.NewRow("reserve", map[string]any{
			"concept": concept,
		})
		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
