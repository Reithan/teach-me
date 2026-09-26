package cli_test

// Tests for tm answer --concede (M14f).
//
// --concede records the answer as class=fail with summary "conceded" in one
// mutation. The answer event gains concede:true; the grade event gains
// via:"concede". No grader sub-agent is invoked.
//
// Table-driven unit tests cover: happy path, event ordering and fields,
// refusals identical to plain answer, and the grade-on-conceded-question
// refusal. The lifecycle test drives the full concede-to-gate path.

import (
	"os"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Unit tests ─────────────────────────────────────────────────────────────────

// TestConcede_HappyPath_Probe verifies that --concede on an unanswered probe
// question writes the answer node as class=fail with label "conceded" and
// the graph passes lint.
func TestConcede_HappyPath_Probe(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	out, errOut, code := run(t, "answer", "q1", "I don't know", "--concede")
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

	// a1 must exist with class=fail and label="conceded".
	var a1 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a1" {
			a1 = item.A
			break
		}
	}
	if a1 == nil {
		t.Fatal("a1 not found in testing block")
	}
	if a1.Class != "fail" {
		t.Errorf("a1.Class: want 'fail', got %q", a1.Class)
	}
	if a1.Label != "conceded" {
		t.Errorf("a1.Label: want 'conceded', got %q", a1.Label)
	}

	// Edge q1 → a1 must exist.
	edgeFound := false
	for _, e := range g.Edges {
		if e.From == "q1" && e.To == "a1" {
			edgeFound = true
			break
		}
	}
	if !edgeFound {
		t.Error("edge q1→a1 not found")
	}

	lintFile(t, file, dir)
}

// TestConcede_EventFields verifies both the answer and grade events are
// written in one mutation with the correct §10 fields.
func TestConcede_EventFields(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	rawText := "I don't know this at all"
	askedText := "how does X work?"

	_, _, code := run(t, "answer", "q1", rawText, "--asked", askedText, "--concede")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}

	rows := readEventLog(t, file)
	if len(rows) < 2 {
		t.Fatalf("want at least 2 events (answer + grade), got %d", len(rows))
	}

	answerRow := rows[len(rows)-2]
	gradeRow := rows[len(rows)-1]

	// Answer event fields.
	if answerRow["ev"] != "answer" {
		t.Errorf("answerRow ev: want 'answer', got %v", answerRow["ev"])
	}
	if answerRow["q"] != "q1" {
		t.Errorf("answerRow q: want 'q1', got %v", answerRow["q"])
	}
	if answerRow["raw"] != rawText {
		t.Errorf("answerRow raw: want %q, got %v", rawText, answerRow["raw"])
	}
	if answerRow["asked"] != askedText {
		t.Errorf("answerRow asked: want %q, got %v", askedText, answerRow["asked"])
	}
	if concede, ok := answerRow["concede"]; !ok || concede != true {
		t.Errorf("answerRow concede: want true, got %v (ok=%v)", concede, ok)
	}

	// Grade event fields.
	if gradeRow["ev"] != "grade" {
		t.Errorf("gradeRow ev: want 'grade', got %v", gradeRow["ev"])
	}
	if gradeRow["q"] != "q1" {
		t.Errorf("gradeRow q: want 'q1', got %v", gradeRow["q"])
	}
	if gradeRow["verdict"] != "fail" {
		t.Errorf("gradeRow verdict: want 'fail', got %v", gradeRow["verdict"])
	}
	if gradeRow["recorded"] != "fail" {
		t.Errorf("gradeRow recorded: want 'fail', got %v", gradeRow["recorded"])
	}
	if gradeRow["summary"] != "conceded" {
		t.Errorf("gradeRow summary: want 'conceded', got %v", gradeRow["summary"])
	}
	if gradeRow["raw"] != rawText {
		t.Errorf("gradeRow raw: want %q, got %v", rawText, gradeRow["raw"])
	}
	if via, ok := gradeRow["via"]; !ok || via != "concede" {
		t.Errorf("gradeRow via: want 'concede', got %v (ok=%v)", via, ok)
	}
	// guided and oos must be false (not set on the concede path).
	if gradeRow["guided"] != false {
		t.Errorf("gradeRow guided: want false, got %v", gradeRow["guided"])
	}
	if gradeRow["oos"] != false {
		t.Errorf("gradeRow oos: want false, got %v", gradeRow["oos"])
	}
}

// TestConcede_NormalAnswer_NoConcede verifies that a plain answer (without
// --concede) does NOT gain the concede field in the answer event.
func TestConcede_NormalAnswer_NoConcede(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	_, _, code := run(t, "answer", "q1", "my answer")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}

	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("no events")
	}
	last := rows[len(rows)-1]
	if last["ev"] != "answer" {
		t.Errorf("ev: want 'answer', got %v", last["ev"])
	}
	if _, ok := last["concede"]; ok {
		t.Errorf("concede field must be absent on plain answer; got %v", last["concede"])
	}
}

// TestConcede_Refusals_TableDriven checks that the refusals for --concede are
// identical to plain answer: unknown qid (exit 3), already has an answer
// (exit 1), and grader role forbidden (exit 1).
func TestConcede_Refusals_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T)
		args     []string
		wantCode int
		wantErr  string
	}{
		{
			name:     "unknown qid",
			setup:    func(_ *testing.T) {},
			args:     []string{"answer", "q99", "I don't know", "--concede"},
			wantCode: 3,
			wantErr:  `unknown question "q99"`,
		},
		{
			name:     "already has an answer",
			setup:    func(_ *testing.T) {},
			args:     []string{"answer", "q1", "I don't know", "--concede"},
			wantCode: 1,
			wantErr:  "q1 already has an answer",
		},
		{
			name: "grader role forbidden",
			setup: func(t *testing.T) {
				t.Setenv("TM_ROLE", "grader")
			},
			args:     []string{"answer", "q1", "I don't know", "--concede"},
			wantCode: 1,
			wantErr:  "answer is not available when TM_ROLE=grader",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, errPath := qSetupDir(t)
			if tc.name == "already has an answer" {
				qWriteGraph(t, dir, answerAlreadyAnsweredGraph())
			} else {
				qWriteGraph(t, dir, answerProbeGraph())
			}
			tc.setup(t)
			_, errOut, code := run(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("want exit %d, got %d; stderr:\n%s", tc.wantCode, code, errOut)
			}
			if !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want err containing %q; got:\n%s", tc.wantErr, errOut)
			}
			rows := readErrlog(t, errPath)
			if len(rows) != 1 || rows[0].Exit != tc.wantCode {
				t.Errorf("ERRORS.jsonl: want 1 row exit %d; got %v", tc.wantCode, rows)
			}
		})
	}
}

// TestConcede_GradeRefusesAfterConcede verifies that after a question is
// recorded via --concede (answer class=fail), tm grade refuses with exit 1
// because there is no pending answer.
func TestConcede_GradeRefusesAfterConcede(t *testing.T) {
	dir, _ := qSetupDir(t)
	qWriteGraph(t, dir, answerProbeGraph())

	// Concede q1.
	_, _, code := run(t, "answer", "q1", "I don't know", "--concede")
	if code != 0 {
		t.Fatalf("concede: want exit 0, got %d", code)
	}

	// Now try to grade q1 → exit 1 (no pending answer).
	_, errOut, code := run(t, "grade", "q1", "pass", "should not work")
	if code != 1 {
		t.Fatalf("grade after concede: want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "no pending answer") {
		t.Errorf("want 'no pending answer' err; got:\n%s", errOut)
	}
}

// ── Lifecycle test ─────────────────────────────────────────────────────────────

// TestLifecycle_Concede_TeachingRoundThenGate drives the full concede path:
//
//  1. Start from lifecycleTeachReadyGraph: probe_1 already resolved fail,
//     probe_2 fallback (q3, q4) unanswered, GAP set. Teaching round is open.
//  2. Assert: fallback probes are locked ("teaching round not complete").
//  3. Assert: adding a teach question succeeds ("teach allowed").
//  4. Concede the teach question (TM_MAX_TEACH=1 → teaching spent).
//  5. Assert: fallback probes (q3, q4) are now answerable.
//  6. Concede all probes in probe_2 (q3, q4) → second fail batch.
//     TM_MAX_FAILS=2 → gate trips.
//  7. Assert: ask mycon exits 1 with "is gated".
//
// Covers §12 edges: Verdict→DraftRound, Teaching→Answering (teaching spent),
// Verdict→Gated.
func TestLifecycle_Concede_TeachingRoundThenGate(t *testing.T) {
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	t.Setenv("TM_PROBE_MAX", "5")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "8")
	t.Setenv("TM_MAX_FAILS", "2")  // gate after 2 failed probe batches
	t.Setenv("TM_MAX_STALL", "99") // avoid stall gate during this test
	t.Setenv("TM_MAX_TEACH", "1")  // one teach question exhausts teaching

	// lifecycleTeachReadyGraph has probe_1 (q1=pass, q2=fail, RESOLVED) and
	// probe_2 fallback (q3, q4 unanswered). GAP is set on mycon.
	file := qWriteGraph(t, dir, lifecycleTeachReadyGraph())

	// ── Step 2: fallback probes must be locked (teaching round open) ──────────
	_, errOut, code := run(t, "answer", "q3", "trying anyway")
	if code != 1 {
		t.Fatalf("§12: fallback must be locked; want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "teaching round") {
		t.Errorf("§12: want 'teaching round' in err; got:\n%s", errOut)
	}

	// ── Step 3: adding a teach question must succeed ──────────────────────────
	out, errOut, code := run(t, "q", "mycon", "cd3f27ccd149@src.txt:1-3", "the missed insight", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("§12: add teach question: want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	teachQID := strings.TrimSpace(out)
	if teachQID == "" {
		t.Fatal("§12: add teach question: want question ID, got empty")
	}

	// ── Step 4: concede the teach question (TM_MAX_TEACH=1 → teaching spent) ──
	_, errOut, code = run(t, "answer", teachQID, "I don't know", "--concede")
	if code != 0 {
		t.Fatalf("§12: concede teach: want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// ── Step 5: concede q3 (first fallback probe) ────────────────────────────
	// After teaching spent, probe_2 is answerable. Concede q3 → probe_2 now
	// has one fail answer. State: FailedProbeBatches=[probe_1, probe_2] (2 ≥
	// TM_MAX_FAILS=2) → concept gated in the derived state after this mutation.
	_, errOut, code = run(t, "answer", "q3", "I don't know", "--concede")
	if code != 0 {
		t.Fatalf("§12: concede q3 (fallback): want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// ── Step 6: gate is now tripped; assert the gate refusal ──────────────────
	// The concept is gated in the derived state (2 failed probe batches =
	// TM_MAX_FAILS). Attempting to concede q4 hits the gated check.
	_, errOut, code = run(t, "answer", "q4", "I don't know", "--concede")
	if code != 1 {
		t.Fatalf("§12 concede gate: want exit 1 (gated), got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 concede gate: want 'is gated'; got:\n%s", errOut)
	}

	// ask mycon also refuses (gated).
	_, errOut, code = run(t, "ask", "mycon")
	if code != 1 {
		t.Fatalf("§12 concede gate: ask on gated: want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "is gated") {
		t.Errorf("§12 concede gate: ask on gated err; got:\n%s", errOut)
	}

	// ── Verify event log concede fields ──────────────────────────────────────
	// The fixture already has q/grade events for q1..q4 (from the initial
	// probe_1 pass/fail). We added: q event for teachQID, then conceded
	// teachQID and q3 → 2 answer events + 2 grade events.
	rows := readEventLog(t, file)
	var answerRows, gradeRows []map[string]any
	for _, r := range rows {
		switch r["ev"] {
		case "answer":
			answerRows = append(answerRows, r)
		case "grade":
			gradeRows = append(gradeRows, r)
		}
	}
	// 2 conceded questions → 2 answer events + 2 grade events.
	if len(answerRows) != 2 {
		t.Errorf("want 2 answer events (teach + q3 conceded), got %d", len(answerRows))
	}
	if len(gradeRows) != 2 {
		t.Errorf("want 2 grade events, got %d", len(gradeRows))
	}
	for _, ar := range answerRows {
		if ar["concede"] != true {
			t.Errorf("answer event concede: want true, got %v (row: %v)", ar["concede"], ar)
		}
	}
	for _, gr := range gradeRows {
		if gr["via"] != "concede" {
			t.Errorf("grade event via: want 'concede', got %v (row: %v)", gr["via"], gr)
		}
	}

	lintM6(t, file, dir, 2)
}
