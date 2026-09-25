package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Monotonic ID lifecycle ───────────────────────────────────────────────────

// monotonicGraph is a two-concept graph.  mycon has probe_1 (q1 pending a1,
// q2 pass a2) so that grading q1 pass triggers the full pass procedure.
// other is an untested concept with no questions yet.
func monotonicGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
        other["Other concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["correct answer"]:::pass
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
    classDef pass stroke:#3fb950
`
}

// TestMonotonicIDs_LifecycleNoReuse verifies the core monotonic guarantee:
// after mycon's questions (q1, q2) are removed by the pass procedure, a new
// tm q for "other" allocates q3 — it does not reuse q1 or q2.
func TestMonotonicIDs_LifecycleNoReuse(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, monotonicGraph())

	// Step 1: grade q1 pass → all probe_1 probes pass → mycon passes.
	// This removes q1, q2, a1, a2 from the testing block and logs gc + pass.
	_, errOut, code := run(t, "grade", "q1", "pass", "great answer")
	if code != 0 {
		t.Fatalf("grade q1: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Verify the testing block is empty and mycon is now in passed.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph after grade: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph after grade: %v", parseErr)
	}
	if len(g.TestingItems) != 0 {
		t.Fatalf("testing block not empty after pass: %d items remain", len(g.TestingItems))
	}

	// Step 2: snapshot the event log BEFORE allocating a new question.
	// This captures the max logged ID from prior events (gc nodes have q1,q2,a1,a2).
	logRowsBefore := readEventLog(t, file)
	maxLoggedNBefore := 0
	for _, row := range logRowsBefore {
		collectMaxIDs(row, &maxLoggedNBefore)
	}

	// Step 3: allocate a new question for "other".
	// The old max-in-file logic would yield q1; monotonic allocation yields q3.
	out, errOut2, code2 := run(t, "q", "other", "e266782c2841@src.txt:1-2", "scope for other")
	if code2 != 0 {
		t.Fatalf("tm q other: want exit 0, got %d; stderr:\n%s", code2, errOut2)
	}
	newQID := strings.TrimSpace(out)

	// Must be q3, not q1 or q2.
	if newQID != "q3" {
		t.Errorf("new question ID: want q3, got %q (IDs q1/q2 must not be reused)", newQID)
	}

	// Step 4: verify the graph carries %% tm:next with advanced counters.
	data2, err2 := os.ReadFile(file)
	if err2 != nil {
		t.Fatalf("read graph after tm q: %v", err2)
	}
	g2, parseErr2 := graph.Parse(data2)
	if parseErr2 != nil {
		t.Fatalf("parse graph after tm q: %v", parseErr2)
	}
	if g2.NextMeta == nil {
		t.Fatal("NextMeta absent from graph after tm q")
	}
	if g2.NextMeta.Q <= 3 {
		t.Errorf("NextMeta.Q must be > 3 after allocating q3; got %d", g2.NextMeta.Q)
	}

	// Step 5: verify the new ID exceeds all IDs logged before it was created.
	newN := graph.QuestionN(newQID)
	if newN <= maxLoggedNBefore {
		t.Errorf("new ID %s (N=%d) does not exceed max pre-allocation logged N=%d", newQID, newN, maxLoggedNBefore)
	}
}

// collectMaxIDs walks all string values in a JSON map (including nested arrays
// and objects) and updates maxN to the highest qN/aN numeric suffix found.
func collectMaxIDs(m map[string]any, maxN *int) {
	for _, v := range m {
		collectIDValue(v, maxN)
	}
}

func collectIDValue(v any, maxN *int) {
	switch val := v.(type) {
	case string:
		if graph.ValidQuestionID(val) || graph.ValidAnswerID(val) {
			if n := graph.QuestionN(val); n > *maxN {
				*maxN = n
			}
		}
	case map[string]any:
		collectMaxIDs(val, maxN)
	case []any:
		for _, elem := range val {
			collectIDValue(elem, maxN)
		}
	}
}
