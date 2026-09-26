package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// dropRun is the Run handler for `tm drop <id>`, where <id> is a concept or
// the qid of a drifted ungraded question.
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
		ns := graphNodeSets(g, false)

		// Unknown ID → exit 3.
		if !ns.AllNodes[concept] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Question ID: handle drift-drop path (§6, §7 drop-question row, §8 accounting).
		if !ns.AllConcepts[concept] {
			return dropQuestionDrift(g, s, concept, usageLine)
		}

		// Passed concept → exit 1.
		if ns.PassedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is passed",
				Exit: 1,
			}
		}

		// Has children (dependents: outgoing concept→concept edges) → exit 1.
		for _, e := range g.Edges {
			if e.From == concept && ns.AllConcepts[e.To] {
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

// dropQuestionDrift implements the question drift-drop path:
//
//	tm drop <qid>
//
// Accepted only when:
//   - qid is an ungraded question (no answer, or pending answer)
//   - the question's citation has drifted (cite.CheckDrift true)
//
// Refuses with:
//   - "question is already graded" (exit 1) if a non-pending answer exists
//   - "citation has not drifted" (exit 1) if no drift detected
//
// The question remains in the graph but gains an "unclear" answer node so
// that `tm q --re <qid>` works via the existing ProbeReplacesUnclear path.
// Batch accounting treats the drop as an unclear verdict with no verdict
// consequences (no fail-count increment, §8.8).
func dropQuestionDrift(
	g *graph.Graph,
	s *state.State,
	qid string,
	usageLine string,
) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
	// Find the question node.
	var qn *graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == qid {
			qn = item.Q
			break
		}
	}
	if qn == nil {
		// ID exists but is not a question (e.g. an answer node). Refuse.
		return nil, nil, &ops.Refusal{
			Err:  qid + " is not a concept or question",
			Fix:  usageLine,
			Exit: 1,
		}
	}

	// Refuse if the question already has a graded answer (§7 drop-question row).
	an := s.AnswerFor(qid)
	if an != nil && an.Class != "pending" {
		return nil, nil, &ops.Refusal{
			Err:  qid + " is already graded",
			Exit: 1,
		}
	}

	// Refuse if the citation has not drifted (§7 drop-question row).
	srcRoot := s.Cfg().SrcRoot
	drifted, driftErr := checkCiteDrift(qn.Cite, srcRoot)
	if driftErr != nil {
		// Unresolvable citation: keep existing behavior, do not refuse as drift.
		return nil, nil, &ops.Refusal{
			Err:  fmt.Sprintf("%s citation is unresolvable: %v", qid, driftErr),
			Exit: 1,
		}
	}
	if !drifted {
		return nil, nil, &ops.Refusal{
			Err:  qid + " citation has not drifted",
			Fix:  "tm drop is for drifted questions; use tm grade for graded ones",
			Exit: 1,
		}
	}

	// Add or replace the "unclear" answer tombstone so the existing --re path
	// works. The answer ID mirrors the question number (a<N> for q<N>).
	qN := graph.QuestionN(qid)
	aid := fmt.Sprintf("a%d", qN)

	tombstone := &graph.AnswerNode{
		ID:    aid,
		Class: "unclear",
		Label: graph.DroppedLabel,
	}

	// Capture the pending answer text before we overwrite it (§10 event field).
	var pendingAnswerText string
	if an != nil && an.Class == "pending" {
		pendingAnswerText = an.Label
	}

	newG := *g
	newItems := make([]graph.TestingItem, len(g.TestingItems))
	copy(newItems, g.TestingItems)

	replaced := false
	for i, item := range newItems {
		if item.A != nil && item.A.ID == aid {
			// Replace the existing pending answer in place.
			newItems[i].A = tombstone
			replaced = true
			break
		}
	}
	if !replaced {
		newItems = append(newItems, graph.TestingItem{A: tombstone})
	}
	newG.TestingItems = newItems

	// Add the q→a edge only when there was no existing answer (which already
	// implies the edge exists).
	newEdges := append([]*graph.Edge{}, g.Edges...)
	if !replaced {
		newEdges = append(newEdges, &graph.Edge{From: qid, To: aid})
	}
	newG.Edges = newEdges

	// Build event.
	nodeObj := map[string]any{
		"scope": qn.Scope,
		"src":   qn.Cite,
		"class": qn.Class,
	}

	// Collect edges touching the question for the event edges field.
	var edgeList []map[string]any
	for _, e := range g.Edges {
		if e.From == qid || e.To == qid {
			edgeList = append(edgeList, map[string]any{
				"from": e.From,
				"to":   e.To,
				"rel":  e.Label,
			})
		}
	}
	if edgeList == nil {
		edgeList = []map[string]any{}
	}

	evFields := map[string]any{
		"id":     qid,
		"node":   nodeObj,
		"edges":  edgeList,
		"reason": "drift",
	}
	if pendingAnswerText != "" {
		evFields["answer"] = pendingAnswerText
	}
	if len(qn.Aids) > 0 {
		evFields["aids"] = qn.Aids
	}

	row := eventlog.NewRow("drop", evFields)

	return &newG, []eventlog.Row{row}, nil
}
