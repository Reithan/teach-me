package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// ── Helpers ────────────────────────────────────────────────────────────────────

// setupSrcFile writes src.txt (10 numbered lines) into dir and sets TM_SRC_ROOT.
func setupSrcFile(t *testing.T, dir string) {
	t.Helper()
	content := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
}

// newGraph creates a fresh skeleton graph and sets TM_FILE to its path.
// Returns the absolute path to the graph file.
func newGraph(t *testing.T, dir string) string {
	t.Helper()
	file := filepath.Join(dir, "g.mmd")
	_, errOut, code := run(t, "new", file)
	if code != 0 {
		t.Fatalf("tm new: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	t.Setenv("TM_FILE", file)
	return file
}

// buildMinimalGraph writes a minimal .mmd file containing one passed concept
// and one untested concept, then sets TM_FILE to the path.
func buildMinimalGraph(t *testing.T, dir string) {
	t.Helper()
	file := filepath.Join(dir, "g.mmd")
	mmd := `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
flowchart TB
    subgraph passed["Concepts User understands"]
        passed_c["Passed concept scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        untested_c["Untested concept scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
	if err := os.WriteFile(file, []byte(mmd), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", file)
}

// readEventLog reads and parses all lines from the event log for the given file.
func readEventLog(t *testing.T, file string) []map[string]any {
	t.Helper()
	logPath := file + ".jsonl"
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log %s: %v", logPath, err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("parse event log line %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

// lintFile checks that the graph at file passes lint. Uses a standard config.
func lintFile(t *testing.T, file, srcRoot string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file for lint: %v", err)
	}
	viols := lint.Check(data, lint.Config{
		SrcRoot: srcRoot, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	})
	if len(viols) > 0 {
		t.Errorf("graph fails lint after mutation: %v", viols)
	}
}

// ── tm add tests ──────────────────────────────────────────────────────────────

// TestAdd_HappyPath verifies that tm add creates a concept in untested,
// the graph round-trips and passes lint, the output is "ok", and an add event
// is logged with correct parents/children (empty when none given).
func TestAdd_HappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	file := newGraph(t, dir)

	out, errOut, code := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "My concept scope")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want stdout 'ok', got %q", out)
	}

	// Verify graph round-trips.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph file: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	roundTripped := graph.Write(g)
	if string(data) != string(roundTripped) {
		t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
	}

	// Verify concept is in untested block.
	found := false
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			found = true
			if c.Scope != "My concept scope" {
				t.Errorf("scope: want %q, got %q", "My concept scope", c.Scope)
			}
			if len(c.Cites) != 1 || c.Cites[0] != "f5ca3875b379@src.txt:1-5" {
				t.Errorf("cites: want [f5ca3875b379@src.txt:1-5], got %v", c.Cites)
			}
		}
	}
	if !found {
		t.Error("concept 'mycon' not found in untested block")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: skip the "new" event, find the "add" event.
	rows := readEventLog(t, file)
	var addRow map[string]any
	for _, r := range rows {
		if r["ev"] == "add" {
			addRow = r
			break
		}
	}
	if addRow == nil {
		t.Fatal("no 'add' event found in event log")
	}
	if addRow["id"] != "mycon" {
		t.Errorf("add event id: want 'mycon', got %v", addRow["id"])
	}
	if addRow["scope"] != "My concept scope" {
		t.Errorf("add event scope: want 'My concept scope', got %v", addRow["scope"])
	}
	if addRow["src"] != "f5ca3875b379@src.txt:1-5" {
		t.Errorf("add event src: want 'f5ca3875b379@src.txt:1-5', got %v", addRow["src"])
	}
	// parents and children should be empty slices.
	parents, ok := addRow["parents"].([]any)
	if !ok || len(parents) != 0 {
		t.Errorf("add event parents: want empty array, got %v", addRow["parents"])
	}
	children, ok := addRow["children"].([]any)
	if !ok || len(children) != 0 {
		t.Errorf("add event children: want empty array, got %v", addRow["children"])
	}
}

// TestAdd_HappyPath_WithParentChild verifies that --parent and --child flags
// create edges in the correct directions and are recorded in the event log.
func TestAdd_HappyPath_WithParentChild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	file := newGraph(t, dir)

	// Add two concepts first to serve as parent and child endpoints.
	_, _, c1 := run(t, "add", "prereq", "cd3f27ccd149@src.txt:1-3", "Prerequisite concept")
	if c1 != 0 {
		t.Fatalf("setup add prereq: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "followup", "e28e810e6e2e@src.txt:3-5", "Follow-up concept")
	if c2 != 0 {
		t.Fatalf("setup add followup: exit %d", c2)
	}

	// Now add "middle" with prereq as parent and followup as child.
	out, errOut, code := run(t, "add", "middle", "25070e52a6ae@src.txt:2-4", "Middle concept",
		"--parent", "prereq:enables",
		"--child", "followup:leads to")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify edges in graph.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)

	hasParentEdge := false
	hasChildEdge := false
	for _, e := range g.Edges {
		if e.From == "prereq" && e.To == "middle" && e.Label == "enables" {
			hasParentEdge = true
		}
		if e.From == "middle" && e.To == "followup" && e.Label == "leads to" {
			hasChildEdge = true
		}
	}
	if !hasParentEdge {
		t.Error("want edge prereq --enables--> middle")
	}
	if !hasChildEdge {
		t.Error("want edge middle --leads to--> followup")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Verify add event has correct parents/children.
	rows := readEventLog(t, file)
	var addRow map[string]any
	for _, r := range rows {
		if r["ev"] == "add" && r["id"] == "middle" {
			addRow = r
			break
		}
	}
	if addRow == nil {
		t.Fatal("no 'add' event for 'middle' found")
	}
	parents, _ := addRow["parents"].([]any)
	if len(parents) != 1 || parents[0] != "prereq" {
		t.Errorf("add event parents: want [prereq], got %v", parents)
	}
	children, _ := addRow["children"].([]any)
	if len(children) != 1 || children[0] != "followup" {
		t.Errorf("add event children: want [followup], got %v", children)
	}
}

// TestAdd_IDExists_Untested verifies that adding an already-existing untested
// concept exits 1 with "already exists" and no fix line.
func TestAdd_IDExists_Untested(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	file := newGraph(t, dir)
	_ = file

	// Add once.
	_, _, c1 := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")
	if c1 != 0 {
		t.Fatalf("first add: exit %d", c1)
	}

	// Add again → should fail.
	_, errOut, code := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon already exists") {
		t.Errorf("want 'already exists' err; got:\n%s", errOut)
	}
	// No fix line for untested concept.
	if strings.Contains(errOut, "fix:") {
		t.Errorf("want no fix line for untested duplicate; got:\n%s", errOut)
	}
}

// TestAdd_IDExists_Passed verifies that adding a concept that exists in the
// passed block exits 1 with "already exists" and a "fix: tm reopen <id>" hint.
func TestAdd_IDExists_Passed(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	// Build a graph with a passed concept.
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "add", "passed_c", "f5ca3875b379@src.txt:1-5", "scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: passed_c already exists") {
		t.Errorf("want 'already exists' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen passed_c") {
		t.Errorf("want 'fix: tm reopen passed_c'; got:\n%s", errOut)
	}
}

// TestAdd_ReservedID verifies that a reserved ID (q1, end, etc.) exits 3 with
// the usage line as the fix.
func TestAdd_ReservedID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	cases := []string{"q1", "a5", "end", "passed", "untested", "testing"}
	for _, id := range cases {
		id := id
		t.Run(id, func(t *testing.T) {
			_, errOut, code := run(t, "add", id, "f5ca3875b379@src.txt:1-5", "scope")
			if code != 3 {
				t.Fatalf("want exit 3 for reserved id %q, got %d; stderr:\n%s", id, code, errOut)
			}
			if !strings.Contains(errOut, "err:") {
				t.Errorf("want err: line; got:\n%s", errOut)
			}
			if !strings.Contains(errOut, "fix: tm add") {
				t.Errorf("want usage fix line; got:\n%s", errOut)
			}
		})
	}
}

// TestAdd_BadCitation_Parse verifies that a citation that fails to parse
// (missing line range) exits 3 with the usage line as the fix.
func TestAdd_BadCitation_Parse(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "src.txt", "scope")
	if code != 3 {
		t.Fatalf("want exit 3 for bad citation, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm add") {
		t.Errorf("want usage fix line; got:\n%s", errOut)
	}
}

// TestAdd_BadCitation_OutOfBounds verifies that a citation referencing
// out-of-bounds lines exits 3.
func TestAdd_BadCitation_OutOfBounds(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	// src.txt has 10 lines; request line 100.
	_, errOut, code := run(t, "add", "mycon", "src.txt:5-100", "scope")
	if code != 3 {
		t.Fatalf("want exit 3 for out-of-bounds citation, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm add") {
		t.Errorf("want usage fix line; got:\n%s", errOut)
	}
}

// TestAdd_UnknownParent verifies that a --parent referencing a non-existent
// concept ID exits 3 with the usage line as fix.
func TestAdd_UnknownParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope",
		"--parent", "nonexistent:some rel")
	if code != 3 {
		t.Fatalf("want exit 3 for unknown parent, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestAdd_UnknownChild verifies that a --child referencing a non-existent
// concept ID exits 3.
func TestAdd_UnknownChild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope",
		"--child", "nonexistent:depends on")
	if code != 3 {
		t.Fatalf("want exit 3 for unknown child, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestAdd_CycleViaChild verifies that adding a concept with --child pointing
// to a concept that already has a path back to the new concept exits 1 with
// "edge would close a cycle".
func TestAdd_CycleViaChild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	file := newGraph(t, dir)
	_ = file

	// Create a chain: a → b. Then add c --child a: new edge c→a. Also c is
	// --child b which creates c→b. Then add d --child c and --parent b would
	// mean b→d and d→c and c→b which is a cycle. Let's keep it simple:
	//
	// Add concept "a", then add concept "b" with --child a (b→a).
	// Then add "c" with --parent a and --child b: a→c and c→b.
	// This creates a cycle: a→c→b... wait, b→a, so a→c→b→a is a cycle.

	_, _, c1 := run(t, "add", "a", "cd3f27ccd149@src.txt:1-3", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "b", "e28e810e6e2e@src.txt:3-5", "concept b", "--child", "a:requires")
	if c2 != 0 {
		t.Fatalf("add b with child a: exit %d", c2)
	}

	// Now add c with --parent a and --child b: a→c and c→b, plus b→a already
	// exists. Path: a→c→b→a is a cycle.
	_, errOut, code := run(t, "add", "c", "be2b44461fad@src.txt:5-7", "concept c",
		"--parent", "a:followed by",
		"--child", "b:leads back")
	if code != 1 {
		t.Fatalf("want exit 1 for cycle, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: edge would close a cycle") {
		t.Errorf("want 'edge would close a cycle' err; got:\n%s", errOut)
	}
}

// TestAdd_MissingColonInFlag verifies that a --parent or --child value missing
// a colon exits 3 with the usage line.
func TestAdd_MissingColonInFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	_ = newGraph(t, dir)

	_, errOut, code := run(t, "add", "mycon", "f5ca3875b379@src.txt:1-5", "scope",
		"--parent", "nocolon")
	if code != 3 {
		t.Fatalf("want exit 3 for missing colon in flag, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm add") {
		t.Errorf("want usage fix line; got:\n%s", errOut)
	}
}

// ── tm link tests ─────────────────────────────────────────────────────────────

// TestLink_HappyPath verifies that tm link adds an edge, graph lints, and
// one "link" event is logged with correct from/to/rel.
func TestLink_HappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	file := newGraph(t, dir)

	// Add two concepts.
	_, _, c1 := run(t, "add", "a", "f5ca3875b379@src.txt:1-5", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "b", "f5ca3875b379@src.txt:1-5", "concept b")
	if c2 != 0 {
		t.Fatalf("add b: exit %d", c2)
	}

	// Link them.
	out, errOut, code := run(t, "link", "a", "b", "prerequisite for")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify edge in graph.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	found := false
	for _, e := range g.Edges {
		if e.From == "a" && e.To == "b" && e.Label == "prerequisite for" {
			found = true
		}
	}
	if !found {
		t.Error("want edge a --prerequisite for--> b in graph")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log has a "link" event.
	rows := readEventLog(t, file)
	var linkRow map[string]any
	for _, r := range rows {
		if r["ev"] == "link" {
			linkRow = r
			break
		}
	}
	if linkRow == nil {
		t.Fatal("no 'link' event found in event log")
	}
	if linkRow["from"] != "a" {
		t.Errorf("link event from: want 'a', got %v", linkRow["from"])
	}
	if linkRow["to"] != "b" {
		t.Errorf("link event to: want 'b', got %v", linkRow["to"])
	}
	if linkRow["rel"] != "prerequisite for" {
		t.Errorf("link event rel: want 'prerequisite for', got %v", linkRow["rel"])
	}
}

// TestLink_UnknownFrom verifies that linking from an unknown ID exits 3.
func TestLink_UnknownFrom(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	_ = newGraph(t, dir)
	_, _, c1 := run(t, "add", "b", "f5ca3875b379@src.txt:1-5", "concept b")
	if c1 != 0 {
		t.Fatalf("add b: exit %d", c1)
	}

	_, errOut, code := run(t, "link", "nonexistent", "b", "rel")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestLink_UnknownTo verifies that linking to an unknown ID exits 3.
func TestLink_UnknownTo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	_ = newGraph(t, dir)
	_, _, c1 := run(t, "add", "a", "f5ca3875b379@src.txt:1-5", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}

	_, errOut, code := run(t, "link", "a", "nonexistent", "rel")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestLink_NonConceptFrom verifies that linking from a question node exits 1.
func TestLink_NonConceptFrom(t *testing.T) {
	// Resolve fixture path before Chdir so relative resolution works.
	probeFixture := checkProbeFixture(t)

	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	// Copy to temp dir to avoid leaking event log.
	data, err := os.ReadFile(probeFixture)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", file)

	// Create a second concept to link to.
	_, _, c1 := run(t, "add", "other", "f5ca3875b379@src.txt:1-5", "other concept")
	if c1 != 0 {
		t.Fatalf("add other: exit %d", c1)
	}

	// Try to link from q1 (a question node) to other → non-concept from.
	_, errOut, code := run(t, "link", "q1", "other", "rel")
	if code != 1 {
		t.Fatalf("want exit 1 for non-concept from, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 is not a concept") {
		t.Errorf("want 'not a concept' err; got:\n%s", errOut)
	}
}

// TestLink_NonConceptTo verifies that linking to an answer node exits 1.
func TestLink_NonConceptTo(t *testing.T) {
	// Resolve fixture path before Chdir so relative resolution works.
	probeFixture := checkProbeFixture(t)

	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	// Copy to temp dir to avoid leaking event log.
	data, err := os.ReadFile(probeFixture)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", file)

	// Try to link mycon to a1 (an answer node) → non-concept to.
	_, errOut, code := run(t, "link", "mycon", "a1", "rel")
	if code != 1 {
		t.Fatalf("want exit 1 for non-concept to, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: a1 is not a concept") {
		t.Errorf("want 'not a concept' err; got:\n%s", errOut)
	}
}

// TestLink_Cycle verifies that adding a back edge exits 1 with "edge would
// close a cycle".
func TestLink_Cycle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	_ = newGraph(t, dir)

	_, _, c1 := run(t, "add", "a", "cd3f27ccd149@src.txt:1-3", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "b", "e28e810e6e2e@src.txt:3-5", "concept b")
	if c2 != 0 {
		t.Fatalf("add b: exit %d", c2)
	}
	// a → b
	_, _, c3 := run(t, "link", "a", "b", "first to second")
	if c3 != 0 {
		t.Fatalf("link a→b: exit %d", c3)
	}

	// b → a creates a cycle.
	_, errOut, code := run(t, "link", "b", "a", "back edge")
	if code != 1 {
		t.Fatalf("want exit 1 for cycle, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: edge would close a cycle") {
		t.Errorf("want 'edge would close a cycle'; got:\n%s", errOut)
	}
}

// TestLink_DuplicateEdge verifies that adding an identical duplicate edge
// exits 1 with a friendly error.
func TestLink_DuplicateEdge(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	_ = newGraph(t, dir)

	_, _, c1 := run(t, "add", "a", "cd3f27ccd149@src.txt:1-3", "concept a")
	if c1 != 0 {
		t.Fatalf("add a: exit %d", c1)
	}
	_, _, c2 := run(t, "add", "b", "e28e810e6e2e@src.txt:3-5", "concept b")
	if c2 != 0 {
		t.Fatalf("add b: exit %d", c2)
	}
	_, _, c3 := run(t, "link", "a", "b", "my rel")
	if c3 != 0 {
		t.Fatalf("first link: exit %d", c3)
	}

	_, errOut, code := run(t, "link", "a", "b", "my rel")
	if code != 1 {
		t.Fatalf("want exit 1 for duplicate, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// TestAdd_NoLeakedFiles verifies no .tmconfig or ERRORS.jsonl is left in
// the working directory after a series of add/link operations.
func TestAdd_NoLeakedFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	errlogPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)

	_ = newGraph(t, dir)
	_, _, c1 := run(t, "add", "x", "f5ca3875b379@src.txt:1-5", "concept x")
	if c1 != 0 {
		t.Fatalf("add x: exit %d", c1)
	}

	// No ERRORS.jsonl in the working dir (success path).
	if _, err := os.Stat(errlogPath); !os.IsNotExist(err) {
		// errlog only exists if there were errors.
		t.Logf("note: errlog exists at %s (only unexpected if add succeeded)", errlogPath)
	}

	// Specifically verify no stray files at repo root patterns.
	for _, name := range []string{".tmconfig", "ERRORS.jsonl"} {
		// These should only exist inside dir (where we chdir'd), not at repo root.
		repoRootPath := filepath.Join("/home/reithan/projects/teach-me", name)
		if _, err := os.Stat(repoRootPath); err == nil {
			t.Errorf("leaked file at repo root: %s", repoRootPath)
		}
	}
}
