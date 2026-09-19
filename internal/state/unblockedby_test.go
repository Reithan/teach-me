package state

import (
	"reflect"
	"testing"
)

// TestUnblockedBy covers the full set of cases for §5 unblocking:
// a child is unblocked when its only non-passing parent is the passing concept.
func TestUnblockedBy(t *testing.T) {
	tests := []struct {
		name    string
		graph   string
		concept string // the concept that just passed
		want    []string
	}{
		{
			name: "single child unblocked",
			// parent passes → child has no other parents → unblocked.
			graph: `flowchart TB
    subgraph passed["p"]
        other["Other<br/>r.txt:1-5"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:6-10"]
        child["Child<br/>r.txt:11-15"]
        parent --"a"--> child
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    []string{"child"},
		},
		{
			name: "child with two parents both passing",
			// p1 already passed; parent is passing → child unblocked.
			graph: `flowchart TB
    subgraph passed["p"]
        p1["P1<br/>r.txt:1-5"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:6-10"]
        child["Child<br/>r.txt:11-15"]
        p1 --"a"--> child
        parent --"b"--> child
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    []string{"child"},
		},
		{
			name: "child blocked by other untested parent",
			// parent passes, but blocker is still untested → child stays blocked.
			graph: `flowchart TB
    subgraph passed["p"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:1-5"]
        blocker["Blocker<br/>r.txt:6-10"]
        child["Child<br/>r.txt:11-15"]
        parent --"a"--> child
        blocker --"b"--> child
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    nil,
		},
		{
			name: "multiple children, mixed",
			// parent passes; child_a has no other parents → unblocked;
			// child_b also has blocker (untested) → still blocked.
			graph: `flowchart TB
    subgraph passed["p"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:1-5"]
        blocker["Blocker<br/>r.txt:6-10"]
        child_a["Child A<br/>r.txt:11-15"]
        child_b["Child B<br/>r.txt:16-20"]
        parent --"a"--> child_a
        parent --"b"--> child_b
        blocker --"c"--> child_b
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    []string{"child_a"},
		},
		{
			name: "no children",
			// parent passes but has no concept-edges going forward.
			graph: `flowchart TB
    subgraph passed["p"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:1-5"]
        sibling["Sibling<br/>r.txt:6-10"]
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    nil,
		},
		{
			name: "concept is not a parent of anything",
			// passing concept has no outgoing edges; result is empty.
			graph: `flowchart TB
    subgraph passed["p"]
        p1["P1<br/>r.txt:1-5"]
    end
    subgraph untested["u"]
        leaf["Leaf<br/>r.txt:6-10"]
        p1 --"a"--> leaf
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "leaf",
			want:    nil,
		},
		{
			name: "two children both unblocked",
			// parent passes; child_a and child_b each have parent as their
			// only concept-parent → both unblocked (sorted ascending).
			graph: `flowchart TB
    subgraph passed["p"]
    end
    subgraph untested["u"]
        parent["Parent<br/>r.txt:1-5"]
        child_a["Child A<br/>r.txt:6-10"]
        child_b["Child B<br/>r.txt:11-15"]
        parent --"a"--> child_a
        parent --"b"--> child_b
    end
    subgraph testing["t"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`,
			concept: "parent",
			want:    []string{"child_a", "child_b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustLoad(t, tc.graph, defaultCfg())
			got := s.UnblockedBy(tc.concept)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("UnblockedBy(%q) = %v; want %v", tc.concept, got, tc.want)
			}
		})
	}
}
