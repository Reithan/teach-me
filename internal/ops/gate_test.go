package ops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// gatedGraph is a minimal graph where concept "con" has one failed probe batch
// (probe_1 with q1/a1 class fail). With MaxFails=1, "con" is gated.
const gatedGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        con --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// gatedStall is a minimal graph where concept "con" has stall streak = 1 and
// MaxStall=1, making it gated via stall.
const gatedStall = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        q2["teach scope<br/>src.txt:1-5"]:::teach_2
        a2["fail answer"]:::fail
        con --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// setupGatedFile writes content to dir/g.mmd and returns the path.
func setupGatedFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setupGatedFile: %v", err)
	}
	return path
}

// TestClearGate_RoundTrip verifies that ClearGate writes the gate meta line
// correctly and that graph.Write + graph.Parse round-trips it to the same
// GateMeta value.
func TestClearGate_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := setupGatedFile(t, dir, gatedGraph)

	sCfg := state.Config{
		MaxFails: 1, MaxStall: 4, MaxTeach: 8,
		ProbeMin: 1, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
		SrcRoot: dir,
	}
	s, err := state.Load(path, sCfg)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	g := s.Graph()

	// Verify pre-condition: con is gated.
	cs := s.ConceptStatus("con")
	if !cs.Gated {
		t.Fatal("want con to be gated before ClearGate")
	}

	newG, row, ok := ops.ClearGate(g, s, "con", "add", "")
	if !ok {
		t.Fatal("ClearGate returned ok=false but con is gated")
	}

	// Check row fields.
	if row.Ev != "gate" {
		t.Errorf("row ev: want 'gate', got %v", row.Ev)
	}
	if row.Fields["concept"] != "con" {
		t.Errorf("row concept: want 'con', got %v", row.Fields["concept"])
	}
	if row.Fields["via"] != "add" {
		t.Errorf("row via: want 'add', got %v", row.Fields["via"])
	}
	if row.Fields["trip"] != "probes" {
		t.Errorf("row trip: want 'probes', got %v", row.Fields["trip"])
	}
	wantBase := 1 // max BatchN of probe_1 = 1
	if row.Fields["base"] != wantBase {
		t.Errorf("row base: want %d, got %v", wantBase, row.Fields["base"])
	}

	// Round-trip: Write → Parse → check UntestedMetas.
	outBytes := graph.Write(newG)
	parsed, parseErr := graph.Parse(outBytes)
	if parseErr != nil {
		t.Fatalf("graph.Parse after ClearGate: %v", parseErr)
	}
	if len(parsed.UntestedMetas) != 1 {
		t.Fatalf("want 1 gate meta, got %d", len(parsed.UntestedMetas))
	}
	meta := parsed.UntestedMetas[0]
	if meta.Concept != "con" {
		t.Errorf("meta.Concept: want 'con', got %q", meta.Concept)
	}
	if meta.Base != wantBase {
		t.Errorf("meta.Base: want %d, got %d", wantBase, meta.Base)
	}
}

// TestClearGate_NotGated verifies that ClearGate returns ok=false when the
// concept is not gated.
func TestClearGate_NotGated(t *testing.T) {
	dir := t.TempDir()
	// Use minimalGraph (no probe batches → not gated).
	path := setupGatedFile(t, dir, minimalGraph)

	sCfg := state.Config{
		MaxFails: 1, MaxStall: 4, MaxTeach: 8,
		ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3,
		SrcRoot: dir,
	}
	s, err := state.Load(path, sCfg)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	g := s.Graph()

	_, _, ok := ops.ClearGate(g, s, "c1", "add", "")
	if ok {
		t.Error("ClearGate returned ok=true for a non-gated concept")
	}
}

// TestClearGate_TripProbes verifies that trip="probes" is set when
// FailedProbeBatches >= MaxFails.
func TestClearGate_TripProbes(t *testing.T) {
	dir := t.TempDir()
	path := setupGatedFile(t, dir, gatedGraph)

	sCfg := state.Config{MaxFails: 1, MaxStall: 4, MaxTeach: 8, ProbeMin: 1, ProbeMax: 5, TeachMin: 1, TeachMax: 3}
	s, err := state.Load(path, sCfg)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	_, row, ok := ops.ClearGate(s.Graph(), s, "con", "reopen", "")
	if !ok {
		t.Fatal("want ok=true")
	}
	if row.Fields["trip"] != "probes" {
		t.Errorf("want trip='probes', got %v", row.Fields["trip"])
	}
}

// TestClearGate_TripStall verifies that trip="stall" is set when gated by
// stall (not failed probe batches).
func TestClearGate_TripStall(t *testing.T) {
	dir := t.TempDir()
	path := setupGatedFile(t, dir, gatedStall)

	// MaxFails=2 so probe gate doesn't fire (only 1 failed probe batch);
	// MaxStall=1 so the stall streak of 1 teach question suffices.
	sCfg := state.Config{MaxFails: 2, MaxStall: 1, MaxTeach: 8, ProbeMin: 1, ProbeMax: 5, TeachMin: 1, TeachMax: 3}
	s, err := state.Load(path, sCfg)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	cs := s.ConceptStatus("con")
	if !cs.Gated {
		t.Fatalf("want con gated; FailedProbeBatches=%v Stalled=%v", cs.FailedProbeBatches, cs.Stalled)
	}
	_, row, ok := ops.ClearGate(s.Graph(), s, "con", "override", "reason")
	if !ok {
		t.Fatal("want ok=true")
	}
	if row.Fields["trip"] != "stall" {
		t.Errorf("want trip='stall', got %v", row.Fields["trip"])
	}
	if row.Fields["via"] != "override" {
		t.Errorf("want via='override', got %v", row.Fields["via"])
	}
	if row.Fields["reason"] != "reason" {
		t.Errorf("want reason='reason', got %v", row.Fields["reason"])
	}
}

// TestClearGate_ReplacesExistingMeta verifies that ClearGate replaces an
// existing gate meta line for the same concept rather than appending a new one.
func TestClearGate_ReplacesExistingMeta(t *testing.T) {
	dir := t.TempDir()
	path := setupGatedFile(t, dir, gatedGraph)

	sCfg := state.Config{MaxFails: 1, MaxStall: 4, MaxTeach: 8, ProbeMin: 1, ProbeMax: 5, TeachMin: 1, TeachMax: 3}
	s, err := state.Load(path, sCfg)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}

	// First clear: produces a new meta.
	g1, _, ok := ops.ClearGate(s.Graph(), s, "con", "add", "")
	if !ok {
		t.Fatal("want ok=true on first clear")
	}
	if len(g1.UntestedMetas) != 1 {
		t.Fatalf("want 1 meta after first clear, got %d", len(g1.UntestedMetas))
	}

	// Second clear on g1 (which already has the meta): should replace, not append.
	g2, _, ok2 := ops.ClearGate(g1, s, "con", "override", "r")
	if !ok2 {
		t.Fatal("want ok=true on second clear")
	}
	if len(g2.UntestedMetas) != 1 {
		t.Errorf("want exactly 1 meta after second clear, got %d", len(g2.UntestedMetas))
	}
}
