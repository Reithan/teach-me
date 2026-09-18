package ops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
	"github.com/reithan/teach-me/internal/lockfile"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// fixedClock is a test clock returning a fixed time.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// minimalGraph returns the bytes of a minimal, lint-clean graph in dir.
// The concept has no citations so lint rule 11 is not triggered.
const minimalGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        c1["Concept one"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// setupGraph writes minimalGraph to dir/g.mmd and returns the path.
func setupGraph(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(path, []byte(minimalGraph), 0o644); err != nil {
		t.Fatalf("setupGraph: %v", err)
	}
	return path
}

// cfgs returns test-friendly state.Config and lint.Config for the given dir.
func cfgs(dir string) (state.Config, lint.Config) {
	sCfg := state.Config{
		ProbeMin: 2, ProbeMax: 5,
		TeachMin: 1, TeachMax: 3,
		MaxFails: 2, MaxTeach: 8, MaxStall: 4,
		SrcRoot: dir,
	}
	lCfg := lint.Config{
		SrcRoot: dir, ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
	}
	return sCfg, lCfg
}

func TestMutate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	t.Setenv("TM_ROLE", "")

	graphFile := setupGraph(t, dir)
	clk := fixedClock{t: time.Now()}
	sCfg, lCfg := cfgs(dir)

	// Apply adds a second concept c2 to the untested block.
	called := false
	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		called = true
		// Add c2 to the untested block.
		g.UntestedConcepts = append(g.UntestedConcepts, &graph.ConceptNode{
			ID:    "c2",
			Scope: "Concept two",
			Block: graph.BlockUntested,
		})
		rows := []eventlog.Row{
			eventlog.NewRow("add", map[string]any{"id": "c2", "scope": "Concept two"}),
		}
		return g, rows, nil
	}

	rows, refusal, err := ops.Mutate(graphFile, sCfg, lCfg, clk, apply)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if refusal != nil {
		t.Fatalf("unexpected refusal: %v", refusal)
	}
	if !called {
		t.Fatal("apply was not called")
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}

	// Graph file must contain c2.
	data, err := os.ReadFile(graphFile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if !strings.Contains(string(data), `c2["Concept two"]`) {
		t.Errorf("graph does not contain c2 after mutation:\n%s", data)
	}

	// Event log must contain one line.
	logPath := eventlog.Path(graphFile)
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(logData), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 log line, got %d: %s", len(lines), logData)
	}

	// No temp file should remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestMutate_RefusalShortCircuits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	t.Setenv("TM_ROLE", "")

	graphFile := setupGraph(t, dir)
	original, _ := os.ReadFile(graphFile)

	clk := fixedClock{t: time.Now()}
	sCfg, lCfg := cfgs(dir)

	apply := func(_ *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		return nil, nil, &ops.Refusal{Err: "not allowed", Exit: 1}
	}

	rows, refusal, err := ops.Mutate(graphFile, sCfg, lCfg, clk, apply)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refusal == nil {
		t.Fatal("expected refusal, got nil")
	}
	if refusal.Err != "not allowed" {
		t.Errorf("refusal.Err = %q, want %q", refusal.Err, "not allowed")
	}
	if refusal.Exit != 1 {
		t.Errorf("refusal.Exit = %d, want 1", refusal.Exit)
	}
	if rows != nil {
		t.Errorf("expected nil rows on refusal, got %v", rows)
	}

	// File must be unchanged.
	after, _ := os.ReadFile(graphFile)
	if string(after) != string(original) {
		t.Errorf("graph was modified despite refusal")
	}

	// No event log should exist.
	if _, err := os.Stat(eventlog.Path(graphFile)); !os.IsNotExist(err) {
		t.Errorf("event log should not exist after a refusal")
	}
}

func TestMutate_PreExistingLintFailureRefusesExit1(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	t.Setenv("TM_ROLE", "")

	// Write a graph that fails lint (missing classDef lines, bad format).
	graphFile := filepath.Join(dir, "bad.mmd")
	if err := os.WriteFile(graphFile, []byte("not a valid graph\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	clk := fixedClock{t: time.Now()}
	sCfg, lCfg := cfgs(dir)

	applyCalled := false
	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		applyCalled = true
		return g, nil, nil
	}

	_, refusal, err := ops.Mutate(graphFile, sCfg, lCfg, clk, apply)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refusal == nil {
		t.Fatal("expected refusal for lint-failing graph, got nil")
	}
	if refusal.Exit != 1 {
		t.Errorf("refusal.Exit = %d, want 1", refusal.Exit)
	}
	if !strings.Contains(refusal.Err, "lint") {
		t.Errorf("refusal.Err should mention lint, got: %s", refusal.Err)
	}
	if applyCalled {
		t.Error("apply should not be called when current graph fails lint")
	}
}

func TestMutate_LockHeldDuringApply(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	t.Setenv("TM_ROLE", "")

	graphFile := setupGraph(t, dir)
	clk := fixedClock{t: time.Now()}
	sCfg, lCfg := cfgs(dir)

	lockObserved := false
	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Lock file must exist while apply runs.
		lockPath := lockfile.LockPath(graphFile)
		if _, err := os.Stat(lockPath); err == nil {
			lockObserved = true
		}
		return g, nil, nil
	}

	_, _, err := ops.Mutate(graphFile, sCfg, lCfg, clk, apply)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if !lockObserved {
		t.Error("lock file was not present during apply")
	}

	// Lock file must be gone after Mutate returns.
	if _, err := os.Stat(lockfile.LockPath(graphFile)); !os.IsNotExist(err) {
		t.Error("lock file still present after Mutate returned")
	}
}

func TestMutate_MultipleEventRows(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	t.Setenv("TM_ROLE", "")

	graphFile := setupGraph(t, dir)
	clk := fixedClock{t: time.Now()}
	sCfg, lCfg := cfgs(dir)

	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		rows := []eventlog.Row{
			eventlog.NewRow("add", map[string]any{"id": "c2"}),
			eventlog.NewRow("link", map[string]any{"from": "c1", "to": "c2", "rel": "requires"}),
		}
		// Add c2 and an edge to keep the output valid.
		g.UntestedConcepts = append(g.UntestedConcepts, &graph.ConceptNode{
			ID: "c2", Scope: "C2", Block: graph.BlockUntested,
		})
		g.Edges = append(g.Edges, &graph.Edge{From: "c1", To: "c2", Label: "requires"})
		return g, rows, nil
	}

	rows, refusal, err := ops.Mutate(graphFile, sCfg, lCfg, clk, apply)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if refusal != nil {
		t.Fatalf("unexpected refusal: %v", refusal)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	logData, _ := os.ReadFile(eventlog.Path(graphFile))
	logLines := strings.Split(strings.TrimRight(string(logData), "\n"), "\n")
	if len(logLines) != 2 {
		t.Errorf("expected 2 event log lines, got %d: %s", len(logLines), logData)
	}
}
