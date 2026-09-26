package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// copyFixtureTo copies the fixture at fixturePath to dir/g.mmd (to avoid
// leaking an event log onto the checked-in fixture) and sets TM_FILE to the
// copy. fixturePath must be resolved before any t.Chdir call.
func copyFixtureTo(t *testing.T, fixturePath, dir string) {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	dst := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
	t.Setenv("TM_FILE", dst)
}

// ── tm edit tests ─────────────────────────────────────────────────────────────

// TestEdit_HappyPath verifies that tm edit rewrites a concept's scope,
// round-trips, lints clean, and logs an "edit" event with before/after scope.
func TestEdit_HappyPath(t *testing.T) {
	dir, _ := qSetupDir(t)

	file := newGraph(t, dir)

	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "Original scope")
	if c1 != 0 {
		t.Fatalf("add: exit %d", c1)
	}

	out, errOut, code := run(t, "edit", "mycon", "Updated scope")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want stdout 'ok', got %q", out)
	}

	// Verify scope changed in graph.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	found := false
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			found = true
			if c.Scope != "Updated scope" {
				t.Errorf("scope: want 'Updated scope', got %q", c.Scope)
			}
		}
	}
	if !found {
		t.Error("concept 'mycon' not found in untested block after edit")
	}

	// Round-trip check.
	roundTripped := graph.Write(g)
	if string(data) != string(roundTripped) {
		t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: find the "edit" event.
	rows := readEventLog(t, file)
	var editRow map[string]any
	for _, r := range rows {
		if r["ev"] == "edit" {
			editRow = r
			break
		}
	}
	if editRow == nil {
		t.Fatal("no 'edit' event found in event log")
	}
	if editRow["id"] != "mycon" {
		t.Errorf("edit event id: want 'mycon', got %v", editRow["id"])
	}
	if editRow["before"] != "Original scope" {
		t.Errorf("edit event before: want 'Original scope', got %v", editRow["before"])
	}
	if editRow["after"] != "Updated scope" {
		t.Errorf("edit event after: want 'Updated scope', got %v", editRow["after"])
	}
}

// TestEdit_SrcFlag verifies that --src updates the citation and lints clean.
func TestEdit_SrcFlag(t *testing.T) {
	dir, _ := qSetupDir(t)

	file := newGraph(t, dir)

	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "My scope")
	if c1 != 0 {
		t.Fatalf("add: exit %d", c1)
	}

	out, errOut, code := run(t, "edit", "mycon", "My scope", "--src", "b8ea715cd2ec@src.txt:3-7")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify citation changed.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			if len(c.Cites) != 1 || c.Cites[0] != "b8ea715cd2ec@src.txt:3-7" {
				t.Errorf("cites: want [b8ea715cd2ec@src.txt:3-7], got %v", c.Cites)
			}
		}
	}

	lintFile(t, file, dir)
}

// TestEdit_BadSrcParse verifies that a malformed --src citation exits 3.
func TestEdit_BadSrcParse(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, _, _ = run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")

	_, errOut, code := run(t, "edit", "mycon", "scope", "--src", "no-range")
	if code != 3 {
		t.Fatalf("want exit 3 for bad citation, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm edit") {
		t.Errorf("want fix: usage line; got:\n%s", errOut)
	}
}

// TestEdit_BadSrcOutOfBounds verifies that an out-of-bounds --src citation exits 3.
func TestEdit_BadSrcOutOfBounds(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, _, _ = run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")

	// src.txt has 10 lines; request 100.
	_, errOut, code := run(t, "edit", "mycon", "scope", "--src", "src.txt:5-100")
	if code != 3 {
		t.Fatalf("want exit 3 for out-of-bounds citation, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm edit") {
		t.Errorf("want fix: usage line; got:\n%s", errOut)
	}
}

// TestEdit_UnknownID verifies that editing an unknown ID exits 3.
func TestEdit_UnknownID(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "edit", "nonexistent", "new scope")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestEdit_QuestionID verifies that editing a question ID exits 1 with
// "is not a concept".
func TestEdit_QuestionID(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "edit", "q1", "new scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 is not a concept") {
		t.Errorf("want 'not a concept' err; got:\n%s", errOut)
	}
}

// TestEdit_PassedConcept verifies that editing a passed concept exits 1 with
// a reopen fix hint.
func TestEdit_PassedConcept(t *testing.T) {
	dir, _ := qSetupDir(t)
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "edit", "passed_c", "new scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: passed_c is passed") {
		t.Errorf("want 'is passed' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen passed_c") {
		t.Errorf("want reopen fix hint; got:\n%s", errOut)
	}
}

// TestEdit_HasQuestions verifies that editing a concept that has questions
// exits 1 with "has questions". Uses check_probe.mmd fixture which has
// mycon with two probe questions attached (M6 q command not yet available).
func TestEdit_HasQuestions(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "edit", "mycon", "new scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon has questions") {
		t.Errorf("want 'has questions' err; got:\n%s", errOut)
	}
}

// ── tm drop tests ─────────────────────────────────────────────────────────────

// TestDrop_HappyPath verifies that dropping a leaf concept removes it and its
// incoming edge, the graph round-trips and lints clean, and logs a "drop" event
// with the correct node and edges fields.
func TestDrop_HappyPath(t *testing.T) {
	dir, _ := qSetupDir(t)

	file := newGraph(t, dir)

	// Add two concepts and link A → B.
	_, _, c1 := run(t, "add", "a", "f5ca3875b379@src.txt:1-5", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "b", "f5ca3875b379@src.txt:1-5", "concept b")
	if c2 != 0 {
		t.Fatalf("add b: exit %d", c2)
	}
	_, _, c3 := run(t, "link", "a", "b", "prereq for")
	if c3 != 0 {
		t.Fatalf("link a→b: exit %d", c3)
	}

	// Drop b (leaf: no outgoing concept→concept edges).
	out, errOut, code := run(t, "drop", "b")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify b is removed and a→b edge is gone.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	for _, c := range g.UntestedConcepts {
		if c.ID == "b" {
			t.Error("concept 'b' should be removed")
		}
	}
	for _, e := range g.Edges {
		if (e.From == "a" && e.To == "b") || (e.From == "b" || e.To == "b") {
			t.Errorf("edge touching 'b' should be removed: %+v", e)
		}
	}

	// Round-trip check.
	roundTripped := graph.Write(g)
	if string(data) != string(roundTripped) {
		t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: find "drop" event.
	rows := readEventLog(t, file)
	var dropRow map[string]any
	for _, r := range rows {
		if r["ev"] == "drop" {
			dropRow = r
			break
		}
	}
	if dropRow == nil {
		t.Fatal("no 'drop' event found in event log")
	}
	if dropRow["id"] != "b" {
		t.Errorf("drop event id: want 'b', got %v", dropRow["id"])
	}
	// node should have scope and src fields.
	nodeObj, ok := dropRow["node"].(map[string]any)
	if !ok {
		t.Fatalf("drop event node: want object, got %T: %v", dropRow["node"], dropRow["node"])
	}
	if nodeObj["scope"] != "concept b" {
		t.Errorf("drop event node.scope: want 'concept b', got %v", nodeObj["scope"])
	}
	// edges should contain the removed a→b edge.
	edgeList, ok := dropRow["edges"].([]any)
	if !ok {
		t.Fatalf("drop event edges: want array, got %T: %v", dropRow["edges"], dropRow["edges"])
	}
	if len(edgeList) != 1 {
		t.Fatalf("drop event edges: want 1 edge, got %d", len(edgeList))
	}
	edgeObj, ok := edgeList[0].(map[string]any)
	if !ok {
		t.Fatalf("drop event edge: want object, got %T", edgeList[0])
	}
	if edgeObj["from"] != "a" || edgeObj["to"] != "b" || edgeObj["rel"] != "prereq for" {
		t.Errorf("drop event edge: want {from:a, to:b, rel:'prereq for'}, got %v", edgeObj)
	}
}

// TestDrop_HasChildren verifies that dropping a concept with dependents exits 1.
func TestDrop_HasChildren(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, _, c1 := run(t, "add", "parent", "f5ca3875b379@src.txt:1-5", "parent concept")
	if c1 != 0 {
		t.Fatalf("add parent: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "child", "f5ca3875b379@src.txt:1-5", "child concept")
	if c2 != 0 {
		t.Fatalf("add child: exit %d", c2)
	}
	_, _, c3 := run(t, "link", "parent", "child", "leads to")
	if c3 != 0 {
		t.Fatalf("link: exit %d", c3)
	}

	// Try to drop parent (has child depending on it).
	_, errOut, code := run(t, "drop", "parent")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: parent has children") {
		t.Errorf("want 'has children' err; got:\n%s", errOut)
	}
}

// TestDrop_PassedConcept verifies that dropping a passed concept exits 1.
func TestDrop_PassedConcept(t *testing.T) {
	dir, _ := qSetupDir(t)
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "drop", "passed_c")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: passed_c is passed") {
		t.Errorf("want 'is passed' err; got:\n%s", errOut)
	}
}

// TestDrop_UnknownID verifies that dropping an unknown ID exits 3.
func TestDrop_UnknownID(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "drop", "nonexistent")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestDrop_QuestionID_NotDrifted verifies that dropping a question whose
// citation has not drifted exits 1 with "citation has not drifted".
func TestDrop_QuestionID_NotDrifted(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "drop", "q1")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "citation has not drifted") {
		t.Errorf("want 'citation has not drifted' err; got:\n%s", errOut)
	}
}

// TestDrop_HasQuestions verifies that dropping a concept with questions exits 1.
// Uses check_probe.mmd fixture (M6 q command not yet available).
func TestDrop_HasQuestions(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "drop", "mycon")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon has questions") {
		t.Errorf("want 'has questions' err; got:\n%s", errOut)
	}
}

// ── tm gap tests ──────────────────────────────────────────────────────────────

// TestGap_SetNew verifies that gap sets a GAP field on a concept with none,
// the graph round-trips, lints clean, and logs an event with before="" and
// after=gap.
func TestGap_SetNew(t *testing.T) {
	dir, _ := qSetupDir(t)

	file := newGraph(t, dir)

	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "My concept scope")
	if c1 != 0 {
		t.Fatalf("add: exit %d", c1)
	}

	out, errOut, code := run(t, "gap", "mycon", "missed the key term")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify GAP set in graph.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			if c.GAP != "missed the key term" {
				t.Errorf("GAP: want 'missed the key term', got %q", c.GAP)
			}
		}
	}

	// Round-trip check.
	roundTripped := graph.Write(g)
	if string(data) != string(roundTripped) {
		t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: find the "gap" event.
	rows := readEventLog(t, file)
	var gapRow map[string]any
	for _, r := range rows {
		if r["ev"] == "gap" {
			gapRow = r
			break
		}
	}
	if gapRow == nil {
		t.Fatal("no 'gap' event found in event log")
	}
	if gapRow["concept"] != "mycon" {
		t.Errorf("gap event concept: want 'mycon', got %v", gapRow["concept"])
	}
	if gapRow["before"] != "" {
		t.Errorf("gap event before: want '', got %v", gapRow["before"])
	}
	if gapRow["after"] != "missed the key term" {
		t.Errorf("gap event after: want 'missed the key term', got %v", gapRow["after"])
	}
}

// TestGap_ReplaceExisting verifies that gap replaces a previous GAP value,
// recording the correct before/after in the event log.
func TestGap_ReplaceExisting(t *testing.T) {
	dir, _ := qSetupDir(t)

	file := newGraph(t, dir)

	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")
	if c1 != 0 {
		t.Fatalf("add: exit %d", c1)
	}
	_, _, c2 := run(t, "gap", "mycon", "first gap")
	if c2 != 0 {
		t.Fatalf("first gap: exit %d", c2)
	}

	out, errOut, code := run(t, "gap", "mycon", "second gap")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify GAP updated.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			if c.GAP != "second gap" {
				t.Errorf("GAP: want 'second gap', got %q", c.GAP)
			}
		}
	}

	lintFile(t, file, dir)

	// Event log: find the SECOND gap event (before = "first gap").
	rows := readEventLog(t, file)
	var gapRow map[string]any
	for _, r := range rows {
		if r["ev"] == "gap" && r["before"] == "first gap" {
			gapRow = r
			break
		}
	}
	if gapRow == nil {
		t.Fatal("no 'gap' event with before='first gap' found")
	}
	if gapRow["after"] != "second gap" {
		t.Errorf("gap event after: want 'second gap', got %v", gapRow["after"])
	}
}

// TestGap_PassedConcept verifies that gap on a passed concept exits 1 with a
// reopen fix hint.
func TestGap_PassedConcept(t *testing.T) {
	dir, _ := qSetupDir(t)
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "gap", "passed_c", "some gap")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: passed_c is passed") {
		t.Errorf("want 'is passed' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen passed_c") {
		t.Errorf("want reopen fix hint; got:\n%s", errOut)
	}
}

// TestGap_UnknownID verifies that gap on an unknown ID exits 3.
func TestGap_UnknownID(t *testing.T) {
	dir, _ := qSetupDir(t)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "gap", "nonexistent", "some gap")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestGap_QuestionID verifies that gap on a question ID exits 1 with
// "is not a concept".
func TestGap_QuestionID(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "gap", "q1", "some gap")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 is not a concept") {
		t.Errorf("want 'not a concept' err; got:\n%s", errOut)
	}
}
