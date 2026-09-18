package ops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// passCfg returns a default state.Config with no SrcRoot validation.
func passCfg(dir string) state.Config {
	return state.Config{
		ProbeMin: 2, ProbeMax: 5,
		TeachMin: 1, TeachMax: 3,
		MaxFails: 2, MaxTeach: 8, MaxStall: 4,
		SrcRoot: dir,
	}
}

// mustParseState writes content to a temp .mmd file and loads state from it.
func mustParseState(t *testing.T, content string) (*graph.Graph, *state.State) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := state.Load(path, passCfg(dir))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s.Graph(), s
}

// --- fixtures ---

// twoProbePassGraph: concept "target" in untested, with probe_1 batch (q1+q2)
// both answered pass, a gate meta line, and two concepts it connects to.
//
// Edges by 4.2 rule:
//   - parent --"enables"--> target : passed(0) → untested(1) → home=untested
//   - target --"leads to"--> child : untested(1) → untested(1) → home=untested
//   - target --> q1 : untested(1) → testing(2) → home=testing
//   - target --> q2 : untested(1) → testing(2) → home=testing
//   - q1 --> a1 : testing(2) → testing(2) → home=testing
//   - q2 --> a2 : testing(2) → testing(2) → home=testing
const twoProbePassGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
        parent["Parent concept<br/>ref.txt:1-10"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:gate target base=0
        target["Target concept<br/>GAP: a known gap<br/>ref.txt:11-20"]
        child["Child concept<br/>ref.txt:21-30"]
        parent --"enables"--> target
        target --"leads to"--> child
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>ref.txt:11-15"]:::probe_1
        q2["Second probe<br/>ref.txt:15-20"]:::probe_1
        a1["Good answer one"]:::pass
        a2["Good answer two"]:::pass
        target --> q1
        target --> q2
        q1 --> a1
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// withTeachGraph: extends twoProbePassGraph to also include a teach batch.
// probe_1 has one fail (a2 → fail), probe_2 is the fallback, teach_3 exists.
const withTeachGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
        parent["Parent concept<br/>ref.txt:1-10"]
    end
    subgraph untested["Concepts User has not been tested on"]
        target["Target concept<br/>GAP: a known gap<br/>ref.txt:11-20"]
        child["Child concept<br/>ref.txt:21-30"]
        parent --"enables"--> target
        target --"leads to"--> child
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>ref.txt:11-15"]:::probe_1
        q2["Second probe<br/>ref.txt:15-20"]:::probe_1
        a1["Good answer one"]:::pass
        a2["Insufficient answer"]:::fail
        q3["Fallback probe A<br/>ref.txt:11-15"]:::probe_2
        q4["Fallback probe B<br/>ref.txt:15-20"]:::probe_2
        q5["Teach question<br/>ref.txt:11-15"]:::teach_3
        a5["Teach answer pass"]:::pass
        target --> q1
        target --> q2
        q1 --> a1
        q2 --> a2
        target --> q3
        target --> q4
        a2 --> q5
        q5 --> a5
    end
    classDef probe_1,probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestRemoveTestingSubtree_BasicProbes tests removal of a simple probe batch.
func TestRemoveTestingSubtree_BasicProbes(t *testing.T) {
	g, s := mustParseState(t, twoProbePassGraph)

	rst, newG := ops.RemoveTestingSubtree(g, s, "target")

	// Concept and batches.
	if rst.Concept != "target" {
		t.Errorf("Concept = %q; want %q", rst.Concept, "target")
	}
	if len(rst.Batches) != 1 || rst.Batches[0] != "probe_1" {
		t.Errorf("Batches = %v; want [probe_1]", rst.Batches)
	}

	// Nodes: q1, q2 (questions), a1, a2 (answers) in declaration order.
	wantNodeIDs := []string{"q1", "q2", "a1", "a2"}
	if len(rst.Nodes) != len(wantNodeIDs) {
		t.Fatalf("len(Nodes) = %d; want %d", len(rst.Nodes), len(wantNodeIDs))
	}
	for i, id := range wantNodeIDs {
		if rst.Nodes[i].ID != id {
			t.Errorf("Nodes[%d].ID = %q; want %q", i, rst.Nodes[i].ID, id)
		}
	}
	// Verify classes.
	if rst.Nodes[0].Class != "probe_1" {
		t.Errorf("Nodes[0].Class = %q; want %q", rst.Nodes[0].Class, "probe_1")
	}
	if rst.Nodes[2].Class != "pass" {
		t.Errorf("Nodes[2].Class = %q; want %q", rst.Nodes[2].Class, "pass")
	}
	// Verify labels are populated.
	if rst.Nodes[0].Label == "" {
		t.Error("Nodes[0].Label empty; want scope text")
	}
	if rst.Nodes[2].Label == "" {
		t.Error("Nodes[2].Label empty; want answer text")
	}

	// Edges: target→q1, target→q2, q1→a1, q2→a2.
	if len(rst.Edges) != 4 {
		t.Errorf("len(Edges) = %d; want 4", len(rst.Edges))
	}

	// Meta: the gate line for target.
	if len(rst.Meta) != 1 || rst.Meta[0].Concept != "target" {
		t.Errorf("Meta = %v; want one gate meta for target", rst.Meta)
	}

	// New graph must not contain any removed testing items.
	for _, item := range newG.TestingItems {
		if item.Q != nil {
			switch item.Q.ID {
			case "q1", "q2":
				t.Errorf("question %q still in newG", item.Q.ID)
			}
		}
		if item.A != nil {
			switch item.A.ID {
			case "a1", "a2":
				t.Errorf("answer %q still in newG", item.A.ID)
			}
		}
	}

	// Edges incident to removed nodes must be gone.
	for _, e := range newG.Edges {
		for _, id := range []string{"q1", "q2", "a1", "a2"} {
			if e.From == id || e.To == id {
				t.Errorf("edge %s→%s still in newG after removal", e.From, e.To)
			}
		}
	}

	// Gate meta for target must be gone.
	for _, m := range newG.UntestedMetas {
		if m.Concept == "target" {
			t.Error("gate meta for target still in newG")
		}
	}

	// Concept-to-concept edges must be preserved.
	found := map[string]bool{}
	for _, e := range newG.Edges {
		found[e.From+"→"+e.To] = true
	}
	if !found["parent→target"] {
		t.Error("parent→target edge missing from newG")
	}
	if !found["target→child"] {
		t.Error("target→child edge missing from newG")
	}

	// Input graph must be unchanged (copy-on-write).
	if len(g.TestingItems) != len(newG.TestingItems)+4 {
		// original has q1, q2, a1, a2 which newG lacks
		origItems := 0
		for _, it := range g.TestingItems {
			if it.Q != nil || it.A != nil {
				origItems++
			}
		}
		newItems := 0
		for _, it := range newG.TestingItems {
			if it.Q != nil || it.A != nil {
				newItems++
			}
		}
		if origItems != newItems+4 {
			t.Errorf("original TestingItems count=%d, newG=%d; expected diff=4", origItems, newItems)
		}
	}
}

// TestRemoveTestingSubtree_WithTeachBatch tests removal across multiple batches.
func TestRemoveTestingSubtree_WithTeachBatch(t *testing.T) {
	g, s := mustParseState(t, withTeachGraph)

	rst, newG := ops.RemoveTestingSubtree(g, s, "target")

	// All three batches should be present.
	wantBatches := []string{"probe_1", "probe_2", "teach_3"}
	if len(rst.Batches) != len(wantBatches) {
		t.Fatalf("Batches = %v; want %v", rst.Batches, wantBatches)
	}
	for i, b := range wantBatches {
		if rst.Batches[i] != b {
			t.Errorf("Batches[%d] = %q; want %q", i, rst.Batches[i], b)
		}
	}

	// All 8 testing nodes should be collected: q1,q2,a1,a2,q3,q4,q5,a5.
	wantCount := 8
	if len(rst.Nodes) != wantCount {
		t.Errorf("len(Nodes) = %d; want %d", len(rst.Nodes), wantCount)
	}

	// New graph testing block must be empty.
	if len(newG.TestingItems) != 0 {
		t.Errorf("newG.TestingItems not empty (len=%d)", len(newG.TestingItems))
	}

	// No gate meta for target (withTeachGraph has none, no meta lines).
	if len(rst.Meta) != 0 {
		t.Errorf("Meta = %v; want empty for withTeachGraph", rst.Meta)
	}
}

// TestMoveToPassed moves a concept from untested to the top of passed and
// verifies GAP is cleared.
func TestMoveToPassed(t *testing.T) {
	g, _ := mustParseState(t, twoProbePassGraph)

	newG := ops.MoveToPassed(g, "target")

	// target must appear at top of passed.
	if len(newG.PassedConcepts) == 0 {
		t.Fatal("PassedConcepts empty after MoveToPassed")
	}
	top := newG.PassedConcepts[0]
	if top.ID != "target" {
		t.Errorf("PassedConcepts[0].ID = %q; want target", top.ID)
	}
	if top.GAP != "" {
		t.Errorf("PassedConcepts[0].GAP = %q; want empty (stripped)", top.GAP)
	}
	if top.Scope == "" {
		t.Error("PassedConcepts[0].Scope empty; scope must be preserved")
	}
	if top.Block != graph.BlockPassed {
		t.Errorf("PassedConcepts[0].Block = %v; want BlockPassed", top.Block)
	}

	// Previously passed concept (parent) must still be there.
	if len(newG.PassedConcepts) != 2 {
		t.Errorf("len(PassedConcepts) = %d; want 2", len(newG.PassedConcepts))
	}
	if newG.PassedConcepts[1].ID != "parent" {
		t.Errorf("PassedConcepts[1].ID = %q; want parent", newG.PassedConcepts[1].ID)
	}

	// target must be removed from untested.
	for _, c := range newG.UntestedConcepts {
		if c.ID == "target" {
			t.Error("target still in UntestedConcepts after MoveToPassed")
		}
	}

	// child must still be in untested.
	found := false
	for _, c := range newG.UntestedConcepts {
		if c.ID == "child" {
			found = true
		}
	}
	if !found {
		t.Error("child missing from UntestedConcepts")
	}

	// Original graph must be unchanged.
	if len(g.PassedConcepts) != 1 {
		t.Errorf("original PassedConcepts modified (len=%d)", len(g.PassedConcepts))
	}
}

// TestMoveToPassed_NoGAP verifies GAP stripping when GAP is already empty.
func TestMoveToPassed_NoGAP(t *testing.T) {
	// A graph where target has no GAP field.
	const noGAP = `flowchart TB
    subgraph passed["p"]
    end
    subgraph untested["u"]
        target["Target scope<br/>ref.txt:1-10"]
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	g, _ := mustParseState(t, noGAP)
	newG := ops.MoveToPassed(g, "target")

	if len(newG.PassedConcepts) != 1 || newG.PassedConcepts[0].ID != "target" {
		t.Errorf("PassedConcepts = %v; want [target]", newG.PassedConcepts)
	}
	if newG.PassedConcepts[0].GAP != "" {
		t.Errorf("GAP = %q; want empty", newG.PassedConcepts[0].GAP)
	}
}

// TestEdgeRelocationRoundTrip verifies that after RemoveTestingSubtree +
// MoveToPassed + Write + Parse, edges land in the blocks that §4.2 requires.
//
// Before pass:
//   - parent --"enables"--> target: home=untested (target is untested)
//   - target --"leads to"--> child: home=untested
//
// After target moves to passed:
//   - parent --"enables"--> target: home=passed (both endpoints passed)
//   - target --"leads to"--> child: home=untested (child still untested)
func TestEdgeRelocationRoundTrip(t *testing.T) {
	g, s := mustParseState(t, twoProbePassGraph)

	// Remove subtree then move to passed.
	_, g1 := ops.RemoveTestingSubtree(g, s, "target")
	g2 := ops.MoveToPassed(g1, "target")

	// Write and re-parse.
	out := graph.Write(g2)
	g3, err := graph.Parse(out)
	if err != nil {
		t.Fatalf("Parse after Write: %v", err)
	}

	// Verify written output contains both edges.
	outStr := string(out)
	if !strings.Contains(outStr, `parent --"enables"--> target`) {
		t.Error("parent→target edge missing from output")
	}
	if !strings.Contains(outStr, `target --"leads to"--> child`) {
		t.Error("target→child edge missing from output")
	}

	// Build block lookup for the re-parsed graph.
	passedSet := map[string]bool{}
	for _, c := range g3.PassedConcepts {
		passedSet[c.ID] = true
	}
	untestedSet := map[string]bool{}
	for _, c := range g3.UntestedConcepts {
		untestedSet[c.ID] = true
	}

	// Find the two edges in the written text and verify block placement.
	//
	// The writer emits edges inside each subgraph's block.  We find where in
	// the output each edge appears relative to the subgraph boundaries.
	passedStart := strings.Index(outStr, `subgraph passed[`)
	untestedStart := strings.Index(outStr, `subgraph untested[`)
	testingStart := strings.Index(outStr, `subgraph testing[`)

	if passedStart < 0 || untestedStart < 0 || testingStart < 0 {
		t.Fatal("cannot find subgraph boundaries in output")
	}

	parentTargetPos := strings.Index(outStr, `parent --"enables"--> target`)
	targetChildPos := strings.Index(outStr, `target --"leads to"--> child`)

	// parent→target must be in the passed block (between passedStart and untestedStart).
	if parentTargetPos < passedStart || parentTargetPos > untestedStart {
		t.Errorf("parent→target edge at pos %d; want in passed block [%d, %d)",
			parentTargetPos, passedStart, untestedStart)
	}

	// target→child must be in the untested block (between untestedStart and testingStart).
	if targetChildPos < untestedStart || targetChildPos > testingStart {
		t.Errorf("target→child edge at pos %d; want in untested block [%d, %d)",
			targetChildPos, untestedStart, testingStart)
	}

	// target must be in the passed block.
	if !passedSet["target"] {
		t.Error("target not in passed block after round-trip")
	}
	if !untestedSet["child"] {
		t.Error("child not in untested block after round-trip")
	}
}
