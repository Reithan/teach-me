package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// reserveRun is the Run handler for `tm reserve <concept> [--reason "<text>"]`.
//
// Moves an untested concept to the reserve block. If the concept has questions,
// --reason is required and all of the concept's batches must be resolved
// (every question has a non-pending answer). Question-less concepts still work
// without --reason (the pruner relies on this).
// Edges stay; children stop being blocked. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (not untested; questions without --reason; open batch)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func reserveRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	reason := ""
	if v := ctx.Flags["reason"]; len(v) > 0 {
		reason = v[0]
	}
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

		// Check whether the concept has any questions.
		hasQuestions := false
		for _, item := range g.TestingItems {
			if item.Q == nil {
				continue
			}
			cid, ok := s.ConceptOf(item.Q.ID)
			if ok && cid == concept {
				hasQuestions = true
				break
			}
		}

		if hasQuestions {
			// --reason is required when the concept has questions.
			if reason == "" {
				return nil, nil, &ops.Refusal{
					Err:  concept + " has questions",
					Fix:  usageLine,
					Exit: 1,
				}
			}

			// All batches must be resolved (no unanswered or ungraded questions).
			for _, batchClass := range s.ConceptBatches(concept) {
				bs := s.BatchStateOf(batchClass)
				if bs != state.BatchResolved {
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("%s has an unresolved batch (%s)", concept, batchClass),
						Exit: 1,
					}
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

		fields := map[string]any{
			"concept": concept,
		}
		if reason != "" {
			fields["reason"] = reason
		}
		row := eventlog.NewRow("reserve", fields)
		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
