package graph

import "testing"

// helper builds a Graph with the given concept IDs (all in untested) and edges.
func buildConceptGraph(ids []string, edges [][2]string) *Graph {
	g := &Graph{}
	for _, id := range ids {
		g.UntestedConcepts = append(g.UntestedConcepts, &ConceptNode{ID: id, Block: BlockUntested})
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, &Edge{From: e[0], To: e[1], Label: "rel"})
	}
	return g
}

func TestHasConceptCycle_NoCycle(t *testing.T) {
	// a → b → c: no cycle
	g := buildConceptGraph([]string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"b", "c"}})
	if HasConceptCycle(g) {
		t.Error("want no cycle for linear chain a→b→c")
	}
}

func TestHasConceptCycle_DirectSelfLoop(t *testing.T) {
	// a → a
	g := buildConceptGraph([]string{"a"}, [][2]string{{"a", "a"}})
	if !HasConceptCycle(g) {
		t.Error("want cycle for self-loop a→a")
	}
}

func TestHasConceptCycle_TwoNodeCycle(t *testing.T) {
	// a → b → a
	g := buildConceptGraph([]string{"a", "b"}, [][2]string{{"a", "b"}, {"b", "a"}})
	if !HasConceptCycle(g) {
		t.Error("want cycle for a→b→a")
	}
}

func TestHasConceptCycle_MultiHopBackEdge(t *testing.T) {
	// a → b → c → a
	g := buildConceptGraph([]string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}})
	if !HasConceptCycle(g) {
		t.Error("want cycle for a→b→c→a")
	}
}

func TestHasConceptCycle_DiamondNoCycle(t *testing.T) {
	// a → b, a → c, b → d, c → d: DAG diamond, no cycle
	g := buildConceptGraph([]string{"a", "b", "c", "d"}, [][2]string{
		{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"},
	})
	if HasConceptCycle(g) {
		t.Error("want no cycle for diamond DAG")
	}
}

func TestHasConceptCycle_EmptyGraph(t *testing.T) {
	g := &Graph{}
	if HasConceptCycle(g) {
		t.Error("want no cycle for empty graph")
	}
}

func TestHasConceptCycle_IgnoresTestingEdges(t *testing.T) {
	// concept → q1: testing edge; must not count as a cycle
	g := &Graph{}
	g.UntestedConcepts = append(g.UntestedConcepts, &ConceptNode{ID: "a", Block: BlockUntested})
	g.TestingItems = append(g.TestingItems, TestingItem{Q: &QuestionNode{ID: "q1", Class: "probe_1"}})
	g.Edges = append(g.Edges, &Edge{From: "a", To: "q1", Label: ""})
	if HasConceptCycle(g) {
		t.Error("want no cycle for concept→testing edge")
	}
}
