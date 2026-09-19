package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// gapRun is the Run handler for `tm gap <concept> "<gap>"`.
//
// Sets or replaces the GAP field of an untested concept. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (passed, wrong type)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func gapRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	gap := ctx.Positionals[1]

	usageLine := FindCommand("gap").Usage()

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

		// Passed concept → exit 1 (a passed concept has no GAP per §11).
		if passedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is passed",
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\"", concept),
				Exit: 1,
			}
		}

		// Find the target node in untested.
		var targetNode *graph.ConceptNode
		for _, c := range g.UntestedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		oldGAP := targetNode.GAP

		// Build new node (copy-on-write).
		newNode := *targetNode
		newNode.GAP = gap

		// Build new graph.
		newG := *g
		newConcepts := make([]*graph.ConceptNode, len(g.UntestedConcepts))
		for i, c := range g.UntestedConcepts {
			if c.ID == concept {
				newConcepts[i] = &newNode
			} else {
				newConcepts[i] = c
			}
		}
		newG.UntestedConcepts = newConcepts

		row := eventlog.NewRow("gap", map[string]any{
			"concept": concept,
			"before":  oldGAP,
			"after":   gap,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
