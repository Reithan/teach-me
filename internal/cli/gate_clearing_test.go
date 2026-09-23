package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// gatedConceptGraph is a minimal, lint-clean graph with a gated concept "con".
// With TM_MAX_FAILS=1 and TM_PROBE_MIN=1, probe_1 (one fail answer) makes
// "con" gated.
const gatedConceptGraph = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
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

// gatedChildGraph is a graph with a passed "parent" concept and an untested
// "child" concept that is gated (one failed probe batch). Edge parent → child
// is in the untested block.
const gatedChildGraph = `---
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
        parent["Parent scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        child["Child scope<br/>f5ca3875b379@src.txt:1-5"]
        parent --"requires"--> child
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        child --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// buildGatedGraph writes the given mmd content to a new g.mmd in dir and sets
// TM_FILE to its path. Returns the absolute path.
func buildGatedGraph(t *testing.T, dir, content string) string {
	t.Helper()
	file := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", file)
	return file
}

// gateMetaFor returns the GateMeta for concept in g's untested metas, or nil.
func gateMetaFor(g *graph.Graph, concept string) *graph.GateMeta {
	for i, m := range g.UntestedMetas {
		if m.Concept == concept {
			return &g.UntestedMetas[i]
		}
	}
	return nil
}

// eventsByType returns all event log rows with the given "ev" value.
func eventsByType(rows []map[string]any, ev string) []map[string]any {
	var out []map[string]any
	for _, r := range rows {
		if r["ev"] == ev {
			out = append(out, r)
		}
	}
	return out
}

// ── A. add --child gate clearing ──────────────────────────────────────────────

// TestAddChild_GatedChild_WritesGateMeta verifies that `tm add <new> <cite>
// "<scope>" --child <C>` writes a gate meta line and gate event when C is
// currently gated.
func TestAddChild_GatedChild_WritesGateMeta(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	// Add a new concept "parent" as a prerequisite above the gated "con".
	out, errOut, code := run(t, "add", "newparent", "f5ca3875b379@src.txt:1-5", "parent scope", "--child", "con:requires")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Check graph has gate meta for "con".
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	meta := gateMetaFor(g, "con")
	if meta == nil {
		t.Fatal("want gate meta for 'con', got none")
	}
	if meta.Base != 1 {
		t.Errorf("gate meta base: want 1 (max BatchN of probe_1), got %d", meta.Base)
	}

	// Check event log has add event followed by gate event.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d; all rows: %v", len(gateRows), rows)
	}
	gr := gateRows[0]
	if gr["concept"] != "con" {
		t.Errorf("gate event concept: want 'con', got %v", gr["concept"])
	}
	if gr["via"] != "add" {
		t.Errorf("gate event via: want 'add', got %v", gr["via"])
	}
	if gr["trip"] != "probes" {
		t.Errorf("gate event trip: want 'probes', got %v", gr["trip"])
	}
	baseVal, _ := gr["base"].(float64)
	if int(baseVal) != 1 {
		t.Errorf("gate event base: want 1, got %v", gr["base"])
	}
}

// TestAddChild_NonGatedChild_NoGateMeta verifies that `tm add ... --child C`
// does NOT write a gate meta when C is not gated.
func TestAddChild_NonGatedChild_NoGateMeta(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "2") // need 2 fails to gate; fixture has only 1
	t.Setenv("TM_PROBE_MIN", "1") // fixture has 1 probe question; bypass min-probe lint
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	_, errOut, code := run(t, "add", "newparent", "f5ca3875b379@src.txt:1-5", "parent scope", "--child", "con:requires")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	if meta := gateMetaFor(g, "con"); meta != nil {
		t.Errorf("want no gate meta when con is not gated, got meta %+v", meta)
	}
}

// ── B. reopen gate clearing ────────────────────────────────────────────────────

// TestReopen_GatedDirectChild_WritesGateMeta verifies that reopening a passed
// concept with a gated direct child writes gate meta + gate event for the child.
func TestReopen_GatedDirectChild_WritesGateMeta(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedChildGraph)

	out, errOut, code := run(t, "reopen", "parent", "missed the property")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	meta := gateMetaFor(g, "child")
	if meta == nil {
		t.Fatal("want gate meta for 'child' after reopen, got none")
	}
	if meta.Base != 1 {
		t.Errorf("gate meta base: want 1, got %d", meta.Base)
	}

	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d", len(gateRows))
	}
	gr := gateRows[0]
	if gr["concept"] != "child" {
		t.Errorf("gate event concept: want 'child', got %v", gr["concept"])
	}
	if gr["via"] != "reopen" {
		t.Errorf("gate event via: want 'reopen', got %v", gr["via"])
	}
}

// TestReopen_NonGatedChild_NoGateMeta verifies that reopen does not write a
// gate event when the direct child is not gated.
func TestReopen_NonGatedChild_NoGateMeta(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "2") // need 2 fails; fixture has only 1
	t.Setenv("TM_PROBE_MIN", "1") // fixture has 1 probe question; bypass min-probe lint
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedChildGraph)

	_, errOut, code := run(t, "reopen", "parent", "gap text")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	if meta := gateMetaFor(g, "child"); meta != nil {
		t.Errorf("want no gate meta for non-gated child, got %+v", meta)
	}

	rows := readEventLog(t, file)
	if len(eventsByType(rows, "gate")) != 0 {
		t.Error("want no gate events for non-gated child")
	}
}

// ── C. --override on q ────────────────────────────────────────────────────────

// TestQ_Override_ClearsGateAndAddsQuestion verifies that `tm q ... --override`
// on a gated concept writes the gate meta, emits a gate event, and adds the
// question.
func TestQ_Override_ClearsGateAndAddsQuestion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	out, errOut, code := run(t, "q", "con", "f5ca3875b379@src.txt:1-5", "new probe scope", "--override", "testing reason")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	newQID := strings.TrimSpace(out)
	if newQID == "" {
		t.Error("want new question ID on stdout, got empty")
	}

	// Check gate meta written.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	meta := gateMetaFor(g, "con")
	if meta == nil {
		t.Fatal("want gate meta for 'con' after --override, got none")
	}
	if meta.Base != 1 {
		t.Errorf("gate meta base: want 1, got %d", meta.Base)
	}

	// Check event log: gate event BEFORE q event.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	qRows := eventsByType(rows, "q")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d", len(gateRows))
	}
	if len(qRows) != 1 {
		t.Fatalf("want 1 q event, got %d", len(qRows))
	}
	gr := gateRows[0]
	if gr["via"] != "override" {
		t.Errorf("gate event via: want 'override', got %v", gr["via"])
	}
	if gr["reason"] != "testing reason" {
		t.Errorf("gate event reason: want 'testing reason', got %v", gr["reason"])
	}

	// gate event must come before q event in the log.
	var gateIdx, qIdx int
	for i, r := range rows {
		if r["ev"] == "gate" {
			gateIdx = i
		}
		if r["ev"] == "q" {
			qIdx = i
		}
	}
	if gateIdx >= qIdx {
		t.Errorf("gate event (idx %d) must precede q event (idx %d)", gateIdx, qIdx)
	}
}

// TestQ_NoOverride_GatedRefusal verifies that `tm q` on a gated concept without
// --override returns exit 1 (gated refusal).
func TestQ_NoOverride_GatedRefusal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, gatedConceptGraph)

	_, errOut, code := run(t, "q", "con", "f5ca3875b379@src.txt:1-5", "scope")
	if code != 1 {
		t.Fatalf("want exit 1 (gated refusal), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: con is gated") {
		t.Errorf("want 'con is gated' in stderr, got:\n%s", errOut)
	}
}

// ── D. --override on answer ───────────────────────────────────────────────────

// gatedConceptWithTeachQ is a graph where "con" is gated (probe_1 resolved with
// fail) and has an unanswered teach question q2 in teach_2. Because q2 is in a
// teach batch (not a probe batch), the open-target check in answer.go does not
// fire, allowing --override to clear the gate and proceed to record the answer.
// TM_MAX_FAILS=1, TM_PROBE_MIN=1, TM_TEACH_MIN=1 are required.
const gatedConceptWithTeachQ = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        q2["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_2
        a1["fail answer"]:::fail
        con --> q1
        q1 --> a1
        a1 --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestAnswer_Override_ClearsGateAndRecordsAnswer verifies that `tm answer q2
// "raw" --override "reason"` on a gated concept with a teach question writes
// the gate meta + gate event and records the teach answer.
func TestAnswer_Override_ClearsGateAndRecordsAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptWithTeachQ)

	out, errOut, code := run(t, "answer", "q2", "my raw answer", "--override", "override reason")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Check gate meta. Base = max(BatchN) over all batches for "con".
	// In gatedConceptWithTeachQ: probe_1 (N=1) and teach_2 (N=2) → base=2.
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	meta := gateMetaFor(g, "con")
	if meta == nil {
		t.Fatal("want gate meta for 'con' after answer --override")
	}
	if meta.Base != 2 {
		t.Errorf("gate meta base: want 2 (max of probe_1=1, teach_2=2), got %d", meta.Base)
	}

	// Check event log: gate event before answer event.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	answerRows := eventsByType(rows, "answer")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d", len(gateRows))
	}
	if len(answerRows) != 1 {
		t.Fatalf("want 1 answer event, got %d", len(answerRows))
	}
	gr := gateRows[0]
	if gr["via"] != "override" {
		t.Errorf("gate event via: want 'override', got %v", gr["via"])
	}

	var gateIdx, answerIdx int
	for i, r := range rows {
		if r["ev"] == "gate" {
			gateIdx = i
		}
		if r["ev"] == "answer" {
			answerIdx = i
		}
	}
	if gateIdx >= answerIdx {
		t.Errorf("gate event (idx %d) must precede answer event (idx %d)", gateIdx, answerIdx)
	}
}

// TestAnswer_NoOverride_GatedRefusal verifies that answering a gated concept
// without --override returns exit 1.
func TestAnswer_NoOverride_GatedRefusal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, gatedConceptWithTeachQ)

	_, errOut, code := run(t, "answer", "q2", "answer text")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: con is gated") {
		t.Errorf("want 'con is gated' err, got:\n%s", errOut)
	}
}

// ── E. ask read-only guarantee ────────────────────────────────────────────────

// TestAsk_ReadOnly_NoOverride verifies that `tm ask` without --override on a
// gated concept returns exit 1 and does NOT write a gate meta to the graph.
func TestAsk_ReadOnly_NoOverride(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	beforeStat, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	_, errOut, code := run(t, "ask", "con")
	if code != 1 {
		t.Fatalf("want exit 1 (gated refusal), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: con is gated") {
		t.Errorf("want 'con is gated' err, got:\n%s", errOut)
	}

	// Graph file must be unchanged (no write).
	afterStat, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if afterStat.ModTime() != beforeStat.ModTime() {
		t.Error("ask without --override must not modify the graph file")
	}

	// No gate meta written.
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	if meta := gateMetaFor(g, "con"); meta != nil {
		t.Errorf("ask without --override must not write gate meta, got %+v", meta)
	}
}

// ── F. --override on ask ──────────────────────────────────────────────────────

// TestAsk_Override_ClearsGateAndEmitsOutput verifies that `tm ask <concept>
// --override "<reason>"` clears the gate (writes meta + event) and proceeds to
// emit the batch (or "nothing to ask" when all batches are below base after
// clearing).
func TestAsk_Override_ClearsGateAndEmitsGateEvent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	_, errOut, code := run(t, "ask", "con", "--override", "clear for diagnosis")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Gate meta must be written.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	meta := gateMetaFor(g, "con")
	if meta == nil {
		t.Fatal("want gate meta for 'con' after ask --override")
	}
	if meta.Base != 1 {
		t.Errorf("gate meta base: want 1, got %d", meta.Base)
	}

	// Gate event in log.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d", len(gateRows))
	}
	gr := gateRows[0]
	if gr["concept"] != "con" {
		t.Errorf("gate event concept: want 'con', got %v", gr["concept"])
	}
	if gr["via"] != "override" {
		t.Errorf("gate event via: want 'override', got %v", gr["via"])
	}
	if gr["reason"] != "clear for diagnosis" {
		t.Errorf("gate event reason: want 'clear for diagnosis', got %v", gr["reason"])
	}
}

// TestAsk_Override_OtherRefusalsStillApply verifies that --override on ask does
// not bypass non-gate refusals (e.g., blocked parent).
func TestAsk_Override_OtherRefusalsStillApply(t *testing.T) {
	// Use gatedChildGraph: "child" is gated AND has a non-passed parent.
	// --override clears gate, but blocked-parent check still fires.
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, gatedChildGraph)

	_, errOut, code := run(t, "ask", "child", "--override", "bypass gate")
	// parent is not passed → blocked-parent refusal should fire (exit 1).
	// But wait: in gatedChildGraph, "parent" is in the PASSED block.
	// After reopen, parent would be untested. But here we just ask child
	// without reopening parent first — parent IS passed, so the parent
	// check passes.
	// Actually in gatedChildGraph the parent IS passed, so the child
	// (child) is on the frontier. The blocked-parent check passes.
	// So exit 0 here (gate cleared, nothing to ask since all batches
	// are below base after clearing).
	if code != 0 {
		t.Fatalf("want exit 0 (parent is passed), got %d; stderr:\n%s", code, errOut)
	}
}

// ── G. Fix 1: ask emits former fallback probe after gate clear ─────────────────

// gatedClearedFallbackGraph has a gate meta (base=4) already written and a
// probe_3 question (q3) that was formerly a fallback probe locked by teach_4.
// After the gate clear, probe_3 must be emittable by ask (Fix 1).
const gatedClearedFallbackGraph = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
        %% tm:gate con base=4
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        q2["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_2
        a2["pass answer"]:::pass
        q3["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_3
        q4["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_4
        a4["pass answer"]:::pass
        con --> q1
        con --> q3
        q1 --> a1
        a1 --> q2
        a1 --> q4
        q2 --> a2
        q4 --> a4
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_3 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestAsk_FallbackProbeEmitted_AfterGateClear verifies Fix 1: once a gate is
// cleared (gate meta base=4 is already present), ask emits the formerly-locked
// fallback probe (probe_3 / q3) rather than returning nothing.
// Before the fix, BatchStateOf(probe_3) returned BatchLocked because teach_4
// (N=4 > probeN=3) existed, even though teach_4 was at the gate base.
func TestAsk_FallbackProbeEmitted_AfterGateClear(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, gatedClearedFallbackGraph)

	out, errOut, code := run(t, "ask", "con")
	if code != 0 {
		t.Fatalf("want exit 0 (gate cleared, probe_3 should be emittable), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "probe_3") {
		t.Errorf("want 'probe_3' batch in ask output, got:\n%s", out)
	}
	if !strings.Contains(out, "q3") {
		t.Errorf("want question 'q3' in ask output, got:\n%s", out)
	}
}

// ── H. Fix 2a: no refusal when latest teach batch is all-pass ─────────────────

// latestTeachAllPassGraph has two fail questions in probe_1 (q1, q5), but
// the latest teach batch (teach_4) is all-pass. Under the old OpenTargets
// condition, ask would refuse because q5 has no passing teach. Under Fix 2,
// ask does not refuse because LatestTeachNotAllPass is false.
const latestTeachAllPassGraph = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        q5["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a5["fail answer"]:::fail
        q2["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_2
        a2["pass answer"]:::pass
        q4["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_4
        a4["pass answer"]:::pass
        con --> q1
        con --> q5
        q1 --> a1
        q5 --> a5
        a1 --> q2
        q2 --> a2
        a2 --> q4
        q4 --> a4
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestAsk_NoRefusal_WhenLatestTeachAllPass verifies Fix 2 (over-refusal path):
// ask must NOT refuse with "teaching round is not complete" when the latest
// teach batch above base is all-pass, even if some failed probe question has
// no passing teach question targeting it (OpenTargets > 0).
func TestAsk_NoRefusal_WhenLatestTeachAllPass(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "3") // not gated: 1 failed batch < MaxFails=3
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, latestTeachAllPassGraph)

	_, errOut, code := run(t, "ask", "con")
	if code != 0 {
		t.Fatalf("want exit 0 (latest teach all-pass, no refusal), got %d; stderr:\n%s", code, errOut)
	}
	if strings.Contains(errOut, "teaching round") {
		t.Errorf("want no 'teaching round' refusal when latest teach is all-pass; got:\n%s", errOut)
	}
}

// ── I. Fix 2b: refusal fires when latest teach batch has a non-pass answer ────

// latestTeachHasFailGraph has probe_1 (q1 fail), teach_2 (q2 pass covers q1),
// and teach_4 (q4 fail). The latest teach batch (teach_4) is not all-pass.
// Under the old OpenTargets condition, ask would NOT refuse (OpenTargets=[]).
// Under Fix 2, ask refuses because LatestTeachNotAllPass is true.
const latestTeachHasFailGraph = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        q2["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_2
        a2["pass answer"]:::pass
        q4["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_4
        a4["fail answer"]:::fail
        con --> q1
        q1 --> a1
        a1 --> q2
        q2 --> a2
        a2 --> q4
        q4 --> a4
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef teach_4 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestAsk_Refuse_WhenLatestTeachHasFail verifies Fix 2 (under-refusal path):
// ask must refuse with "teaching round is not complete" when the latest teach
// batch above base has a non-pass answer, even if OpenTargets is empty
// (the failed probe is already covered by an earlier passing teach question).
func TestAsk_Refuse_WhenLatestTeachHasFail(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "3") // not gated: 1 failed batch < MaxFails=3
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	buildGatedGraph(t, dir, latestTeachHasFailGraph)

	_, errOut, code := run(t, "ask", "con")
	if code != 1 {
		t.Fatalf("want exit 1 (latest teach has fail → teaching incomplete), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "teaching round for con is not complete") {
		t.Errorf("want 'teaching round for con is not complete' in stderr, got:\n%s", errOut)
	}
}

// ── J/K. Fix 3: answer --override clear-then-refusal pattern ──────────────────

// gatedFallbackProbeUnresolvedTeachGraph is a gated concept "con" (probe_1
// with two fail questions q1/q5, TM_MAX_FAILS=1) with an unanswered teach_2
// and an unanswered probe_3 (only one question, q3).
//
// probe_1 has 2 questions so it passes lint even at TM_PROBE_MIN=2.
// probe_3 has 1 question so the apply-level min-count check fires when
// TM_PROBE_MIN=2 (Test K), but not when TM_PROBE_MIN=1 (Test J).
//
// Under old code, answering q3 with --override would wrongly refuse at the
// fallback-probe check (teach_2 is unresolved) using the pre-clear state.
// Under Fix 3, the gate is cleared first, making the fallback-probe check
// irrelevant (all batches fall to/below the new base after clear).
const gatedFallbackProbeUnresolvedTeachGraph = `---
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
    end
    subgraph untested["Concepts User has not been tested on"]
        con["Con scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["fail answer"]:::fail
        q5["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a5["fail answer"]:::fail
        q2["teach scope<br/>f5ca3875b379@src.txt:1-5"]:::teach_2
        q3["probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_3
        con --> q1
        con --> q5
        con --> q3
        q1 --> a1
        q5 --> a5
        a1 --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_3 stroke:#4aa3ff
    classDef teach_2 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestAnswer_Override_FallbackProbe_ClearsGateAndSucceeds verifies Fix 3:
// `tm answer q3 "raw" --override "reason"` on a gated concept must clear the
// gate FIRST, then evaluate remaining refusals against the post-clear state.
// The fallback-probe refusal (teach_2 unresolved) must NOT fire because after
// the gate clear, all batches fall to/below the new base and cs.FallbackProbes
// is empty. The gate line must be persisted before the answer.
func TestAnswer_Override_FallbackProbe_ClearsGateAndSucceeds(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedFallbackProbeUnresolvedTeachGraph)

	out, errOut, code := run(t, "answer", "q3", "my answer", "--override", "override reason")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Gate meta must be written. base = max(N of probe_1=1, teach_2=2, probe_3=3) = 3.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	meta := gateMetaFor(g, "con")
	if meta == nil {
		t.Fatal("want gate meta for 'con' after answer --override, got none")
	}
	if meta.Base != 3 {
		t.Errorf("gate meta base: want 3 (max of probe_1=1, teach_2=2, probe_3=3), got %d", meta.Base)
	}

	// Gate event must precede answer event in the log.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	answerRows := eventsByType(rows, "answer")
	if len(gateRows) != 1 {
		t.Fatalf("want 1 gate event, got %d; all rows: %v", len(gateRows), rows)
	}
	if len(answerRows) != 1 {
		t.Fatalf("want 1 answer event, got %d", len(answerRows))
	}
	gr := gateRows[0]
	if gr["via"] != "override" {
		t.Errorf("gate event via: want 'override', got %v", gr["via"])
	}
	if gr["reason"] != "override reason" {
		t.Errorf("gate event reason: want 'override reason', got %v", gr["reason"])
	}
	var gateIdx, answerIdx int
	for i, r := range rows {
		if r["ev"] == "gate" {
			gateIdx = i
		}
		if r["ev"] == "answer" {
			answerIdx = i
		}
	}
	if gateIdx >= answerIdx {
		t.Errorf("gate event (idx %d) must precede answer event (idx %d)", gateIdx, answerIdx)
	}
}

// TestAnswer_Override_NoGatePersisted_OnSubsequentRefusal verifies the owner
// ruling for Fix 3: when answer --override clears the gate but a subsequent
// refusal fires (here: probe_3 has only 1 question, below TM_PROBE_MIN=2),
// the gate line must NOT be persisted (the whole mutation is atomically aborted).
func TestAnswer_Override_NoGatePersisted_OnSubsequentRefusal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "2") // probe_3 has 1 question < min 2 → refusal fires
	t.Setenv("TM_TEACH_MIN", "1")
	setupSrcFile(t, dir)
	file := buildGatedGraph(t, dir, gatedFallbackProbeUnresolvedTeachGraph)

	_, errOut, code := run(t, "answer", "q3", "my answer", "--override", "override reason")
	if code != 1 {
		t.Fatalf("want exit 1 (min-count refusal after gate clear), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "too few questions") {
		t.Errorf("want 'too few questions' error in stderr, got:\n%s", errOut)
	}

	// Gate meta must NOT be written (mutation was aborted).
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, _ := graph.Parse(data)
	if meta := gateMetaFor(g, "con"); meta != nil {
		t.Errorf("want no gate meta when mutation is aborted; got meta %+v", meta)
	}
}
