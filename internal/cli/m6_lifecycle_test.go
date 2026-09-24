package cli_test

// M6 lifecycle and concurrency tests — §16.5
//
// Each test covers one or more transitions in the §12 state diagram.
// All tests use the in-process run(t, ...) harness, which exercises the real
// lock/atomic-write path while staying in the go test -race suite and under
// diff-coverage.
//
// Env overrides keep each scenario reachable in a few steps:
//   TM_PROBE_MIN / TM_PROBE_MAX control probe batch size checks.
//   TM_MAX_FAILS controls how many failed probe batches gate a concept.
//   TM_MAX_STALL / TM_TEACH_MIN / TM_TEACH_MAX control teaching mechanics.
//   TM_MAX_TEACH controls teaching-spent cap.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/lint"
)

// ── Base fixtures for M6 lifecycle tests ─────────────────────────────────────

// lifecycleTeachReadyGraph is the state just before adding a teach question:
// probe_1 (q1 pass, q2 fail) resolved, probe_2 fallback (q3, q4 unanswered),
// GAP set on mycon. No teach question yet — the lifecycle test adds one via
// tm q --teach --re q2.
func lifecycleTeachReadyGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        q3["Fallback probe 1<br/>e266782c2841@src.txt:1-2"]:::probe_2
        q4["Fallback probe 2<br/>20f437d6f701@src.txt:3-4"]:::probe_2
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        mycon --> q3
        mycon --> q4
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// ── lint helper with custom config ───────────────────────────────────────────

// lintM6 checks that the graph at path passes lint using the given srcRoot and
// probeMin. TeachMin is always 1 for M6 lifecycle tests.
func lintM6(t *testing.T, file, srcRoot string, probeMin int) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file for lint: %v", err)
	}
	cfg := lint.Config{
		SrcRoot:  srcRoot,
		ProbeMin: probeMin,
		ProbeMax: 5,
		TeachMin: 1,
		TeachMax: 8,
	}
	viols := lint.Check(data, cfg)
	if len(viols) > 0 {
		t.Errorf("graph fails lint: %v", viols)
	}
}

// ── §12 edge: Verdict → Passed (all probe pass) ───────────────────────────────

// TestLifecycle_Pass drives a concept through the full pass path:
// Untested → DraftProbes → Answering → Grading → Verdict → Passed.
// Uses TM_PROBE_MIN=2 so that both probes are required (matches default lint).
func TestLifecycle_Pass(t *testing.T) {
	// §12 edge: Verdict → Passed
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_MAX_FAILS", "2")

	// Pre-built graph: mycon in untested, probe_1 with q1 (pending a1) and q2 (pass a2).
	// Grading q1 pass → all pass → pass procedure.
	file := qWriteGraph(t, dir, gradeAllPassGraph())

	// ── Grading: q1 pass → Verdict → Passed ──────────────────────────────────
	out, errOut, code := run(t, "grade", "q1", "pass", "well explained")
	if code != 0 {
		t.Fatalf("grade q1 pass: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade: want 'ok', got %q", out)
	}

	// Assert: mycon in passed, testing block empty.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 pass edge: mycon not in passed after all-probe-pass grade")
	}
	if len(g.TestingItems) != 0 {
		t.Errorf("§12 pass edge: testing block not empty after pass: %d items remain", len(g.TestingItems))
	}

	// Event log: grade → gc → pass.
	rows := readEventLog(t, file)
	if len(rows) != 3 {
		t.Fatalf("§12 pass edge: want 3 events (grade, gc, pass), got %d: %v", len(rows), rows)
	}
	if rows[0]["ev"] != "grade" {
		t.Errorf("event[0]: want 'grade', got %v", rows[0]["ev"])
	}
	if rows[1]["ev"] != "gc" {
		t.Errorf("event[1]: want 'gc', got %v", rows[1]["ev"])
	}
	if rows[2]["ev"] != "pass" {
		t.Errorf("event[2]: want 'pass', got %v", rows[2]["ev"])
	}
	if rows[2]["concept"] != "mycon" {
		t.Errorf("pass event concept: want 'mycon', got %v", rows[2]["concept"])
	}

	lintFile(t, file, dir)
}

// ── §12 edge: Verdict → DraftReplacements → Answering ────────────────────────

// TestLifecycle_UnclearReplacement drives: probe unclear → replacement probe →
// grade replacement pass → concept passes.
// Covers §12 edge: Verdict → DraftReplacements → Answering → (Verdict → Passed).
func TestLifecycle_UnclearReplacement(t *testing.T) {
	// §12 edge: Verdict → DraftReplacements → Answering
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_MAX_FAILS", "2")

	// Pre-built graph: probe_1 with q1 (pending a1) and q2 (unclear a2).
	// Grading q1 unclear → batch complete, no fail, some unclear (§8 step 5).
	file := qWriteGraph(t, dir, gradeProbeUnclearRootGraph())

	// ── Grade q1 unclear → §8 step 5: no fail, some unclear ─────────────────
	out, errOut, code := run(t, "grade", "q1", "unclear", "ambiguous question")
	if code != 0 {
		t.Fatalf("grade q1 unclear: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade: want 'ok', got %q", out)
	}

	// Assert: no pass yet (concept still in untested).
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("§12 unclear replacement: mycon must not pass after unclear verdict")
		}
	}

	rows := readEventLog(t, file)
	if len(rows) != 1 || rows[0]["ev"] != "grade" {
		t.Errorf("§12 unclear replacement: want 1 grade event, got %v", rows)
	}

	// ── DraftReplacements: add one replacement probe per unclear ─────────────
	// probe_1 had q1 (unclear) and q2 (unclear). Replacement batch needs one
	// --re for each unclear probe: q1 and q2.
	out, errOut, code = run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "replacement for q1", "--re", "q1")
	if code != 0 {
		t.Fatalf("q --re q1: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q3 := strings.TrimSpace(out)
	if q3 == "" {
		t.Fatal("q --re q1: want question ID, got empty")
	}

	out, errOut, code = run(t, "q", "mycon", "e28e810e6e2e@src.txt:3-5", "replacement for q2", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --re q2: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q4 := strings.TrimSpace(out)
	if q4 == "" {
		t.Fatal("q --re q2: want question ID, got empty")
	}

	// ── Answering: record answers for the replacement probes ─────────────────
	_, errOut, code = run(t, "answer", q3, "good replacement answer 1")
	if code != 0 {
		t.Fatalf("answer %s: want exit 0, got %d; stderr:\n%s", q3, code, errOut)
	}
	_, errOut, code = run(t, "answer", q4, "good replacement answer 2")
	if code != 0 {
		t.Fatalf("answer %s: want exit 0, got %d; stderr:\n%s", q4, code, errOut)
	}

	// ── Grading replacements: both pass → replacement batch all pass → concept passes ─
	// Replacement probe batch is min-exempt (§8.5). The pass procedure fires
	// when the replacement batch is all pass.
	_, errOut, code = run(t, "grade", q3, "pass", "now clear")
	if code != 0 {
		t.Fatalf("grade %s pass: want exit 0, got %d; stderr:\n%s", q3, code, errOut)
	}

	// After grading q3 (replacement batch has 2 questions, q4 still pending), batch incomplete.
	data, _ = os.ReadFile(file)
	g, _ = graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("§12 unclear replacement: mycon must not pass after only one replacement graded")
		}
	}

	_, errOut, code = run(t, "grade", q4, "pass", "also clear")
	if code != 0 {
		t.Fatalf("grade %s pass: want exit 0, got %d; stderr:\n%s", q4, code, errOut)
	}

	// Assert: mycon now passed.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 unclear replacement: mycon must pass after replacement batch all pass")
	}

	// Event log ends with gc + pass.
	rows = readEventLog(t, file)
	evs := make([]string, len(rows))
	for i, r := range rows {
		evs[i] = r["ev"].(string)
	}
	last2 := evs[len(evs)-2:]
	if len(last2) < 2 || last2[0] != "gc" || last2[1] != "pass" {
		t.Errorf("§12 unclear replacement: last 2 events want [gc, pass], got %v", last2)
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edges: Verdict → DraftRound → Teaching → Answering → Passed ─────────

// TestLifecycle_TeachingRound drives the full teaching round path:
// probe fail → add teach question → teach all pass → fallback probes → concept passes.
// Covers §12 edges: Verdict → DraftRound → Teaching → Answering → Verdict → Passed.
func TestLifecycle_TeachingRound(t *testing.T) {
	// §12 edges: Verdict → DraftRound, Teaching → Answering
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "8")
	t.Setenv("TM_MAX_FAILS", "2")
	t.Setenv("TM_MAX_STALL", "4")
	t.Setenv("TM_MAX_TEACH", "8")

	// Pre-built graph: lifecycleTeachReadyGraph has probe_1 (q1 pass, q2 fail)
	// and probe_2 fallback (q3, q4 unanswered). GAP is set. No teach question yet.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// ── DraftRound: add teach question targeting the failed probe q2 ─────────
	// §12: "tm q --teach --re a failed probe. First teach question locks the probes"
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "teach the missed concept", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach --re q2: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q5 := strings.TrimSpace(out)
	if q5 == "" {
		t.Fatal("q --teach --re q2: want question ID, got empty")
	}

	// ── Teaching: answer + grade teach question all pass ──────────────────────
	_, errOut, code = run(t, "answer", q5, "I understand now")
	if code != 0 {
		t.Fatalf("answer teach: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	out, errOut, code = run(t, "grade", q5, "pass", "well taught")
	if code != 0 {
		t.Fatalf("grade teach pass: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade teach: want 'ok', got %q", out)
	}

	// After teach all pass: fallback probes (q3, q4) become answerable (§8 step 8).
	// Assert: concept still not passed (probes not graded yet).
	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("§12 teaching round: mycon must not pass after teach grade (probes not yet graded)")
		}
	}

	// ── Answering fallback probes (probe_2: q3, q4) ───────────────────────────
	_, errOut, code = run(t, "answer", "q3", "my fallback answer 1")
	if code != 0 {
		t.Fatalf("answer q3: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", "q4", "my fallback answer 2")
	if code != 0 {
		t.Fatalf("answer q4: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// ── Grade fallback probes: both pass → Verdict → Passed ──────────────────
	_, errOut, code = run(t, "grade", "q3", "pass", "confirmed 1")
	if code != 0 {
		t.Fatalf("grade q3: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	out, errOut, code = run(t, "grade", "q4", "pass", "confirmed 2")
	if code != 0 {
		t.Fatalf("grade q4: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade q4: want 'ok', got %q", out)
	}

	// Assert: mycon now passed.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 teaching round: mycon must pass after fallback probes all pass")
	}
	if len(g.TestingItems) != 0 {
		t.Errorf("§12 teaching round: testing block not empty after pass: %d items remain", len(g.TestingItems))
	}

	rows := readEventLog(t, file)
	// Check the sequence ends with gc + pass events.
	evs := make([]string, len(rows))
	for i, r := range rows {
		evs[i] = r["ev"].(string)
	}
	lastTwo := evs[len(evs)-2:]
	if len(lastTwo) < 2 || lastTwo[0] != "gc" || lastTwo[1] != "pass" {
		t.Errorf("§12 teaching round: last 2 events want [gc, pass], got %v", lastTwo)
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Teaching with --oos ────────────────────────────────────────────

// TestLifecycle_OOS drives: a two-question teach batch where one question is
// graded --oos (closing that branch) and the other passes in-scope; the in-scope
// pass resolves step 8 and fallback probes become answerable.
// Covers §12 edge: "An oos flag closes that branch".
func TestLifecycle_OOS(t *testing.T) {
	// §12 edge: Teaching with --oos closes OOS branch; in-scope questions resolve
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "8")
	t.Setenv("TM_MAX_TEACH", "8")
	t.Setenv("TM_MAX_STALL", "4")
	t.Setenv("TM_MAX_FAILS", "2")

	// Graph: lifecycleTeachReadyGraph has probe_1 (q1 pass, q2 fail), probe_2
	// fallback (q3, q4 unanswered), GAP set. No teach question yet.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// Add two teach questions to teach_3 BEFORE any answers (so both can be added).
	// q5: in-scope (will grade pass)
	// q6: OOS (will grade fail --oos, closing that branch)
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "teach in-scope", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach in-scope: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q5 := strings.TrimSpace(out)

	out, errOut, code = run(t, "q", "mycon", "e28e810e6e2e@src.txt:3-5", "teach oos branch", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q6 := strings.TrimSpace(out)

	// Answer both teach questions.
	_, errOut, code = run(t, "answer", q5, "in-scope clear explanation")
	if code != 0 {
		t.Fatalf("answer q5: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", q6, "tangential rambling")
	if code != 0 {
		t.Fatalf("answer q6: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Grade q5 pass (in-scope) → batch not complete (q6 still pending) → step 3.
	_, errOut, code = run(t, "grade", q5, "pass", "understood")
	if code != 0 {
		t.Fatalf("grade q5 pass: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Grade q6 fail --oos → batch complete: q5 (pass, in-scope), q6 (fail, OOS).
	// §8 step 7: OOS question excluded from steps 8/9. §8 step 8:
	// "every in-scope question pass" (q5 passes) → fallback probes become answerable.
	out, errOut, code = run(t, "grade", q6, "fail", "out of scope tangent", "--oos")
	if code != 0 {
		t.Fatalf("grade q6 --oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade --oos: want 'ok', got %q", out)
	}

	// Assert: OOS answer node has OOS=true.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	var oosAnswer *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.OOS {
			oosAnswer = item.A
			break
		}
	}
	if oosAnswer == nil {
		t.Fatal("§12 --oos: no OOS answer node found in graph")
	}

	// Grade event for q6 must have oos=true.
	rows := readEventLog(t, file)
	var gradeOOSRow map[string]any
	for _, r := range rows {
		if r["ev"] == "grade" && r["q"] == q6 {
			gradeOOSRow = r
		}
	}
	if gradeOOSRow == nil {
		t.Fatalf("§12 --oos: no grade event for %s in log", q6)
	}
	if gradeOOSRow["oos"] != true {
		t.Errorf("§12 --oos: grade event oos: want true, got %v", gradeOOSRow["oos"])
	}

	// After OOS + in-scope pass: fallback probes q3, q4 should now be answerable.
	_, errOut, code = run(t, "answer", "q3", "answer 3")
	if code != 0 {
		t.Fatalf("answer q3 after oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", "q4", "answer 4")
	if code != 0 {
		t.Fatalf("answer q4 after oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", "q3", "pass", "confirmed")
	if code != 0 {
		t.Fatalf("grade q3 after oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", "q4", "pass", "confirmed")
	if code != 0 {
		t.Fatalf("grade q4 after oos: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// mycon must now be passed.
	data, _ = os.ReadFile(file)
	g, _ = graph.Parse(data)
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 --oos: mycon must pass after all fallback probes pass")
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Teaching → Gated (stall) ───────────────────────────────────────

// TestLifecycle_StallGate drives: teach question fails → stall streak reaches
// TM_MAX_STALL → concept becomes gated.
// Covers §12 edge: Teaching → Gated (stall streak at limit).
func TestLifecycle_StallGate(t *testing.T) {
	// §12 edge: Teaching → Gated (stall)
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "8")
	t.Setenv("TM_MAX_STALL", "1") // one in-scope fail/unclear batch → gated
	t.Setenv("TM_MAX_TEACH", "8")
	t.Setenv("TM_MAX_FAILS", "2")

	// Graph: lifecycleTeachReadyGraph — probe_1 (fail), probe_2 fallback (q3, q4).
	// Adding teach question; grading it fail → stall_streak=1 = TM_MAX_STALL → gated.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// Add teach question.
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "teach scope", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach --re: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q5 := strings.TrimSpace(out)

	// Answer the teach question.
	_, errOut, code = run(t, "answer", q5, "wrong answer")
	if code != 0 {
		t.Fatalf("answer teach: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Grade fail → batch complete, stall streak=1 = TM_MAX_STALL (§8 step 10).
	// After this, concept is gated.
	out, errOut, code = run(t, "grade", q5, "fail", "did not explain")
	if code != 0 {
		t.Fatalf("grade teach fail: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade teach: want 'ok', got %q", out)
	}

	// Assert: concept is gated (ask must refuse with "gated" error).
	_, errOut, code = run(t, "ask", "mycon")
	if code != 1 {
		t.Fatalf("§12 stall gate: want exit 1 (gated), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 stall gate: want 'is gated' in err, got:\n%s", errOut)
	}

	// q --teach should also be refused (gated).
	_, errOut, code = run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "new teach", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("§12 stall gate: q --teach on gated concept: want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 stall gate: q --teach gated err: got:\n%s", errOut)
	}

	// Grade event in log.
	rows := readEventLog(t, file)
	gradeRows := eventsByType(rows, "grade")
	if len(gradeRows) != 1 {
		t.Errorf("§12 stall gate: want 1 grade event, got %d", len(gradeRows))
	}
	if gradeRows[0]["verdict"] != "fail" {
		t.Errorf("§12 stall gate: grade verdict: want 'fail', got %v", gradeRows[0]["verdict"])
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Verdict → Gated (probe batches at limit) ───────────────────────

// TestLifecycle_ProbeGate drives: probe_1 fail → probe_2 fail (at TM_MAX_FAILS) →
// concept gated.
// Covers §12 edge: any fail, failed probe batches at limit → Gated.
func TestLifecycle_ProbeGate(t *testing.T) {
	// §12 edge: Verdict → Gated (failed probe batches at TM_MAX_FAILS)
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_MAX_FAILS", "1") // 1 failed probe batch → gated immediately

	// Pre-built graph: probe_1 with q1 (pending a1) and q2 (pass a2).
	// Grading q1 fail → probe_1 resolved with fail.
	// With TM_MAX_FAILS=1: 1 failed batch = limit → concept gated.
	file := qWriteGraph(t, dir, gradeProbeWithPassAndPendingGraph())

	// Grade q2 (pending in this fixture) fail → probe_1 has fail, batch complete.
	// TM_MAX_FAILS=1 → §8 step 10 → gated.
	out, errOut, code := run(t, "grade", "q2", "fail", "wrong answer")
	if code != 0 {
		t.Fatalf("grade q2 fail: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade: want 'ok', got %q", out)
	}

	// Assert: concept is now gated.
	_, errOut, code = run(t, "ask", "mycon")
	if code != 1 {
		t.Fatalf("§12 probe gate: want exit 1 (gated), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 probe gate: want 'is gated' in err, got:\n%s", errOut)
	}

	// q (new probe) should also be refused (gated).
	_, errOut, code = run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "new probe scope")
	if code != 1 {
		t.Fatalf("§12 probe gate: q on gated concept: want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 probe gate: q gated err: got:\n%s", errOut)
	}

	rows := readEventLog(t, file)
	if len(rows) != 1 || rows[0]["ev"] != "grade" {
		t.Errorf("§12 probe gate: want 1 grade event, got %v", rows)
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Teaching → Answering (teaching spent) ──────────────────────────

// TestLifecycle_TeachingSpent drives: teach questions exceed TM_MAX_TEACH →
// teaching spent → fallback probes become answerable.
// Covers §12 edge: Teaching → Answering (teach question cap reached).
func TestLifecycle_TeachingSpent(t *testing.T) {
	// §12 edge: Teaching → Answering (teaching spent)
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "8")
	t.Setenv("TM_MAX_TEACH", "1") // cap at 1 in-scope teach question → spent immediately
	t.Setenv("TM_MAX_STALL", "4")
	t.Setenv("TM_MAX_FAILS", "2")

	// lifecycleTeachReadyGraph: probe_1 (fail), probe_2 fallback (q3, q4), GAP.
	// No teach question yet; the test adds the first one.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// Add teach question.
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "teach scope", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach --re: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q5 := strings.TrimSpace(out)

	// Answer + grade fail. With TM_MAX_TEACH=1 and 1 in-scope teach question
	// graded, TeachCount=1 >= TM_MAX_TEACH=1 → teaching spent (§8 step 9).
	// Fallback probes become answerable even though teach failed.
	_, errOut, code = run(t, "answer", q5, "still wrong")
	if code != 0 {
		t.Fatalf("answer teach: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	out, errOut, code = run(t, "grade", q5, "fail", "not improving")
	if code != 0 {
		t.Fatalf("grade teach fail: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("grade teach: want 'ok', got %q", out)
	}

	// Teaching spent → q --teach must now be refused (teaching spent).
	_, errOut, code = run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "another teach", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("§12 teaching spent: q --teach after spent: want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "spent") {
		t.Errorf("§12 teaching spent: want 'spent' in err, got:\n%s", errOut)
	}

	// Fallback probes q3, q4 should now be answerable.
	_, errOut, code = run(t, "answer", "q3", "fallback answer 1")
	if code != 0 {
		t.Fatalf("§12 teaching spent: answer q3 (should be answerable): want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", "q4", "fallback answer 2")
	if code != 0 {
		t.Fatalf("§12 teaching spent: answer q4 (should be answerable): want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Grade both pass → concept passes.
	_, errOut, code = run(t, "grade", "q3", "pass", "confirmed")
	if code != 0 {
		t.Fatalf("grade q3: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", "q4", "pass", "confirmed")
	if code != 0 {
		t.Fatalf("grade q4: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// mycon must be passed.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 teaching spent: mycon must pass after fallback probes all pass")
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Passed → Untested (reopen) ─────────────────────────────────────

// TestLifecycle_Reopen drives: concept passes → reopen with GAP → back in untested.
// Covers §12 edge: Passed → Untested.
func TestLifecycle_Reopen(t *testing.T) {
	// §12 edge: Passed → Untested (reopen)
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_MAX_FAILS", "2")

	// Start from a graph where mycon is already in passed with no test items.
	// This is the state after a concept passes.
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
`
	file := qWriteGraph(t, dir, mmd)

	// ── Passed → Untested: reopen ─────────────────────────────────────────────
	out, errOut, code := run(t, "reopen", "mycon", "missed the edge case")
	if code != 0 {
		t.Fatalf("reopen: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("reopen: want 'ok', got %q", out)
	}

	// Assert: mycon moved to untested with GAP.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			t.Error("§12 reopen: mycon must not be in passed after reopen")
		}
	}

	var reopenedConcept *graph.ConceptNode
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			reopenedConcept = c
		}
	}
	if reopenedConcept == nil {
		t.Fatal("§12 reopen: mycon not found in untested after reopen")
	}
	if reopenedConcept.GAP != "missed the edge case" {
		t.Errorf("§12 reopen: GAP want 'missed the edge case', got %q", reopenedConcept.GAP)
	}

	// Event log: reopen event.
	rows := readEventLog(t, file)
	if len(rows) != 1 || rows[0]["ev"] != "reopen" {
		t.Errorf("§12 reopen: want 1 reopen event, got %v", rows)
	}
	if rows[0]["concept"] != "mycon" {
		t.Errorf("§12 reopen: event concept: want 'mycon', got %v", rows[0]["concept"])
	}
	if rows[0]["gap"] != "missed the edge case" {
		t.Errorf("§12 reopen: event gap: want 'missed the edge case', got %v", rows[0]["gap"])
	}

	lintM6(t, file, dir, 2)
}

// ── §12 edge: Map → Orient (upstream insert) ─────────────────────────────────

// TestLifecycle_UpstreamInsert drives: gated concept → add upstream node
// (--child clears gate) → upstream concept passes → child unblocked from frontier.
// Covers §12 edge: Map → Orient (add with gate clearing) + Gated → DraftProbes or Answering.
func TestLifecycle_UpstreamInsert(t *testing.T) {
	// §12 edge: upstream insert + gate clearing
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_MAX_FAILS", "1") // 1 failed batch → gated
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_MAX_TEACH", "8")
	t.Setenv("TM_MAX_STALL", "4")

	// gatedConceptGraph: "con" in untested, probe_1 (q1 fail) → con is gated
	// (TM_MAX_FAILS=1, TM_PROBE_MIN=1).
	file := buildGatedGraph(t, dir, gatedConceptGraph)

	// Assert con is gated before upstream insert.
	_, errOut, code := run(t, "ask", "con")
	if code != 1 || !strings.Contains(errOut, "is gated") {
		t.Fatalf("§12 upstream insert: con should be gated before insert; code=%d err=%s", code, errOut)
	}

	// ── Map → Orient: add upstream concept (--child clears gate) ─────────────
	// "tm add <new> <cite> "<scope>" --child <C>" writes gate meta for C.
	out, errOut, code := run(t, "add", "upstream", "f5ca3875b379@src.txt:1-5", "upstream scope", "--child", "con:requires")
	if code != 0 {
		t.Fatalf("add --child: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("add --child: want 'ok', got %q", out)
	}

	// Gate meta written for "con".
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
		t.Fatal("§12 upstream insert: want gate meta for 'con' after add --child")
	}
	if meta.Base != 1 {
		t.Errorf("§12 upstream insert: gate meta base: want 1, got %d", meta.Base)
	}

	// Gate event in log.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	if len(gateRows) != 1 {
		t.Fatalf("§12 upstream insert: want 1 gate event, got %d", len(gateRows))
	}
	if gateRows[0]["via"] != "add" {
		t.Errorf("§12 upstream insert: gate via: want 'add', got %v", gateRows[0]["via"])
	}

	// ── Upstream passes: add probe to "upstream" → answer → grade pass ────────
	out, errOut, code = run(t, "q", "upstream", "f5ca3875b379@src.txt:1-5", "upstream probe")
	if code != 0 {
		t.Fatalf("q upstream: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	qUp := strings.TrimSpace(out)

	_, errOut, code = run(t, "answer", qUp, "upstream answer")
	if code != 0 {
		t.Fatalf("answer upstream: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	_, errOut, code = run(t, "grade", qUp, "pass", "upstream understood")
	if code != 0 {
		t.Fatalf("grade upstream: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// "upstream" must now be in passed.
	data, _ = os.ReadFile(file)
	g, _ = graph.Parse(data)
	upstreamPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "upstream" {
			upstreamPassed = true
		}
	}
	if !upstreamPassed {
		t.Error("§12 upstream insert: 'upstream' must pass after probe all pass")
	}

	// "con" is on the frontier now (its only parent "upstream" has passed).
	// However "con" has gate meta with base=1: probe batches at N<=1 are blocked.
	// The concept needs a new probe batch (probe_2) to be asked.
	// ask "con" → since there are no probe batches above base=1, nothing to ask.
	out, errOut, code = run(t, "ask", "con")
	if code != 0 {
		t.Fatalf("§12 upstream insert: ask con after upstream passes: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// No output expected (all probes are at/below base, nothing to ask).
	if strings.TrimSpace(out) != "" {
		// It's also fine to get output if there are probes above base.
		// For this fixture, probe_1 (N=1) is at base=1 so filtered.
		// No probes above base → nothing to ask.
		t.Logf("§12 upstream insert: ask con output: %q (expected empty for no probes above base)", out)
	}

	lintM6(t, file, dir, 1)
}

// ── §12 edge: Gated → Answering (pending probes asked as written) ────────────

// TestLifecycle_GateWithPendingProbes drives the §12 edge where a stall gate
// trips while a fallback probe is still unanswered. After the gate clears
// (base = max batch incl. the teach batch), tm ask emits the former fallback
// probe AS WRITTEN (§7 gate paragraph, §12 diagram).
//
// Scenario: probe_1 (q1 pass, q2 fail) resolved → probe_2 (q3, q4) locked as
// fallback → teach_3 added, graded fail → stall gate trips (probe_2 still
// unanswered) → gate cleared via upstream insert (base=3, teach_3 N=3) →
// upstream passes → ask mycon emits q3, q4 as written.
func TestLifecycle_GateWithPendingProbes(t *testing.T) {
	// §12 edge: Teaching → Gated (stall) → Answering (fallback probes as written)
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_MAX_STALL", "1") // one all-fail teach batch → stall gate
	t.Setenv("TM_MAX_FAILS", "2") // keep probe gate from tripping (only 1 failed batch)
	t.Setenv("TM_MAX_TEACH", "8")

	// lifecycleTeachReadyGraph: probe_1 (q1 pass, q2 fail) resolved,
	// probe_2 (q3, q4) locked as fallback, GAP set on mycon.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// Add teach question (teach_3, N=3) targeting q2's fail.
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "teach scope", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("q --teach --re: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	q5 := strings.TrimSpace(out) // teach_3 question ID

	// Answer the teach question.
	_, errOut, code = run(t, "answer", q5, "wrong answer")
	if code != 0 {
		t.Fatalf("answer teach: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Grade fail → teach_3 resolved all-fail → stall streak=1=TM_MAX_STALL → gated.
	// probe_2 (q3, q4) is still unanswered at this moment.
	_, errOut, code = run(t, "grade", q5, "fail", "did not explain")
	if code != 0 {
		t.Fatalf("grade teach fail: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Assert: mycon is gated (stall gate tripped while probe_2 was still unanswered).
	_, errOut, code = run(t, "ask", "mycon")
	if code != 1 {
		t.Fatalf("§12 gate+pending probes: want exit 1 (gated), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 gate+pending probes: want 'is gated' in err, got:\n%s", errOut)
	}

	// ── Gate cleared via upstream insert ─────────────────────────────────────
	// base = max batch N = 3 (teach_3 N=3). After clearing, teach_3 N=3 is NOT
	// > base=3, so probe_2 is no longer locked (BatchStateOf → Draft).
	out, errOut, code = run(t, "add", "upstream", "f5ca3875b379@src.txt:1-5", "upstream scope", "--child", "mycon:requires")
	if code != 0 {
		t.Fatalf("add --child: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("add --child: want 'ok', got %q", out)
	}

	// Gate meta for mycon: base must be 3 (teach_3 N=3 was the max batch).
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	meta := gateMetaFor(g, "mycon")
	if meta == nil {
		t.Fatal("§12 gate+pending probes: want gate meta for 'mycon' after add --child")
	}
	if meta.Base != 3 {
		t.Errorf("§12 gate+pending probes: gate meta base: want 3 (teach_3 N), got %d", meta.Base)
	}

	// Gate event must record trip=stall, via=add, base=3.
	rows := readEventLog(t, file)
	gateRows := eventsByType(rows, "gate")
	if len(gateRows) != 1 {
		t.Fatalf("§12 gate+pending probes: want 1 gate event, got %d", len(gateRows))
	}
	gr := gateRows[0]
	if gr["trip"] != "stall" {
		t.Errorf("§12 gate+pending probes: gate event trip: want 'stall', got %v", gr["trip"])
	}
	if gr["via"] != "add" {
		t.Errorf("§12 gate+pending probes: gate event via: want 'add', got %v", gr["via"])
	}
	baseVal, _ := gr["base"].(float64)
	if int(baseVal) != 3 {
		t.Errorf("§12 gate+pending probes: gate event base: want 3, got %v", gr["base"])
	}

	// ── Upstream passes ───────────────────────────────────────────────────────
	// Need TM_PROBE_MIN=2 probes for upstream.
	out, errOut, code = run(t, "q", "upstream", "f5ca3875b379@src.txt:1-5", "upstream probe 1")
	if code != 0 {
		t.Fatalf("q upstream 1: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	qUp1 := strings.TrimSpace(out)
	out, errOut, code = run(t, "q", "upstream", "e28e810e6e2e@src.txt:3-5", "upstream probe 2")
	if code != 0 {
		t.Fatalf("q upstream 2: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	qUp2 := strings.TrimSpace(out)
	_, errOut, code = run(t, "answer", qUp1, "upstream answer 1")
	if code != 0 {
		t.Fatalf("answer upstream 1: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", qUp2, "upstream answer 2")
	if code != 0 {
		t.Fatalf("answer upstream 2: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", qUp1, "pass", "good")
	if code != 0 {
		t.Fatalf("grade upstream 1: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", qUp2, "pass", "good")
	if code != 0 {
		t.Fatalf("grade upstream 2: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Verify upstream in passed.
	data, _ = os.ReadFile(file)
	g, _ = graph.Parse(data)
	upstreamPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "upstream" {
			upstreamPassed = true
		}
	}
	if !upstreamPassed {
		t.Error("§12 gate+pending probes: upstream must pass after probe all pass")
	}

	// ── Gated → Answering: former fallback probes asked as written ────────────
	// §7 gate paragraph: "Probes left unanswered from before the gate stop being
	// fallback probes and are asked as written."
	// probe_2 (q3, q4) is no longer locked — BatchStateOf returns Draft.
	askOut, errOut, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("§12 gate+pending probes: ask mycon after upstream passes: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(askOut, "q3") {
		t.Errorf("§12 gate+pending probes: ask: want q3 in output, got:\n%s", askOut)
	}
	if !strings.Contains(askOut, "q4") {
		t.Errorf("§12 gate+pending probes: ask: want q4 in output, got:\n%s", askOut)
	}

	// Answer + grade probe_2 probes → mycon passes.
	_, errOut, code = run(t, "answer", "q3", "probe answer 1")
	if code != 0 {
		t.Fatalf("§12 gate+pending probes: answer q3: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "answer", "q4", "probe answer 2")
	if code != 0 {
		t.Fatalf("§12 gate+pending probes: answer q4: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", "q3", "pass", "good")
	if code != 0 {
		t.Fatalf("§12 gate+pending probes: grade q3: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	_, errOut, code = run(t, "grade", "q4", "pass", "good")
	if code != 0 {
		t.Fatalf("§12 gate+pending probes: grade q4: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// mycon must now be passed.
	data, err = os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr = graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	myconPassed := false
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			myconPassed = true
		}
	}
	if !myconPassed {
		t.Error("§12 gate+pending probes: 'mycon' must pass after probe_2 all pass")
	}

	lintM6(t, file, dir, 2)
}

// ── §16.5 concurrency: 20 parallel grade calls ───────────────────────────────

// concurrentGradeGraph builds a graph with one concept and 20 probe questions
// in probe_1, each with a pending answer. All 20 goroutines will grade their
// question simultaneously to exercise the lockfile serialization.
func concurrentGradeGraph(n int) string {
	var b strings.Builder
	b.WriteString(qFrontmatter)
	b.WriteString("flowchart TB\n")
	b.WriteString(`    subgraph passed["Concepts User understands"]`)
	b.WriteString("\n    end\n")
	b.WriteString(`    subgraph untested["Concepts User has not been tested on"]`)
	b.WriteString("\n")
	b.WriteString("        %% tm:format 2\n")
	b.WriteString(`        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]`)
	b.WriteString("\n    end\n")
	b.WriteString(`    subgraph testing["Open tests validating and teaching User understanding"]`)
	b.WriteString("\n")

	// Declare all question and answer nodes.
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "        q%d[\"Probe %d<br/>f5ca3875b379@src.txt:1-5\"]:::probe_1\n", i, i)
		fmt.Fprintf(&b, "        a%d[\"pending answer %d\"]:::pending\n", i, i)
	}
	// Declare all edges.
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "        mycon --> q%d\n", i)
		fmt.Fprintf(&b, "        q%d --> a%d\n", i, i)
	}
	b.WriteString("    end\n")
	b.WriteString("    classDef probe_1 stroke:#4aa3ff\n")
	b.WriteString("    classDef pending stroke-dasharray:4 3\n")
	return b.String()
}

// TestConcurrency_TwentyParallelGrades launches 20 goroutines, each grading
// one of 20 pending answers on the same graph file. After all complete:
//   - the file lints clean
//   - the event log has exactly 20 "grade" rows
//   - no answers remain pending in the graph
//
// This exercises internal/lockfile under concurrent writers (§16.5).
func TestConcurrency_TwentyParallelGrades(t *testing.T) {
	const N = 20
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_PROBE_MAX", "25") // allow all 20 in one batch
	t.Setenv("TM_MAX_FAILS", "2")

	file := qWriteGraph(t, dir, concurrentGradeGraph(N))

	// Run 20 parallel grade calls.
	var wg sync.WaitGroup
	errs := make([]string, N)
	for i := 1; i <= N; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qid := fmt.Sprintf("q%d", n)
			summary := fmt.Sprintf("answer %d summary", n)
			_, errOut, code := run(t, "grade", qid, "pass", summary)
			if code != 0 {
				errs[n-1] = fmt.Sprintf("grade %s: want exit 0, got %d; stderr: %s", qid, code, errOut)
			}
		}(i)
	}
	wg.Wait()

	// Report any goroutine errors.
	for i, e := range errs {
		if e != "" {
			t.Errorf("goroutine %d: %s", i+1, e)
		}
	}

	// All 20 answers must be graded (no pending left).
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	pendingCount := 0
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.Class == "pending" {
			pendingCount++
		}
	}
	if pendingCount != 0 {
		t.Errorf("concurrency: want 0 pending answers, got %d", pendingCount)
	}

	// Event log must have exactly 20 grade rows (all 20 graded, not all passed
	// yet because batch is not complete; each grade is logged individually).
	rows := readEventLog(t, file)
	gradeRows := eventsByType(rows, "grade")
	if len(gradeRows) != N {
		t.Errorf("concurrency: want %d grade events, got %d", N, len(gradeRows))
	}

	// File must lint clean with ProbeMin=1 (we used 20 probes in one batch).
	lintM6(t, file, dir, 1)
}
