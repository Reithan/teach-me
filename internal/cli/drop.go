package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// dropRun is the Run handler for `tm drop <concept>`.
//
// Removes an untested leaf concept that has no questions. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (passed, wrong type, has children, has questions)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func dropRun(ctx *Context) int {
	concept := ctx.Positionals[0]

	usageLine := FindCommand("drop").Usage()

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
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

		// Passed concept → exit 1.
		if passedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is passed",
				Exit: 1,
			}
		}

		// Has children (dependents: outgoing concept→concept edges) → exit 1.
		for _, e := range g.Edges {
			if e.From == concept && allConcepts[e.To] {
				return nil, nil, &ops.Refusal{
					Err:  concept + " has children",
					Exit: 1,
				}
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

		// Find the target node.
		var targetNode *graph.ConceptNode
		for _, c := range g.UntestedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		// Partition edges: those touching concept are removed, rest are kept.
		keptEdges := make([]*graph.Edge, 0, len(g.Edges))
		var removedEdges []*graph.Edge
		for _, e := range g.Edges {
			if e.From == concept || e.To == concept {
				removedEdges = append(removedEdges, e)
			} else {
				keptEdges = append(keptEdges, e)
			}
		}

		// Build new graph (copy-on-write).
		newG := *g
		newConcepts := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)-1)
		for _, c := range g.UntestedConcepts {
			if c.ID != concept {
				newConcepts = append(newConcepts, c)
			}
		}
		newG.UntestedConcepts = newConcepts
		newG.Edges = keptEdges

		// Build event node object.
		nodeObj := map[string]any{
			"scope": targetNode.Scope,
			"src":   targetNode.Cites,
		}
		if targetNode.GAP != "" {
			nodeObj["gap"] = targetNode.GAP
		}

		// Build event edges list.
		edgeList := make([]map[string]any, len(removedEdges))
		for i, e := range removedEdges {
			edgeList[i] = map[string]any{
				"from": e.From,
				"to":   e.To,
				"rel":  e.Label,
			}
		}

		row := eventlog.NewRow("drop", map[string]any{
			"id":    concept,
			"node":  nodeObj,
			"edges": edgeList,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
