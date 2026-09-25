package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// aidRun is the Run handler for:
//
//	tm aid <id> <path>
//	tm aid rm <id> <path>
//
// Links or unlinks an aid path to/from a concept or question.
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal
//	3  usage error, unknown ID
func aidRun(ctx *Context) int {
	switch ctx.Positionals[0] {
	case "rm":
		return aidRM(ctx)
	default:
		return aidAdd(ctx)
	}
}

// aidAdd handles `tm aid <id> <path>`.
func aidAdd(ctx *Context) int {
	if len(ctx.Positionals) < 2 {
		ctx.ErrMsg = "aid requires <id> and <path>"
		ctx.FixMsg = FindCommand("aid").Usage()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
	return aidMutate(ctx, ctx.Positionals[0], ctx.Positionals[1], "add")
}

// aidRM handles `tm aid rm <id> <path>`.
func aidRM(ctx *Context) int {
	if len(ctx.Positionals) < 3 {
		ctx.ErrMsg = "aid rm requires <id> and <path>"
		ctx.FixMsg = FindCommand("aid").Usage()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}
	return aidMutate(ctx, ctx.Positionals[1], ctx.Positionals[2], "rm")
}

// aidMutate applies an aid add or rm operation via ops.Mutate.
func aidMutate(ctx *Context, id, path, action string) int {
	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		_ = s
		allConcepts := make(map[string]bool)
		for _, c := range g.PassedConcepts {
			allConcepts[c.ID] = true
		}
		for _, c := range g.UntestedConcepts {
			allConcepts[c.ID] = true
		}
		for _, c := range g.ReserveConcepts {
			allConcepts[c.ID] = true
		}
		testingQIDs := make(map[string]bool)
		for _, item := range g.TestingItems {
			if item.Q != nil {
				testingQIDs[item.Q.ID] = true
			}
		}
		if !allConcepts[id] && !testingQIDs[id] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown id %s", id),
				Exit: 3,
			}
		}
		if action == "add" {
			return aidApplyAdd(g, id, path, allConcepts)
		}
		return aidApplyRM(g, id, path, allConcepts)
	}
	return runMutation(ctx, apply)
}

// aidApplyAdd adds an aid path to a concept or question node.
func aidApplyAdd(g *graph.Graph, id, path string, allConcepts map[string]bool) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
	if allConcepts[id] {
		cn := findConceptNode(g, id)
		if cn != nil {
			for _, p := range cn.Aids {
				if p == path {
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("%s already links %s", id, path),
						Exit: 1,
					}
				}
			}
			newCN := *cn
			newCN.Aids = append(append([]string(nil), cn.Aids...), path)
			newG := replaceConceptInGraph(g, &newCN)
			row := eventlog.NewRow("aid", map[string]any{"id": id, "path": path, "action": "add"})
			return newG, []eventlog.Row{row}, nil
		}
	}
	for i, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == id {
			for _, p := range item.Q.Aids {
				if p == path {
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("%s already links %s", id, path),
						Exit: 1,
					}
				}
			}
			newQ := *item.Q
			newQ.Aids = append(append([]string(nil), item.Q.Aids...), path)
			newG := *g
			items := make([]graph.TestingItem, len(g.TestingItems))
			copy(items, g.TestingItems)
			items[i] = graph.TestingItem{Q: &newQ}
			newG.TestingItems = items
			row := eventlog.NewRow("aid", map[string]any{"id": id, "path": path, "action": "add"})
			return &newG, []eventlog.Row{row}, nil
		}
	}
	return nil, nil, &ops.Refusal{Err: fmt.Sprintf("unknown id %s", id), Exit: 3}
}

// aidApplyRM removes an aid path from a concept or question node.
func aidApplyRM(g *graph.Graph, id, path string, allConcepts map[string]bool) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
	if allConcepts[id] {
		cn := findConceptNode(g, id)
		if cn != nil {
			for i, p := range cn.Aids {
				if p == path {
					newCN := *cn
					newCN.Aids = append(append([]string(nil), cn.Aids[:i]...), cn.Aids[i+1:]...)
					newG := replaceConceptInGraph(g, &newCN)
					row := eventlog.NewRow("aid", map[string]any{"id": id, "path": path, "action": "rm"})
					return newG, []eventlog.Row{row}, nil
				}
			}
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s does not link %s", id, path),
				Exit: 1,
			}
		}
	}
	for i, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == id {
			for j, p := range item.Q.Aids {
				if p == path {
					newQ := *item.Q
					newQ.Aids = append(append([]string(nil), item.Q.Aids[:j]...), item.Q.Aids[j+1:]...)
					newG := *g
					items := make([]graph.TestingItem, len(g.TestingItems))
					copy(items, g.TestingItems)
					items[i] = graph.TestingItem{Q: &newQ}
					newG.TestingItems = items
					row := eventlog.NewRow("aid", map[string]any{"id": id, "path": path, "action": "rm"})
					return &newG, []eventlog.Row{row}, nil
				}
			}
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s does not link %s", id, path),
				Exit: 1,
			}
		}
	}
	return nil, nil, &ops.Refusal{Err: fmt.Sprintf("unknown id %s", id), Exit: 3}
}

// findConceptNode returns a pointer to the concept node with the given id,
// searching passed, untested, and reserve blocks. Returns nil if not found.
func findConceptNode(g *graph.Graph, id string) *graph.ConceptNode {
	for _, c := range g.PassedConcepts {
		if c.ID == id {
			return c
		}
	}
	for _, c := range g.UntestedConcepts {
		if c.ID == id {
			return c
		}
	}
	for _, c := range g.ReserveConcepts {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// replaceConceptInGraph returns a copy of g with the concept node matching
// newCN.ID replaced by newCN, wherever it appears.
func replaceConceptInGraph(g *graph.Graph, newCN *graph.ConceptNode) *graph.Graph {
	newG := *g
	if replaced, sl := replaceConceptInSlice(g.PassedConcepts, newCN); replaced {
		newG.PassedConcepts = sl
		return &newG
	}
	if replaced, sl := replaceConceptInSlice(g.UntestedConcepts, newCN); replaced {
		newG.UntestedConcepts = sl
		return &newG
	}
	if replaced, sl := replaceConceptInSlice(g.ReserveConcepts, newCN); replaced {
		newG.ReserveConcepts = sl
		return &newG
	}
	return &newG
}

func replaceConceptInSlice(sl []*graph.ConceptNode, newCN *graph.ConceptNode) (bool, []*graph.ConceptNode) {
	for i, c := range sl {
		if c.ID == newCN.ID {
			out := make([]*graph.ConceptNode, len(sl))
			copy(out, sl)
			out[i] = newCN
			return true, out
		}
	}
	return false, nil
}
