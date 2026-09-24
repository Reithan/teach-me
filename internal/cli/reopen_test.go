package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// buildPassedDescendantGraph writes a .mmd with two passed concepts where
// prereq --"required by"--> dependent, then sets TM_FILE to the path.
// After reopening prereq the edge becomes untested→passed.
func buildPassedDescendantGraph(t *testing.T, dir string) string {
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
        prereq["Prereq concept<br/>f5ca3875b379@src.txt:1-5"]
        dependent["Dependent concept<br/>f5ca3875b379@src.txt:1-5"]
        prereq --"required by"--> dependent
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
	if err := os.WriteFile(file, []byte(mmd), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", file)
	return file
}

// ── tm reopen tests ────────────────────────────────────────────────────────────

// TestReopen_HappyPath verifies that reopen moves a passed concept to the top
// of untested with the given GAP, round-trips, lints clean, and logs a
// "reopen" event with {concept, gap}.
func TestReopen_HappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	buildMinimalGraph(t, dir)
	file := filepath.Join(dir, "g.mmd")

	out, errOut, code := run(t, "reopen", "passed_c", "missed the key property")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want stdout 'ok', got %q", out)
	}

	// Verify concept moved to untested with GAP.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	// Must NOT be in passed.
	for _, c := range g.PassedConcepts {
		if c.ID == "passed_c" {
			t.Error("passed_c should be removed from passed block")
		}
	}

	// Must be at top of untested with correct GAP.
	if len(g.UntestedConcepts) == 0 {
		t.Fatal("untested block is empty after reopen")
	}
	top := g.UntestedConcepts[0]
	if top.ID != "passed_c" {
		t.Errorf("want passed_c at top of untested, got %q", top.ID)
	}
	if top.GAP != "missed the key property" {
		t.Errorf("GAP: want 'missed the key property', got %q", top.GAP)
	}
	if top.Block != graph.BlockUntested {
		t.Errorf("Block: want BlockUntested, got %v", top.Block)
	}

	// Round-trip check.
	roundTripped := graph.Write(g)
	if string(data) != string(roundTripped) {
		t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", data, roundTripped)
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: find the "reopen" event.
	rows := readEventLog(t, file)
	var reopenRow map[string]any
	for _, r := range rows {
		if r["ev"] == "reopen" {
			reopenRow = r
			break
		}
	}
	if reopenRow == nil {
		t.Fatal("no 'reopen' event found in event log")
	}
	if reopenRow["concept"] != "passed_c" {
		t.Errorf("reopen event concept: want 'passed_c', got %v", reopenRow["concept"])
	}
	if reopenRow["gap"] != "missed the key property" {
		t.Errorf("reopen event gap: want 'missed the key property', got %v", reopenRow["gap"])
	}
}

// TestReopen_PrependToUntested verifies that the reopened concept is prepended
// to the front of the untested list, not appended.
func TestReopen_PrependToUntested(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	buildMinimalGraph(t, dir)
	file := filepath.Join(dir, "g.mmd")

	// buildMinimalGraph has untested_c already in untested.
	_, _, c := run(t, "reopen", "passed_c", "some gap")
	if c != 0 {
		t.Fatalf("reopen: exit %d", c)
	}

	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)

	if len(g.UntestedConcepts) < 2 {
		t.Fatalf("want at least 2 untested concepts, got %d", len(g.UntestedConcepts))
	}
	if g.UntestedConcepts[0].ID != "passed_c" {
		t.Errorf("want passed_c at index 0, got %q", g.UntestedConcepts[0].ID)
	}
	if g.UntestedConcepts[1].ID != "untested_c" {
		t.Errorf("want untested_c at index 1, got %q", g.UntestedConcepts[1].ID)
	}
}

// TestReopen_PassedDescendant verifies that reopening a concept whose
// descendant remains in passed produces a lint-clean graph (§spec: descendants
// stay passed).
func TestReopen_PassedDescendant(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	file := buildPassedDescendantGraph(t, dir)

	// Reopen prereq; dependent stays in passed.
	_, errOut, code := run(t, "reopen", "prereq", "missed the prerequisite concept")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	// prereq must be in untested.
	foundUntested := false
	for _, c := range g.UntestedConcepts {
		if c.ID == "prereq" {
			foundUntested = true
		}
	}
	if !foundUntested {
		t.Error("prereq should be in untested after reopen")
	}

	// dependent must still be in passed.
	foundPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "dependent" {
			foundPassed = true
		}
	}
	if !foundPassed {
		t.Error("dependent should remain in passed after reopening prereq")
	}

	// Lint must be clean: untested-prereq → passed-dependent is valid.
	viols := lint.Check(data, lint.Config{
		SrcRoot: dir, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	})
	if len(viols) > 0 {
		t.Errorf("graph with passed descendant fails lint: %v", viols)
	}
}

// TestReopen_UnknownID verifies that reopening an unknown ID exits 3 with a
// fix hint pointing at the usage line.
func TestReopen_UnknownID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "reopen", "nonexistent_id", "some gap")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen") {
		t.Errorf("want fix: usage line; got:\n%s", errOut)
	}
}

// TestReopen_UntestedConcept verifies that reopening an untested concept exits
// 1 with "is not passed".
func TestReopen_UntestedConcept(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	buildMinimalGraph(t, dir)

	_, errOut, code := run(t, "reopen", "untested_c", "some gap")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: untested_c is not passed") {
		t.Errorf("want 'is not passed' err; got:\n%s", errOut)
	}
}

// TestReopen_QuestionID verifies that reopening a question ID exits 1 with
// "is not a concept".
func TestReopen_QuestionID(t *testing.T) {
	probeFixture := checkProbeFixture(t) // must resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)

	_, errOut, code := run(t, "reopen", "q1", "some gap")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 is not a concept") {
		t.Errorf("want 'not a concept' err; got:\n%s", errOut)
	}
}
