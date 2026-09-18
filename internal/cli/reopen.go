package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

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

	usageLine := FindCommand("reopen").Usage()

	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Build node membership sets.
		allConcepts := make(map[string]bool)
		passedSet := make(map[string]bool)
		for _, c := range g.PassedConcepts {
			allConcepts[c.ID] = true
			passedSet[c.ID] = true
		}
		for _, c := range g.UntestedConcepts {
			allConcepts[c.ID] = true
		}

		allNodes := make(map[string]bool)
		for id := range allConcepts {
			allNodes[id] = true
		}
		for _, item := range g.TestingItems {
			if item.Q != nil {
				allNodes[item.Q.ID] = true
			}
			if item.A != nil {
				allNodes[item.A.ID] = true
			}
		}

		// Unknown ID → exit 3.
		if !allNodes[concept] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Not a concept (q or a node) → exit 1.
		if !allConcepts[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not a concept",
				Exit: 1,
			}
		}

		// Not in passed → exit 1.
		if !passedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not passed",
				Exit: 1,
			}
		}

		// Find the target node in passed.
		var targetNode *graph.ConceptNode
		for _, c := range g.PassedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		// Build new node (copy-on-write): move to untested, set GAP.
		newNode := *targetNode
		newNode.Block = graph.BlockUntested
		newNode.GAP = gap

		// Build new graph: remove from passed, prepend to untested.
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

		row := eventlog.NewRow("reopen", map[string]any{
			"concept": concept,
			"gap":     gap,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
