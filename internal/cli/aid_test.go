package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Fixtures ──────────────────────────────────────────────────────────────────

const aidBaseGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ca["Concept A<br/>f5ca3875b379@src.txt:1-5"]
        cc["Concept C<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        cb["Concept B<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["q scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["pending answer"]:::pending
        cc --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// aidAllPassGraph has q1 and q2 both with pending answers → grading q1 pass
// makes all-pass → gc fires. q1 will have an aid added before grading.
const aidAllPassGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>f5ca3875b379@src.txt:1-2"]:::probe_1
        a1["pending answer"]:::pending
        q2["Second probe<br/>f5ca3875b379@src.txt:3-5"]:::probe_1
        a2["correct answer"]:::pass
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// aidSetupDir creates a temp dir, sets TM_FILE="", TM_ERRORS, TM_PROBE_MIN=1,
// and creates src.txt. TM_PROBE_MIN=1 lets single-question probe batches pass lint.
func aidSetupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	return dir
}

// readGraph reads and parses the graph at path.
func readGraph(t *testing.T, path string) *graph.Graph {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, err := graph.Parse(data)
	if err != nil {
		t.Fatalf("parse graph: %v", err)
	}
	return g
}

// findConceptAids returns Aids from the concept with the given id, searching all blocks.
func findConceptAids(g *graph.Graph, id string) []string {
	for _, c := range g.PassedConcepts {
		if c.ID == id {
			return c.Aids
		}
	}
	for _, c := range g.UntestedConcepts {
		if c.ID == id {
			return c.Aids
		}
	}
	for _, c := range g.ReserveConcepts {
		if c.ID == id {
			return c.Aids
		}
	}
	return nil
}

// findQuestionAids returns Aids from the question with the given id.
func findQuestionAids(g *graph.Graph, id string) []string {
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == id {
			return item.Q.Aids
		}
	}
	return nil
}

// ── tm aid add ────────────────────────────────────────────────────────────────

func TestAid_AddConceptUntested(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	out, errOut, code := run(t, "aid", "ca", "aids/ca-notes.pdf")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr=%q", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	g := readGraph(t, path)
	if aids := findConceptAids(g, "ca"); len(aids) != 1 || aids[0] != "aids/ca-notes.pdf" {
		t.Errorf("ca.Aids: want [aids/ca-notes.pdf], got %v", aids)
	}

	rows := readEventLog(t, path)
	last := rows[len(rows)-1]
	if last["ev"] != "aid" {
		t.Errorf("event ev: want 'aid', got %v", last["ev"])
	}
	if last["id"] != "ca" || last["path"] != "aids/ca-notes.pdf" || last["action"] != "add" {
		t.Errorf("aid event fields: %v", last)
	}
}

func TestAid_AddConceptReserve(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	_, _, code := run(t, "aid", "cb", "aids/cb.txt")
	if code != 0 {
		t.Fatalf("want exit 0 for reserve concept, got %d", code)
	}

	g := readGraph(t, path)
	if aids := findConceptAids(g, "cb"); len(aids) != 1 || aids[0] != "aids/cb.txt" {
		t.Errorf("cb.Aids: want [aids/cb.txt], got %v", aids)
	}
}

func TestAid_AddQuestion(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	_, _, code := run(t, "aid", "q1", "aids/q1-help.md")
	if code != 0 {
		t.Fatalf("want exit 0 for question aid, got %d", code)
	}

	g := readGraph(t, path)
	if aids := findQuestionAids(g, "q1"); len(aids) != 1 || aids[0] != "aids/q1-help.md" {
		t.Errorf("q1.Aids: want [aids/q1-help.md], got %v", aids)
	}
}

// ── tm aid rm ────────────────────────────────────────────────────────────────

func TestAid_RemoveConcept(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	// Add first.
	if _, _, code := run(t, "aid", "ca", "aids/rm-me.txt"); code != 0 {
		t.Fatal("add failed")
	}

	// Remove.
	out, errOut, code := run(t, "aid", "rm", "ca", "aids/rm-me.txt")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr=%q", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	g := readGraph(t, path)
	if aids := findConceptAids(g, "ca"); len(aids) != 0 {
		t.Errorf("ca.Aids after rm: want [], got %v", aids)
	}

	rows := readEventLog(t, path)
	last := rows[len(rows)-1]
	if last["ev"] != "aid" || last["action"] != "rm" {
		t.Errorf("expected aid event with action=rm; got %v", last)
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestAid_UnknownID_Exit3(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	_, errOut, code := run(t, "aid", "zz99", "aids/x.txt")
	if code != 3 {
		t.Errorf("want exit 3, got %d; stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "unknown id") {
		t.Errorf("want 'unknown id' in stderr; got %q", errOut)
	}
}

func TestAid_DuplicateAdd_Exit1(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	run(t, "aid", "ca", "aids/dup.txt")

	_, errOut, code := run(t, "aid", "ca", "aids/dup.txt")
	if code != 1 {
		t.Errorf("want exit 1 for duplicate, got %d; stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "already links") {
		t.Errorf("want 'already links' in stderr; got %q", errOut)
	}
}

func TestAid_RemoveMissing_Exit1(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	_, errOut, code := run(t, "aid", "rm", "ca", "aids/notlinked.txt")
	if code != 1 {
		t.Errorf("want exit 1 for missing aid, got %d; stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "does not link") {
		t.Errorf("want 'does not link' in stderr; got %q", errOut)
	}
}

// ── gc carries aids ───────────────────────────────────────────────────────────

func TestAid_GCCarriesAids(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidAllPassGraph)
	t.Setenv("TM_FILE", path)

	// Link an aid to q1.
	if _, _, code := run(t, "aid", "q1", "aids/q1.pdf"); code != 0 {
		t.Fatal("aid add q1 failed")
	}

	// Grade q1 pass → all pass → gc fires.
	out, errOut, code := run(t, "grade", "q1", "pass", "looks good")
	if code != 0 {
		t.Fatalf("grade q1 pass: exit %d; stdout=%q stderr=%q", code, out, errOut)
	}

	// Check gc event carries aids.
	rows := readEventLog(t, path)
	var gcRow map[string]any
	for _, r := range rows {
		if r["ev"] == "gc" {
			gcRow = r
			break
		}
	}
	if gcRow == nil {
		t.Fatal("no gc event in event log")
	}
	meta, ok := gcRow["meta"].([]interface{})
	if !ok {
		t.Fatalf("gc meta field missing or wrong type; row: %v", gcRow)
	}
	var foundAid bool
	for _, entry := range meta {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if e["aid"] == "q1" && e["path"] == "aids/q1.pdf" {
			foundAid = true
			break
		}
	}
	if !foundAid {
		t.Errorf("gc meta: missing aid entry {aid:q1, path:aids/q1.pdf}; meta=%v", meta)
	}

	// Aid line must be gone from graph.
	g := readGraph(t, path)
	if got := findQuestionAids(g, "q1"); len(got) > 0 {
		t.Errorf("q1.Aids after gc: want empty, got %v", got)
	}
}

// ── tm show includes aids ─────────────────────────────────────────────────────

func TestAid_ShowConceptIncludesAids(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	run(t, "aid", "ca", "aids/ref.pdf")

	out, _, code := run(t, "show", "ca")
	if code != 0 {
		t.Fatalf("show ca: exit %d", code)
	}
	if !strings.Contains(out, "aids: aids/ref.pdf") {
		t.Errorf("show ca: want 'aids: aids/ref.pdf'; got:\n%s", out)
	}
}

func TestAid_ShowQuestionIncludesAids(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	run(t, "aid", "q1", "aids/q-ref.md")

	out, _, code := run(t, "show", "q1")
	if code != 0 {
		t.Fatalf("show q1: exit %d", code)
	}
	if !strings.Contains(out, "aids: aids/q-ref.md") {
		t.Errorf("show q1: want 'aids: aids/q-ref.md'; got:\n%s", out)
	}
}

// ── aid moves with concept during reserve ─────────────────────────────────────

func TestAid_MovesWithConceptOnReserve(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)

	// Add aid to ca (untested).
	if _, _, code := run(t, "aid", "ca", "aids/reserve-test.txt"); code != 0 {
		t.Fatal("aid add failed")
	}

	// Move ca to reserve.
	if _, _, code := run(t, "reserve", "ca"); code != 0 {
		t.Fatal("reserve ca failed")
	}

	// Aid must still be attached to ca in reserve block.
	g := readGraph(t, path)
	if aids := findConceptAids(g, "ca"); len(aids) != 1 || aids[0] != "aids/reserve-test.txt" {
		t.Errorf("ca.Aids after reserve: want [aids/reserve-test.txt], got %v", aids)
	}
}

// ── grader is forbidden ───────────────────────────────────────────────────────

func TestAid_ForbidGrader(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidBaseGraph)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_ROLE", "grader")

	_, errOut, code := run(t, "aid", "ca", "aids/x.txt")
	if code == 0 {
		t.Errorf("want non-zero exit for grader role, got 0")
	}
	if !strings.Contains(errOut, "grader") && !strings.Contains(errOut, "role") {
		t.Errorf("want role error in stderr; got %q", errOut)
	}
}

// ── readEventLogJSON reads the EVENTS.jsonl embedded in the graph dir ─────────

// Verify the gc aids field is correctly encoded as a JSON object.
func TestAid_GCEventAidsEncoding(t *testing.T) {
	dir := aidSetupDir(t)
	path := writeGraph(t, dir, aidAllPassGraph)
	t.Setenv("TM_FILE", path)

	run(t, "aid", "q1", "aids/enc.pdf")
	run(t, "grade", "q1", "pass", "ok")

	rows := readEventLog(t, path)
	var gcRow map[string]any
	for _, r := range rows {
		if r["ev"] == "gc" {
			gcRow = r
			break
		}
	}
	if gcRow == nil {
		t.Fatal("no gc event")
	}

	// Verify the meta list contains an aid entry for q1.
	meta, ok := gcRow["meta"].([]interface{})
	if !ok {
		t.Fatalf("gc meta field missing or wrong type; row: %v", gcRow)
	}
	var foundAid bool
	for _, entry := range meta {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if e["aid"] == "q1" {
			foundAid = true
			break
		}
	}
	if !foundAid {
		t.Errorf("gc meta: no aid entry for q1; meta=%v", meta)
	}
}
