package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Fixtures ──────────────────────────────────────────────────────────────────

// reserveTestGraph is a minimal four-block graph with:
//   - concept "ca" (untested, no questions)
//   - concept "cb" (untested, parent of ca, no questions)
//   - concept "cc" (untested, has a question q1)
//   - "cb" --"requires"--> "ca" edge
const reserveTestGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ca["Concept A<br/>f5ca3875b379@src.txt:1-5"]
        cb["Concept B<br/>f5ca3875b379@src.txt:1-5"]
        cc["Concept C<br/>f5ca3875b379@src.txt:1-5"]
        cb --"requires"--> ca
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        cc --> q1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// activateTestGraph is a four-block graph with "rr" in reserve (parent of "ca").
const activateTestGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ca["Concept A<br/>f5ca3875b379@src.txt:1-5"]
        cb["Concept B<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        rr["Reserve R<br/>f5ca3875b379@src.txt:1-5"]
        rr --"provides"--> ca
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// pruneTestGraph has:
//   - goal "gg" (untested, no questions)
//   - parent "pp" (untested, parent of gg, no questions) — in gg's closure
//   - unrelated "xx" (untested, not in gg's closure, no questions)
//   - "pp" --"requires"--> "gg" edge
const pruneTestGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        gg["Goal G<br/>f5ca3875b379@src.txt:1-5"]
        pp["Parent P<br/>f5ca3875b379@src.txt:1-5"]
        xx["Unrelated X<br/>f5ca3875b379@src.txt:1-5"]
        pp --"requires"--> gg
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// gatedWithReserveParentGraph has a gated concept "cc" whose parent "rr" is in
// reserve. With TM_MAX_FAILS=1 and TM_PROBE_MIN=1, the failed probe_1 answer
// makes "cc" gated (no explicit gate meta; state derives gate from fail count).
const gatedWithReserveParentGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        cc["Child C<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        rr["Reserve R<br/>f5ca3875b379@src.txt:1-5"]
        rr --"provides"--> cc
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["wrong answer"]:::fail
        cc --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// threeBlockTestGraph is a pre-v0.3 three-block file (no reserve subgraph).
const threeBlockTestGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ca["Concept A<br/>f5ca3875b379@src.txt:1-5"]
        cb["Concept B<br/>f5ca3875b379@src.txt:1-5"]
        cb --"requires"--> ca
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// writeGraph writes content to "g.mmd" in dir and returns the path.
func writeGraph(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return path
}

// ── tm reserve ────────────────────────────────────────────────────────────────

func TestReserve(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantErr  string
		wantExit int
	}{
		{
			name:     "success: move question-less untested concept",
			args:     []string{"reserve", "ca"},
			wantOut:  "ok",
			wantExit: 0,
		},
		{
			name:     "refuse: concept has questions, no --reason",
			args:     []string{"reserve", "cc"},
			wantErr:  "err: cc has questions",
			wantExit: 1,
		},
		{
			name:     "refuse: concept has questions, --reason but batch not resolved",
			args:     []string{"reserve", "cc", "--reason", "skip it"},
			wantErr:  "err: cc has an unresolved batch",
			wantExit: 1,
		},
		{
			name:     "refuse: unknown ID",
			args:     []string{"reserve", "zzz"},
			wantErr:  `err: unknown ID "zzz"`,
			wantExit: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, reserveTestGraph)
			t.Setenv("TM_FILE", path)

			out, errOut, code := run(t, tc.args...)
			if code != tc.wantExit {
				t.Errorf("exit: want %d got %d; stdout=%q stderr=%q",
					tc.wantExit, code, out, errOut)
			}
			if tc.wantOut != "" && strings.TrimSpace(out) != tc.wantOut {
				t.Errorf("stdout: want %q got %q", tc.wantOut, strings.TrimSpace(out))
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q in %q", tc.wantErr, errOut)
			}
			if code == 0 {
				rows := readEventLog(t, path)
				last := rows[len(rows)-1]
				if last["ev"] != "reserve" || last["concept"] != "ca" {
					t.Errorf("event: want reserve{ca}, got %v", last)
				}
			}
		})
	}
}

// TestReserve_ConceptMovedToReserveBlock checks the graph bytes after reserve.
func TestReserve_ConceptMovedToReserveBlock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, reserveTestGraph)
	t.Setenv("TM_FILE", path)

	_, _, code := run(t, "reserve", "ca")
	if code != 0 {
		t.Fatal("reserve should succeed")
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	rIdx := strings.Index(content, "subgraph reserve")
	tIdx := strings.Index(content, "subgraph testing")
	aIdx := strings.Index(content, `ca["Concept A`)
	if rIdx < 0 || tIdx < 0 || aIdx < 0 {
		t.Fatalf("subgraph or concept not found; reserve=%d testing=%d ca=%d", rIdx, tIdx, aIdx)
	}
	if aIdx <= rIdx || aIdx >= tIdx {
		t.Errorf("ca not in reserve block: rIdx=%d aIdx=%d tIdx=%d", rIdx, aIdx, tIdx)
	}
	lintFile(t, path, dir)
}

// TestReserve_NotUntested verifies refusal when concept is already in reserve.
func TestReserve_NotUntested(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, activateTestGraph)
	t.Setenv("TM_FILE", path)

	_, errOut, code := run(t, "reserve", "rr")
	if code != 1 {
		t.Errorf("exit: want 1 got %d; stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "is not in untested") {
		t.Errorf("stderr: want 'is not in untested'; got %q", errOut)
	}
}

// ── tm activate ───────────────────────────────────────────────────────────────

func TestActivate(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantErr  string
		wantExit int
	}{
		{
			name:     "success: move reserve concept to untested",
			args:     []string{"activate", "rr"},
			wantOut:  "ok",
			wantExit: 0,
		},
		{
			name:     "refuse: concept not in reserve",
			args:     []string{"activate", "ca"},
			wantErr:  "err: ca is not in reserve",
			wantExit: 1,
		},
		{
			name:     "refuse: unknown ID",
			args:     []string{"activate", "zzz"},
			wantErr:  `err: unknown ID "zzz"`,
			wantExit: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, activateTestGraph)
			t.Setenv("TM_FILE", path)

			out, errOut, code := run(t, tc.args...)
			if code != tc.wantExit {
				t.Errorf("exit: want %d got %d; stdout=%q stderr=%q",
					tc.wantExit, code, out, errOut)
			}
			if tc.wantOut != "" && strings.TrimSpace(out) != tc.wantOut {
				t.Errorf("stdout: want %q got %q", tc.wantOut, strings.TrimSpace(out))
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q in %q", tc.wantErr, errOut)
			}
			if code == 0 {
				rows := readEventLog(t, path)
				last := rows[len(rows)-1]
				if last["ev"] != "activate" || last["concept"] != "rr" {
					t.Errorf("event: want activate{rr}, got %v", last)
				}
			}
		})
	}
}

// ── tm prune ──────────────────────────────────────────────────────────────────

func TestPrune(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantErr  string
		wantExit int
	}{
		{
			name:     "basic: parks xx (outside closure), not pp (in closure)",
			args:     []string{"prune", "gg"},
			wantOut:  "reserved 1",
			wantExit: 0,
		},
		{
			name:     "keep 1: parks pp and xx (goal only kept)",
			args:     []string{"prune", "gg", "--keep", "1"},
			wantOut:  "reserved 2",
			wantExit: 0,
		},
		{
			name:     "keep 2: parks only xx (pp and gg fill N=2)",
			args:     []string{"prune", "gg", "--keep", "2"},
			wantOut:  "reserved 1",
			wantExit: 0,
		},
		{
			name:     "refuse: --keep 0",
			args:     []string{"prune", "gg", "--keep", "0"},
			wantErr:  "err: --keep must be a positive integer",
			wantExit: 1,
		},
		{
			name:     "refuse: unknown goal",
			args:     []string{"prune", "zzz"},
			wantErr:  `err: unknown ID "zzz"`,
			wantExit: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, pruneTestGraph)
			t.Setenv("TM_FILE", path)

			out, errOut, code := run(t, tc.args...)
			if code != tc.wantExit {
				t.Errorf("exit: want %d got %d; stdout=%q stderr=%q",
					tc.wantExit, code, out, errOut)
			}
			if tc.wantOut != "" && strings.TrimSpace(out) != tc.wantOut {
				t.Errorf("stdout: want %q got %q", tc.wantOut, strings.TrimSpace(out))
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("stderr: want %q in %q", tc.wantErr, errOut)
			}
		})
	}
}

// TestPrune_GoalInReserve_FixMessage checks the spec §7 fix line.
func TestPrune_GoalInReserve_FixMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	const g = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ca["Concept A<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        gg["Goal G<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	path := writeGraph(t, dir, g)
	t.Setenv("TM_FILE", path)

	_, errOut, code := run(t, "prune", "gg")
	if code != 1 {
		t.Errorf("exit: want 1 got %d", code)
	}
	if !strings.Contains(errOut, "fix: tm activate gg") {
		t.Errorf("fix should say tm activate gg; got: %q", errOut)
	}
}

// TestPrune_EventFields checks the event log entry for prune.
func TestPrune_EventFields(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, pruneTestGraph)
	t.Setenv("TM_FILE", path)

	_, _, code := run(t, "prune", "gg")
	if code != 0 {
		t.Fatal("prune should succeed")
	}
	rows := readEventLog(t, path)
	if len(rows) == 0 {
		t.Fatal("no events written")
	}
	ev := rows[len(rows)-1]
	if ev["ev"] != "prune" {
		t.Errorf("ev: want prune got %v", ev["ev"])
	}
	if ev["goal"] != "gg" {
		t.Errorf("goal: want gg got %v", ev["goal"])
	}
	if ev["keep"] != nil {
		t.Errorf("keep: want nil got %v", ev["keep"])
	}
	moved, _ := ev["moved"].([]interface{})
	if len(moved) != 1 || moved[0] != "xx" {
		t.Errorf("moved: want [xx] got %v", ev["moved"])
	}
}

// ── Gate fix message ──────────────────────────────────────────────────────────

// TestGateFixMsg_WithReserveParent verifies that a gated concept whose parent
// is in reserve produces a fix line leading with "tm activate <parent>".
func TestGateFixMsg_WithReserveParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, gatedWithReserveParentGraph)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")

	_, errOut, code := run(t, "ask", "cc")
	if code != 1 {
		t.Errorf("exit: want 1 got %d", code)
	}
	if !strings.Contains(errOut, "fix: tm activate rr") {
		t.Errorf("fix should start with tm activate rr; got: %q", errOut)
	}
	if !strings.Contains(errOut, "tm add --child cc") {
		t.Errorf("fix should contain tm add --child cc; got: %q", errOut)
	}
}

// TestGateFixMsg_NoReserveParent verifies the activate clause is omitted when
// the gated concept has no reserve parents.
func TestGateFixMsg_NoReserveParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)

	const gatedGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
        pp["Parent P<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        cc["Child C<br/>f5ca3875b379@src.txt:1-5"]
        pp --"requires"--> cc
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Probe scope<br/>f5ca3875b379@src.txt:1-5"]:::probe_1
        a1["wrong answer"]:::fail
        cc --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	path := writeGraph(t, dir, gatedGraph)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")

	_, errOut, code := run(t, "ask", "cc")
	if code != 1 {
		t.Errorf("exit: want 1 got %d", code)
	}
	if strings.Contains(errOut, "tm activate") {
		t.Errorf("fix should NOT contain tm activate; got: %q", errOut)
	}
	if !strings.Contains(errOut, "tm add --child cc") {
		t.Errorf("fix should contain tm add --child cc; got: %q", errOut)
	}
}

// ── Lifecycle tests ───────────────────────────────────────────────────────────

// TestPruneThenPassThroughReserveParent exercises §16.5:
// prune parks a parent into reserve → child becomes frontier → ask/pass child.
func TestPruneThenPassThroughReserveParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)

	// goal "ga" has parent "gb" (also untested, no questions).
	const g = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        ga["Goal A<br/>f5ca3875b379@src.txt:1-5"]
        gb["Foundation B<br/>f5ca3875b379@src.txt:1-5"]
        gb --"requires"--> ga
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	path := writeGraph(t, dir, g)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_PROBE_MAX", "3")

	// prune ga --keep 1: parks gb (beyond hop 0 = goal only).
	out, errOut, code := run(t, "prune", "ga", "--keep", "1")
	if code != 0 {
		t.Fatalf("prune: want exit 0 got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "reserved 1" {
		t.Errorf("prune output: want 'reserved 1' got %q", out)
	}

	// ga is now on the frontier (gb is in reserve, not blocking).
	// Add a question and pass it.
	_, errOut, code = run(t, "q", "ga", "f5ca3875b379@src.txt:1-5", "probe scope")
	if code != 0 {
		t.Fatalf("q: want exit 0 got %d; stderr: %s", code, errOut)
	}

	out, errOut, code = run(t, "ask", "ga")
	if code != 0 {
		t.Fatalf("ask ga: want exit 0 got %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "q1") {
		t.Errorf("ask ga: expected q1; got %q", out)
	}

	_, errOut, code = run(t, "answer", "q1", "correct answer")
	if code != 0 {
		t.Fatalf("answer: want exit 0 got %d; stderr: %s", code, errOut)
	}

	_, errOut, code = run(t, "grade", "q1", "pass", "correct")
	if code != 0 {
		t.Fatalf("grade: want exit 0 got %d; stderr: %s", code, errOut)
	}

	out, _, _ = run(t, "status")
	if !strings.Contains(out, "passed 1") {
		t.Errorf("after passing ga: want 'passed 1'; got %q", out)
	}
}

// TestActivateClearsGate exercises §16.5:
// gate a concept, activate reserve parent, assert gate clears with via=activate,
// and concept stays blocked by frontier rule until parent passes.
func TestActivateClearsGate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, gatedWithReserveParentGraph)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_MAX_FAILS", "1")
	t.Setenv("TM_PROBE_MIN", "1")

	// Before activate: ask cc should fail with gate error.
	_, errOut, code := run(t, "ask", "cc")
	if code != 1 {
		t.Errorf("before activate: want exit 1 got %d", code)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("before activate: expected 'is gated'; got %q", errOut)
	}

	// Activate rr: clears cc's gate.
	_, errOut, code = run(t, "activate", "rr")
	if code != 0 {
		t.Fatalf("activate rr: want exit 0 got %d; stderr: %s", code, errOut)
	}

	// Verify activate event with unblocked=[cc] and gate event with via=activate.
	rows := readEventLog(t, path)
	var activateRow, gateRow map[string]any
	for _, r := range rows {
		switch r["ev"] {
		case "activate":
			activateRow = r
		case "gate":
			gateRow = r
		}
	}
	if activateRow == nil {
		t.Fatal("no activate event")
	}
	if gateRow == nil {
		t.Fatal("no gate event")
	} else {
		if gateRow["via"] != "activate" {
			t.Errorf("gate via: want activate got %v", gateRow["via"])
		}
		if gateRow["concept"] != "cc" {
			t.Errorf("gate concept: want cc got %v", gateRow["concept"])
		}
	}
	if ub, _ := activateRow["unblocked"].([]interface{}); len(ub) == 0 || ub[0] != "cc" {
		t.Errorf("activate unblocked: want [cc] got %v", activateRow["unblocked"])
	}

	// After activate: cc is not gated anymore but still blocked by frontier
	// (rr is now in untested, not passed). Error should be about parent not passed.
	_, errOut, code = run(t, "ask", "cc")
	if code != 1 {
		t.Errorf("after activate: want exit 1 (frontier block) got %d", code)
	}
	if strings.Contains(errOut, "is gated") {
		t.Errorf("after activate: cc should not be gated; stderr: %q", errOut)
	}
}

// TestUpgradeThreeBlockOnMutation exercises §16.5:
// a pre-v0.3 three-block file gets the reserve block inserted on first write.
func TestUpgradeThreeBlockOnMutation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)

	original := []byte(threeBlockTestGraph)
	if bytes.Contains(original, []byte("subgraph reserve")) {
		t.Fatal("threeBlockTestGraph already has a reserve block")
	}
	path := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_FILE", path)

	// tm status: read-only — no write, three-block file loads fine.
	_, errOut, code := run(t, "status")
	if code != 0 {
		t.Fatalf("status on three-block: want exit 0 got %d; stderr: %s", code, errOut)
	}

	// tm reserve ca: triggers a write; reserve block must appear.
	out, errOut, code := run(t, "reserve", "ca")
	if code != 0 {
		t.Fatalf("reserve ca: want exit 0 got %d; stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("reserve output: want ok got %q", out)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(written)

	// Reserve block must be present and ca must be inside it.
	rIdx := strings.Index(content, "subgraph reserve")
	tIdx := strings.Index(content, "subgraph testing")
	uIdx := strings.Index(content, "subgraph untested")
	caIdx := strings.Index(content, `ca["Concept A`)
	cbIdx := strings.Index(content, `cb["Concept B`)

	if rIdx < 0 {
		t.Error("reserve block missing after upgrade mutation")
	}
	if caIdx < 0 {
		t.Error("ca not found in written file")
	} else if caIdx <= rIdx || caIdx >= tIdx {
		t.Errorf("ca not in reserve block: uIdx=%d rIdx=%d caIdx=%d tIdx=%d",
			uIdx, rIdx, caIdx, tIdx)
	}
	// cb must remain in untested.
	if cbIdx < 0 {
		t.Error("cb not found in written file")
	} else if cbIdx <= uIdx || cbIdx >= rIdx {
		t.Errorf("cb not in untested block: uIdx=%d cbIdx=%d rIdx=%d",
			uIdx, cbIdx, rIdx)
	}

	lintFile(t, path, dir)
}

// TestReserveIDTreatedAsUnknown pins that commands other than activate,
// prune, reserve, add, and link do not see reserve concepts: they refuse the ID instead
// of acting on it (edit and gap only search untested when rewriting a node).
func TestReserveIDTreatedAsUnknown(t *testing.T) {
	for _, args := range [][]string{
		{"edit", "rr", "new scope"},
		{"gap", "rr", "a gap"},
		{"drop", "rr"},
		{"reopen", "rr", "a gap"},
		{"ask", "rr"},
	} {
		t.Run(args[0], func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, activateTestGraph)
			t.Setenv("TM_FILE", path)

			_, errOut, code := run(t, args...)
			if code == 0 {
				t.Fatalf("want refusal, got exit 0")
			}
			if !strings.Contains(errOut, `"rr"`) && !strings.Contains(errOut, "rr is not") {
				t.Errorf("stderr should name rr; got %q", errOut)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != activateTestGraph {
				t.Errorf("graph changed on refusal")
			}
		})
	}
}

// TestReserveEndpoints pins that add and link attach edges to reserve concepts
// without blocking active work, and that the cycle check spans reserve. The
// fixture already has rr --> ca.
func TestReserveEndpoints(t *testing.T) {
	const cite = "f5ca3875b379@src.txt:1-5"
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"link reserve parent", []string{"link", "rr", "cb", "requires"}, 0},
		{"link reserve child", []string{"link", "cb", "rr", "requires"}, 0},
		{"link closes cycle through reserve", []string{"link", "ca", "rr", "requires"}, 1},
		{"add reserve parent", []string{"add", "nn", cite, "new", "--parent", "rr:requires"}, 0},
		{"add reserve child", []string{"add", "nn", cite, "new", "--child", "rr:requires"}, 0},
		{"add closes cycle through reserve", []string{"add", "nn", cite, "new", "--parent", "ca:requires", "--child", "rr:requires"}, 1},
		{"add reserve ID", []string{"add", "rr", cite, "new"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, activateTestGraph)
			t.Setenv("TM_FILE", path)

			_, errOut, code := run(t, tc.args...)
			if code != tc.code {
				t.Fatalf("want exit %d, got %d; stderr:\n%s", tc.code, code, errOut)
			}
			if code != 0 {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != activateTestGraph {
					t.Errorf("graph changed on refusal")
				}
				return
			}
			lintFile(t, path, dir)
			out, _, _ := run(t, "status")
			if !strings.Contains(out, "blocked 0") {
				t.Errorf("reserve endpoint blocked active work; status:\n%s", out)
			}
		})
	}
}

// probedParentGraph has "cc" probed with a failed batch and a recorded gap,
// and "cc" --"requires"--> "cd" (issue #75).
const probedParentGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        cc["Concept C<br/>GAP: missed the point<br/>f5ca3875b379@src.txt:1-5"]
        cd["Concept D<br/>f5ca3875b379@src.txt:1-5"]
        cc --"requires"--> cd
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Probe one<br/>f5ca3875b379@src.txt:1-2"]:::probe_1
        a1["wrong answer"]:::fail
        q2["Probe two<br/>f5ca3875b379@src.txt:3-4"]:::probe_1
        a2["right answer"]:::pass
        cc --> q1
        q1 --> a1
        cc --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

// TestReserve_ProbedConceptWithReason pins that a concept found out of scope
// after probing can leave the active graph without losing its history (#75).
func TestReserve_ProbedConceptWithReason(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, probedParentGraph)
	t.Setenv("TM_FILE", path)

	_, errOut, code := run(t, "reserve", "cc", "--reason", "outside the goal's domain")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	rows := readEventLog(t, path)
	last := rows[len(rows)-1]
	if last["ev"] != "reserve" || last["concept"] != "cc" || last["reason"] != "outside the goal's domain" {
		t.Errorf("event: want reserve{cc, reason}, got %v", last)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []string{`q1["Probe one`, `a1["wrong answer"]:::fail`, `q2["Probe two`, `a2["right answer"]:::pass`} {
		if !strings.Contains(string(data), node) {
			t.Errorf("history lost: %s missing", node)
		}
	}
	lintFile(t, path, dir)

	if out, _, _ := run(t, "status"); !strings.Contains(out, "blocked 0") {
		t.Errorf("reserved cc still blocks cd; status:\n%s", out)
	}
}

// TestUnlink pins that a prerequisite edge can be removed (#75).
func TestUnlink(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, probedParentGraph)
	t.Setenv("TM_FILE", path)

	out, errOut, code := run(t, "unlink", "cc", "cd")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `cc --"requires"--> cd`) {
		t.Error("edge cc -> cd still present")
	}
	rows := readEventLog(t, path)
	last := rows[len(rows)-1]
	if last["ev"] != "unlink" || last["from"] != "cc" || last["to"] != "cd" {
		t.Errorf("event: want unlink{cc, cd}, got %v", last)
	}
	lintFile(t, path, dir)
}

// TestUnlink_Refusals covers the error paths for tm unlink.
func TestUnlink_Refusals(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantErrContains string
		wantExit        int
	}{
		{
			name:            "unknown from",
			args:            []string{"unlink", "zzz", "cd"},
			wantErrContains: `err: unknown ID "zzz"`,
			wantExit:        3,
		},
		{
			name:            "unknown to",
			args:            []string{"unlink", "cc", "zzz"},
			wantErrContains: `err: unknown ID "zzz"`,
			wantExit:        3,
		},
		{
			name:            "non-concept from",
			args:            []string{"unlink", "q1", "cd"},
			wantErrContains: "err: q1 is not a concept",
			wantExit:        1,
		},
		{
			name:            "non-concept to",
			args:            []string{"unlink", "cc", "q1"},
			wantErrContains: "err: q1 is not a concept",
			wantExit:        1,
		},
		{
			name:            "edge does not exist",
			args:            []string{"unlink", "cd", "cc"},
			wantErrContains: "err: no edge from cd to cc",
			wantExit:        1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			tempErrlog(t)
			setupSrcFile(t, dir)
			path := writeGraph(t, dir, probedParentGraph)
			t.Setenv("TM_FILE", path)

			_, errOut, code := run(t, tc.args...)
			if code != tc.wantExit {
				t.Errorf("exit: want %d got %d; stderr=%q", tc.wantExit, code, errOut)
			}
			if !strings.Contains(errOut, tc.wantErrContains) {
				t.Errorf("stderr: want %q in %q", tc.wantErrContains, errOut)
			}
		})
	}
}

// reserveConceptPendingProbeGraph is a hand-crafted graph with "cc" in reserve
// and a single pending probe answer — simulating an old-format graph that the
// v0.30 open-batch guard on tm grade must defend against.
const reserveConceptPendingProbeGraph = `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        cd["Concept D<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        cc["Concept C<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["Probe one<br/>f5ca3875b379@src.txt:1-2"]:::probe_1
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

// TestGrade_ReserveConceptGuard verifies that grading a reserve concept's probe
// to all-pass is refused rather than silently failing to move the concept.
func TestGrade_ReserveConceptGuard(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	setupSrcFile(t, dir)
	path := writeGraph(t, dir, reserveConceptPendingProbeGraph)
	t.Setenv("TM_FILE", path)
	t.Setenv("TM_PROBE_MIN", "1") // single-question batch satisfies min

	_, errOut, code := run(t, "grade", "q1", "pass", "good")
	if code != 1 {
		t.Fatalf("want exit 1 for reserve guard, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is in reserve and cannot be passed") {
		t.Errorf("want reserve guard err; got:\n%s", errOut)
	}
}
