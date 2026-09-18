package ops

import (
	"fmt"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// GCNode is one node entry in the gc event (§10 nodes field).
// Fields match the §10 schema: id, label, class.
type GCNode struct {
	ID    string
	Label string
	Class string
}

// RemovedSubtree holds the data collected when RemoveTestingSubtree strips a
// concept's testing nodes, edges, and gate meta lines from the graph. Its
// fields feed the gc event (§10) and the pass event's batches field.
type RemovedSubtree struct {
	// Concept is the concept ID whose subtree was removed.
	Concept string

	// Batches is the list of batch class IDs cleared, in ascending N order.
	// Feeds the pass event's batches field (§10).
	Batches []string

	// Nodes is the question and answer nodes that were removed, in declaration
	// order. Feeds the gc event's nodes field (§10).
	Nodes []GCNode

	// Edges is the edges incident to removed testing nodes. Feeds the gc
	// event's edges field (§10).
	Edges []*graph.Edge

	// Meta is the gate meta lines removed from the untested block. Feeds the
	// gc event's meta field (§10).
	Meta []graph.GateMeta
}

// RemoveTestingSubtree removes conceptID's entire testing subtree from a copy
// of g: all questions, their answers, edges incident to those nodes, and gate
// meta lines for the concept. The returned RemovedSubtree holds the collected
// data for the gc and pass events (§10). The input graph is never mutated.
//
// "Under the concept" means every question reachable via conceptID's batch
// classes and their corresponding answers. Edges are any edge whose From or To
// endpoint is one of the removed question or answer IDs.
func RemoveTestingSubtree(g *graph.Graph, s *state.State, conceptID string) (RemovedSubtree, *graph.Graph) {
	batches := s.ConceptBatches(conceptID)

	rst := RemovedSubtree{
		Concept: conceptID,
		Batches: append([]string(nil), batches...),
	}

	// Build a set of node IDs to remove: all questions in every batch, and
	// their corresponding answers (aN shares the same N as qN).
	removeIDs := make(map[string]bool)
	for _, batchClass := range batches {
		for _, q := range s.BatchQuestions(batchClass) {
			removeIDs[q.ID] = true
			n := graph.QuestionN(q.ID)
			if n > 0 {
				removeIDs[fmt.Sprintf("a%d", n)] = true
			}
		}
	}

	// Collect Nodes in graph declaration order (questions and answers).
	for _, item := range g.TestingItems {
		if item.Q != nil && removeIDs[item.Q.ID] {
			rst.Nodes = append(rst.Nodes, GCNode{
				ID:    item.Q.ID,
				Label: item.Q.Scope,
				Class: item.Q.Class,
			})
		}
		if item.A != nil && removeIDs[item.A.ID] {
			rst.Nodes = append(rst.Nodes, GCNode{
				ID:    item.A.ID,
				Label: item.A.Label,
				Class: item.A.Class,
			})
		}
	}

	// Collect edges incident to any removed node.
	shouldRemoveEdge := func(e *graph.Edge) bool {
		return removeIDs[e.From] || removeIDs[e.To]
	}
	for _, e := range g.Edges {
		if shouldRemoveEdge(e) {
			rst.Edges = append(rst.Edges, e)
		}
	}

	// Collect gate meta lines for this concept.
	for _, m := range g.UntestedMetas {
		if m.Concept == conceptID {
			rst.Meta = append(rst.Meta, m)
		}
	}

	// Build the modified graph: copy-on-write for the three affected slices.
	newG := shallowCopyGraph(g)

	var newItems []graph.TestingItem
	for _, item := range g.TestingItems {
		if item.Q != nil && removeIDs[item.Q.ID] {
			continue
		}
		if item.A != nil && removeIDs[item.A.ID] {
			continue
		}
		newItems = append(newItems, item)
	}
	newG.TestingItems = newItems

	var newEdges []*graph.Edge
	for _, e := range g.Edges {
		if !shouldRemoveEdge(e) {
			newEdges = append(newEdges, e)
		}
	}
	newG.Edges = newEdges

	var newMetas []graph.GateMeta
	for _, m := range g.UntestedMetas {
		if m.Concept != conceptID {
			newMetas = append(newMetas, m)
		}
	}
	newG.UntestedMetas = newMetas

	return rst, newG
}

// MoveToPassed moves conceptID's declaration from the untested block to the
// top of the passed block and clears its GAP field. The input graph is never
// mutated; a modified copy is returned.
//
// Edge relocation is automatic: the writer's edgeHomeBlock rule (§4.2)
// reassigns edges based on the node's new block membership, so callers do not
// need to relocate edges explicitly. Use a round-trip test (Write then Parse)
// to verify placement.
func MoveToPassed(g *graph.Graph, conceptID string) *graph.Graph {
	newG := shallowCopyGraph(g)

	// Find and extract the concept from untested.
	var concept *graph.ConceptNode
	remaining := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts))
	for _, c := range g.UntestedConcepts {
		if c.ID == conceptID {
			concept = c
		} else {
			remaining = append(remaining, c)
		}
	}
	newG.UntestedConcepts = remaining

	if concept == nil {
		// Not found: return the shallow copy unchanged (caller mismatch).
		return newG
	}

	// Build the passed node with GAP cleared.
	passed := &graph.ConceptNode{
		ID:              concept.ID,
		Scope:           concept.Scope,
		GAP:             "", // strip GAP per §8.4
		Cites:           append([]string(nil), concept.Cites...),
		Block:           graph.BlockPassed,
		LeadingComments: append([]string(nil), concept.LeadingComments...),
	}

	// Prepend to passed (newest at the top per §4.1).
	newPassed := make([]*graph.ConceptNode, 0, len(g.PassedConcepts)+1)
	newPassed = append(newPassed, passed)
	newPassed = append(newPassed, g.PassedConcepts...)
	newG.PassedConcepts = newPassed

	return newG
}

// shallowCopyGraph returns a shallow copy of g. The caller must replace any
// slice field it intends to modify so it does not alias the original.
func shallowCopyGraph(g *graph.Graph) *graph.Graph {
	cp := *g
	return &cp
}
