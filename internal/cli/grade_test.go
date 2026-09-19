package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Fixtures ──────────────────────────────────────────────────────────────────

// gradeProbeIncompleteGraph: probe_1 with q1 (pending a1) and q2 (unanswered).
// Grading q1 leaves q2 unanswered → batch incomplete → only grade event (step 3).
func gradeProbeIncompleteGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        mycon --> q1
        q1 --> a1
        mycon --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
`
}

// gradeAllPassGraph: probe_1 with q1 (pending a1) and q2 (pass a2).
// Grading q1 pass → all pass → pass procedure runs (step 4).
func gradeAllPassGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
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

// gradeAlreadyGradedGraph: probe_1 with q1 (pass a1, already graded) and q2 unanswered.
// Trying to grade q1 again → exit 1 (no pending answer).
func gradeAlreadyGradedGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["already graded"]:::pass
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        mycon --> q1
        q1 --> a1
        mycon --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
`
}

// gradeReplacementProbeGraph: probe_1 has q1 (unclear a1) and q2 (unclear a2),
// satisfying ProbeMin=2. probe_2 has q3 (pending a3, edge a1→q3) — a replacement
// probe. Grading q3 unclear → Q5 (§8.2): walkToRootProbe(q3) returns (q1,a1)
// and a1.Class=="unclear" → recorded="fail".
func gradeReplacementProbeGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["unclear answer one"]:::unclear
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        a2["unclear answer two"]:::unclear
        q3["Replacement probe<br/>src.txt:1-3"]:::probe_2
        a3["pending replacement answer"]:::pending
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        a1 --> q3
        q3 --> a3
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
}

// gradeProbeUnclearRootGraph: probe_1 with q1 (pending a1), q2 (unclear a2).
// Grading q1 unclear → q1 is a root probe (incoming edge from mycon) → no Q5 →
// recorded="unclear". Batch complete: q1="unclear", q2="unclear" → no fail (step 5).
func gradeProbeUnclearRootGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        a2["unclear answer"]:::unclear
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
    classDef unclear stroke:#d29922
`
}

// gradeProbeWithPassAndPendingGraph: probe_1 with q1 (pass a1), q2 (pending a2).
// Grading q2 fail → batch complete, has fail → only grade event (step 6).
func gradeProbeWithPassAndPendingGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        a2["pending answer"]:::pending
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef pending stroke-dasharray:4 3
`
}

// gradeTeachPendingGraph: probe_1 resolved with fail, probe_2 fallback (q3, q4),
// teach_3 with q5 (pending a5). Used for teach steps 7, 8, 9.
func gradeTeachPendingGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        q3["Fallback probe 1<br/>src.txt:1-2"]:::probe_2
        q4["Fallback probe 2<br/>src.txt:3-4"]:::probe_2
        q5["Teach question<br/>src.txt:1-3"]:::teach_3
        a5["pending teach answer"]:::pending
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        mycon --> q3
        mycon --> q4
        a2 --> q5
        q5 --> a5
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef pending stroke-dasharray:4 3
`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// gradeSetupDir creates a temp dir with src.txt, sets TM_ERRORS and TM_FILE="".
func gradeSetupDir(t *testing.T) (string, string) {
	t.Helper()
	return qSetupDir(t)
}

// gradeWriteGraph writes mmd to dir/g.mmd and sets TM_FILE.
func gradeWriteGraph(t *testing.T, dir, mmd string) string {
	t.Helper()
	return qWriteGraph(t, dir, mmd)
}

// ── Exit 3 (unknown qid) ──────────────────────────────────────────────────────

func TestGrade_UnknownQID_Exit3(t *testing.T) {
	dir, errPath := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeProbeIncompleteGraph())

	_, errOut, code := run(t, "grade", "q99", "pass", "summary")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, `err: unknown question "q99"`) {
		t.Errorf("want unknown question err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm grade") {
		t.Errorf("want fix: usage; got:\n%s", errOut)
	}

	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("want 1 errlog row exit 3; got %v", rows)
	}
	_ = file
}

// ── Exit 1: no pending answer ─────────────────────────────────────────────────

func TestGrade_AlreadyGradedAnswer_Exit1(t *testing.T) {
	// q1 has a "pass" answer — not pending. Grade refuses (§7 line 266).
	dir, _ := gradeSetupDir(t)
	gradeWriteGraph(t, dir, gradeAlreadyGradedGraph())

	_, errOut, code := run(t, "grade", "q1", "pass", "summary")
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "err: q1 has no pending answer") {
		t.Errorf("want no-pending err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
}

func TestGrade_NoAnswer_Exit1(t *testing.T) {
	// q2 in gradeProbeIncompleteGraph has no answer at all.
	dir, _ := gradeSetupDir(t)
	gradeWriteGraph(t, dir, gradeProbeIncompleteGraph())

	_, errOut, code := run(t, "grade", "q2", "pass", "summary")
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "err: q2 has no pending answer") {
		t.Errorf("want no-pending err; got:\n%s", errOut)
	}
}

// ── Exit 1: --oos on a probe question ─────────────────────────────────────────

func TestGrade_OOSOnProbe_Exit1(t *testing.T) {
	// §7 line 267: --oos is only valid for teach questions.
	dir, _ := gradeSetupDir(t)
	gradeWriteGraph(t, dir, gradeProbeIncompleteGraph())

	_, errOut, code := run(t, "grade", "q1", "pass", "summary", "--oos")
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "oos") {
		t.Errorf("want oos in err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
}

// ── ForbidTeacher: handled by dispatcher ─────────────────────────────────────
// Already covered by TestRoleGuard_TeacherForbidden_Grade in cli_test.go.

// ── Step 3: batch incomplete ──────────────────────────────────────────────────

func TestGrade_StepThree_BatchIncomplete(t *testing.T) {
	// probe_1 has q1 (pending) and q2 (unanswered). Grading q1 → batch still
	// has unanswered q2 → stop after writing grade event.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeProbeIncompleteGraph())

	out, errOut, code := run(t, "grade", "q1", "pass", "grade summary")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Graph: a1 class → pass, label → "grade summary"; q2 still unanswered.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	var a1 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a1" {
			a1 = item.A
			break
		}
	}
	if a1 == nil {
		t.Fatal("a1 not found after grade")
	}
	if a1.Class != "pass" {
		t.Errorf("a1.Class: want 'pass', got %q", a1.Class)
	}
	if a1.Label != "grade summary" {
		t.Errorf("a1.Label: want 'grade summary', got %q", a1.Label)
	}

	// mycon must still be in untested (not passed).
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if myconPassed {
		t.Error("mycon should NOT be in passed (batch incomplete)")
	}

	lintFile(t, file, dir)

	// Event log: exactly one "grade" event; no gc or pass events.
	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event row, got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["q"] != "q1" {
		t.Errorf("q: want 'q1', got %v", row["q"])
	}
	if row["verdict"] != "pass" {
		t.Errorf("verdict: want 'pass', got %v", row["verdict"])
	}
	if row["recorded"] != "pass" {
		t.Errorf("recorded: want 'pass', got %v", row["recorded"])
	}
	if row["summary"] != "grade summary" {
		t.Errorf("summary: want 'grade summary', got %v", row["summary"])
	}
	if row["raw"] != "pending answer" {
		t.Errorf("raw: want 'pending answer', got %v", row["raw"])
	}
	if row["guided"] != false {
		t.Errorf("guided: want false, got %v", row["guided"])
	}
	if row["oos"] != false {
		t.Errorf("oos: want false, got %v", row["oos"])
	}
	// src_text: q1 cites src.txt:1-2 → "line 1\nline 2"
	if _, ok := row["src_text"]; !ok {
		t.Error("grade event missing src_text field")
	}
}

// ── Step 4: probe batch, all pass ─────────────────────────────────────────────

func TestGrade_StepFour_ProbeAllPass(t *testing.T) {
	// probe_1: q1 (pending a1), q2 (pass a2). Grading q1 pass →
	// all pass → pass procedure: remove testing subtree, move mycon to passed.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeAllPassGraph())

	out, errOut, code := run(t, "grade", "q1", "pass", "great answer")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Graph: mycon in passed, testing block empty.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	// mycon must be in passed.
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("mycon not in passed after all-pass grade")
	}

	// mycon must NOT be in untested.
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			t.Error("mycon still in untested after all-pass grade")
		}
	}

	// Testing block must be empty (all nodes removed).
	if len(g.TestingItems) != 0 {
		t.Errorf("testing block not empty: %d items remain", len(g.TestingItems))
	}

	// No edges remaining (all were incident to removed nodes).
	if len(g.Edges) != 0 {
		t.Errorf("edges not fully removed: %d remain", len(g.Edges))
	}

	lintFile(t, file, dir)

	// Event log: grade event, then gc event, then pass event.
	rows := readEventLog(t, file)
	if len(rows) != 3 {
		t.Fatalf("want 3 events (grade, gc, pass), got %d", len(rows))
	}

	// grade event.
	grade := rows[0]
	if grade["ev"] != "grade" {
		t.Errorf("event 0 ev: want 'grade', got %v", grade["ev"])
	}
	if grade["verdict"] != "pass" {
		t.Errorf("grade verdict: want 'pass', got %v", grade["verdict"])
	}
	if grade["recorded"] != "pass" {
		t.Errorf("grade recorded: want 'pass', got %v", grade["recorded"])
	}
	if grade["summary"] != "great answer" {
		t.Errorf("grade summary: want 'great answer', got %v", grade["summary"])
	}
	if grade["raw"] != "pending answer" {
		t.Errorf("grade raw: want 'pending answer', got %v", grade["raw"])
	}

	// gc event.
	gcRow := rows[1]
	if gcRow["ev"] != "gc" {
		t.Errorf("event 1 ev: want 'gc', got %v", gcRow["ev"])
	}
	if gcRow["concept"] != "mycon" {
		t.Errorf("gc concept: want 'mycon', got %v", gcRow["concept"])
	}
	if gcRow["reason"] != "pass" {
		t.Errorf("gc reason: want 'pass', got %v", gcRow["reason"])
	}

	// gc nodes: q1, a1, q2, a2 in declaration order.
	gcNodes, ok := gcRow["nodes"].([]interface{})
	if !ok {
		t.Fatalf("gc nodes: want array, got %T", gcRow["nodes"])
	}
	if len(gcNodes) != 4 {
		t.Fatalf("gc nodes: want 4, got %d", len(gcNodes))
	}

	nodeIDs := make([]string, 0, len(gcNodes))
	for _, n := range gcNodes {
		nm, ok := n.(map[string]interface{})
		if !ok {
			t.Fatalf("gc node: want map, got %T", n)
		}
		nodeIDs = append(nodeIDs, nm["id"].(string))
	}
	wantNodeIDs := []string{"q1", "a1", "q2", "a2"}
	for i, want := range wantNodeIDs {
		if nodeIDs[i] != want {
			t.Errorf("gc node[%d].id: want %q, got %q", i, want, nodeIDs[i])
		}
	}

	// gc edges: all 4 edges (mycon→q1, q1→a1, mycon→q2, q2→a2).
	gcEdges, ok := gcRow["edges"].([]interface{})
	if !ok {
		t.Fatalf("gc edges: want array, got %T", gcRow["edges"])
	}
	if len(gcEdges) != 4 {
		t.Errorf("gc edges: want 4, got %d", len(gcEdges))
	}

	// gc meta: empty (no gate lines).
	gcMeta, ok := gcRow["meta"].([]interface{})
	if !ok {
		t.Fatalf("gc meta: want array, got %T", gcRow["meta"])
	}
	if len(gcMeta) != 0 {
		t.Errorf("gc meta: want empty, got %d", len(gcMeta))
	}

	// pass event.
	passRow := rows[2]
	if passRow["ev"] != "pass" {
		t.Errorf("event 2 ev: want 'pass', got %v", passRow["ev"])
	}
	if passRow["concept"] != "mycon" {
		t.Errorf("pass concept: want 'mycon', got %v", passRow["concept"])
	}
	batches, ok := passRow["batches"].([]interface{})
	if !ok {
		t.Fatalf("pass batches: want array, got %T", passRow["batches"])
	}
	if len(batches) != 1 || batches[0] != "probe_1" {
		t.Errorf("pass batches: want ['probe_1'], got %v", batches)
	}
	unblocked, ok := passRow["unblocked"].([]interface{})
	if !ok {
		// nil is also acceptable when unblocked is empty.
		if passRow["unblocked"] != nil {
			t.Errorf("pass unblocked: want empty array or null, got %v", passRow["unblocked"])
		}
		unblocked = nil
	}
	if len(unblocked) != 0 {
		t.Errorf("pass unblocked: want empty, got %v", unblocked)
	}
}

// ── Step 2 (Q5): unclear verdict, root probe also unclear ─────────────────────

func TestGrade_StepTwo_UnclearRootedAtUnclear(t *testing.T) {
	// probe_1 has q1 (unclear a1) and q2 (unclear a2). probe_2 has q3
	// (replacement probe, edge a1→q3) with pending a3.
	// Grading q3 unclear: Q5 applies because walkToRootProbe(q3) returns
	// (q1, a1) and a1.Class=="unclear" → recorded="fail".
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeReplacementProbeGraph())

	out, errOut, code := run(t, "grade", "q3", "unclear", "still confused")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Graph: a3.Class should be "fail" (stored per Q5), Label = "still confused".
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	var a3 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a3" {
			a3 = item.A
			break
		}
	}
	if a3 == nil {
		t.Fatal("a3 not found after grade")
	}
	if a3.Class != "fail" {
		t.Errorf("a3.Class: want 'fail' (Q5 recorded), got %q", a3.Class)
	}
	if a3.Label != "still confused" {
		t.Errorf("a3.Label: want 'still confused', got %q", a3.Label)
	}

	lintFile(t, file, dir)

	// Event log: grade event with verdict="unclear", recorded="fail".
	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event, got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["q"] != "q3" {
		t.Errorf("q: want 'q3', got %v", row["q"])
	}
	if row["verdict"] != "unclear" {
		t.Errorf("verdict: want 'unclear', got %v", row["verdict"])
	}
	if row["recorded"] != "fail" {
		t.Errorf("recorded: want 'fail' (Q5), got %v", row["recorded"])
	}
}

// ── Step 5: probe batch, no fail, some unclear ────────────────────────────────

func TestGrade_StepFive_ProbeNoFailSomeUnclear(t *testing.T) {
	// probe_1 q1 (pending a1), q2 (unclear a2). q1 is a root probe (mycon→q1)
	// so Q5 does not apply. Grading q1 unclear → batch complete: q1=unclear,
	// q2=unclear → no fail, some unclear → only grade event, no structural change.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeProbeUnclearRootGraph())

	out, errOut, code := run(t, "grade", "q1", "unclear", "ambiguous question")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// mycon must still be in untested (no pass).
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("mycon should NOT pass on unclear verdict")
		}
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event (no gc/pass), got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["verdict"] != "unclear" {
		t.Errorf("verdict: want 'unclear', got %v", row["verdict"])
	}
	// Q5 does NOT apply (q1 is a root probe → walkToRootProbe returns nil,nil).
	if row["recorded"] != "unclear" {
		t.Errorf("recorded: want 'unclear' (no Q5), got %v", row["recorded"])
	}
}

// ── Step 6: probe batch, any fail ─────────────────────────────────────────────

func TestGrade_StepSix_ProbeAnyFail(t *testing.T) {
	// probe_1 q1 (pass a1), q2 (pending a2). Grading q2 fail → batch complete,
	// has fail → only grade event, no structural change.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeProbeWithPassAndPendingGraph())

	out, errOut, code := run(t, "grade", "q2", "fail", "wrong answer")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("mycon should NOT pass after fail verdict")
		}
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event (no gc/pass), got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["verdict"] != "fail" {
		t.Errorf("verdict: want 'fail', got %v", row["verdict"])
	}
	if row["recorded"] != "fail" {
		t.Errorf("recorded: want 'fail', got %v", row["recorded"])
	}
}

// ── Step 7: --oos on a teach question ─────────────────────────────────────────

func TestGrade_StepSeven_OOSOnTeach(t *testing.T) {
	// Grade q5 (teach question) with --oos → a5.OOS=true, event.oos=true.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeTeachPendingGraph())

	out, errOut, code := run(t, "grade", "q5", "pass", "out of scope answer", "--oos")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// a5 must have OOS=true and Class="pass".
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	var a5 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a5" {
			a5 = item.A
			break
		}
	}
	if a5 == nil {
		t.Fatal("a5 not found after grade")
	}
	if !a5.OOS {
		t.Error("a5.OOS: want true (--oos flag)")
	}
	if a5.Class != "pass" {
		t.Errorf("a5.Class: want 'pass', got %q", a5.Class)
	}

	lintFile(t, file, dir)

	// Event log: grade event with oos=true, no gc/pass.
	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event, got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["oos"] != true {
		t.Errorf("oos: want true, got %v", row["oos"])
	}
	if row["verdict"] != "pass" {
		t.Errorf("verdict: want 'pass', got %v", row["verdict"])
	}
}

// ── Step 8: teach batch, all in-scope pass ────────────────────────────────────

func TestGrade_StepEight_TeachAllPass(t *testing.T) {
	// Grade teach q5 pass (no --oos). Batch teach_3 complete, all in-scope pass.
	// No structural writes (step 8 is derived). Only grade event.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeTeachPendingGraph())

	out, errOut, code := run(t, "grade", "q5", "pass", "well explained")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event (no gc/pass), got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["verdict"] != "pass" {
		t.Errorf("verdict: want 'pass', got %v", row["verdict"])
	}
	// mycon still untested (teach verdicts do not contribute to concept pass).
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("mycon must NOT pass from teach verdict")
		}
	}
}

// ── Step 9: teach batch, any fail, not spent ─────────────────────────────────

func TestGrade_StepNine_TeachFail(t *testing.T) {
	// Grade teach q5 fail. Teaching not spent (TeachCount=1 < MaxTeach=8).
	// No structural writes (step 9 is derived). Only grade event.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeTeachPendingGraph())

	out, errOut, code := run(t, "grade", "q5", "fail", "did not explain well")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event, got %d", len(rows))
	}
	row := rows[0]
	if row["ev"] != "grade" {
		t.Errorf("ev: want 'grade', got %v", row["ev"])
	}
	if row["verdict"] != "fail" {
		t.Errorf("verdict: want 'fail', got %v", row["verdict"])
	}
	if row["recorded"] != "fail" {
		t.Errorf("recorded: want 'fail', got %v", row["recorded"])
	}
}

// ── --guided flag ─────────────────────────────────────────────────────────────

func TestGrade_GuidedFlag(t *testing.T) {
	// --guided sets guided=true in the grade event.
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, gradeProbeIncompleteGraph())

	out, errOut, code := run(t, "grade", "q1", "pass", "biased summary", "--guided")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	rows := readEventLog(t, file)
	if len(rows) != 1 {
		t.Fatalf("want 1 event, got %d", len(rows))
	}
	row := rows[0]
	if row["guided"] != true {
		t.Errorf("guided: want true, got %v", row["guided"])
	}
}

// ── Pass procedure: unblocked field ──────────────────────────────────────────

func TestGrade_AllPass_Unblocked(t *testing.T) {
	// When mycon passes and child_con depends only on mycon, child_con appears
	// in the pass event's unblocked field.
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-5"]
        child_con["Child concept<br/>src.txt:1-5"]
        mycon --"requires"--> child_con
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>src.txt:3-4"]:::probe_1
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
	dir, _ := gradeSetupDir(t)
	file := gradeWriteGraph(t, dir, mmd)
	// Set up src file for the child_con citation too (same src.txt range).

	out, errOut, code := run(t, "grade", "q1", "pass", "good")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	if len(rows) != 3 {
		t.Fatalf("want 3 events (grade, gc, pass), got %d", len(rows))
	}
	passRow := rows[2]
	if passRow["ev"] != "pass" {
		t.Errorf("event 2 ev: want 'pass', got %v", passRow["ev"])
	}
	unblocked, ok := passRow["unblocked"].([]interface{})
	if !ok || len(unblocked) == 0 {
		t.Errorf("pass unblocked: want [child_con], got %v", passRow["unblocked"])
	} else if unblocked[0] != "child_con" {
		t.Errorf("pass unblocked[0]: want 'child_con', got %v", unblocked[0])
	}

	_ = filepath.Join(dir, "g.mmd") // silence unused import
}
