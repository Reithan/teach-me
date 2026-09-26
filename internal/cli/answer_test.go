package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Graph fixtures ─────────────────────────────────────────────────────────────

// answerProbeGraph: probe_1 with q1, q2 unanswered (2 questions satisfies
// default ProbeMin=2). No failed batches, no teach batches.
func answerProbeGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        mycon --> q1
        mycon --> q2
    end
    classDef probe_1 stroke:#4aa3ff
`
}

// answerAlreadyAnsweredGraph: probe_1 with q1 (pending a1), q2 unanswered.
// Trying to answer q1 again should fail.
func answerAlreadyAnsweredGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a1["already pending answer"]:::pending
        mycon --> q1
        mycon --> q2
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
`
}

// answerBlockedParentGraph: child_con has parent_con (not passed) as parent.
// probe_1 under child_con has q1, q2.
// Concept-to-concept edge uses labeled format (lint requirement).
func answerBlockedParentGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        parent_con["Parent concept<br/>f5ca3875b379@src.txt:1-5"]
        child_con["Child concept<br/>f5ca3875b379@src.txt:1-5"]
        parent_con --"requires"--> child_con
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        child_con --> q1
        child_con --> q2
    end
    classDef probe_1 stroke:#4aa3ff
`
}

// ── Exit 3 tests ──────────────────────────────────────────────────────────────

func TestAnswer_UnknownQID_Exit3(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, answerProbeGraph())

	_, errOut, code := run(t, "answer", "q99", "some answer")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, `err: unknown question "q99"`) {
		t.Errorf("want unknown question err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 3; got %v", rows)
	}
}

// ── Exit 1 tests ──────────────────────────────────────────────────────────────

func TestAnswer_AlreadyHasAnswer_Exit1(t *testing.T) {
	// q1 already has a pending answer in the graph.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, answerAlreadyAnsweredGraph())

	_, errOut, code := run(t, "answer", "q1", "another attempt")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 already has an answer") {
		t.Errorf("want 'already has an answer' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_BlockedParent_Exit1(t *testing.T) {
	// child_con has parent_con (not passed) as parent.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, answerBlockedParentGraph())

	_, errOut, code := run(t, "answer", "q1", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: parent parent_con is not passed") {
		t.Errorf("want 'parent not passed' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_Gated_Exit1(t *testing.T) {
	// MaxFails=1 + probe_1 resolved with fail → Gated=true.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_MAX_FAILS", "1")
	qWriteGraph(t, dir, qReNotUnclearGraph())

	// q1 is in probe_1 which is already resolved (graded). q1 already has a
	// pass answer in qReNotUnclearGraph. Use a separate qid that doesn't exist...
	// Actually, in qReNotUnclearGraph all q/a pairs have answers. Let's use a
	// different graph: one probe resolved with fail, and add a new question.
	// We'll just check that the gated error fires before we'd get to min-count.
	// Use qReNotUnclearGraph but try to answer an already-answered question first;
	// the gated check fires before the "already has answer" check.
	// Actually, per the apply() ordering: unknown qid → already answered → concept
	// → parent → gated. So gated only fires if question exists and is unanswered.
	// qReNotUnclearGraph has all questions answered. Let me build a custom graph.
	t.Cleanup(func() {}) // no-op, t.Setenv already handles cleanup

	// Use a graph with probe_1 resolved with fail but probe_2 unanswered:
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["wrong answer"]:::fail
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["correct answer"]:::pass
        q3["Fallback 1<br/>e266782c2841@src.txt:1-2"]:::probe_2
        q4["Fallback 2<br/>20f437d6f701@src.txt:3-4"]:::probe_2
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
	qWriteGraph(t, dir, mmd)

	_, errOut, code := run(t, "answer", "q3", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon is gated") {
		t.Errorf("want 'is gated' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_FallbackProbeTeachUnresolved_Exit1(t *testing.T) {
	// probe_2 (q3,q4) is a fallback probe, teach_3 (q5) is unresolved.
	// Answering q3 must fail with "teach batch not resolved".
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachAtMaxGraph())

	_, errOut, code := run(t, "answer", "q3", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: teach batch teach_3 is not resolved for mycon") {
		t.Errorf("want 'teach batch not resolved' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_TeachingIncomplete_Exit1(t *testing.T) {
	// qTeachReadyGraph: probe_1 resolved with fail (OpenTargets=[q2]),
	// probe_2 (q3,q4) is fallback, no teach batch (TeachingSpent=false).
	// Answering q3 should fail because teaching round is not complete.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachReadyGraph())

	_, errOut, code := run(t, "answer", "q3", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: teaching round for mycon is not complete") {
		t.Errorf("want 'teaching round not complete' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_ProbeMinCount_Exit1(t *testing.T) {
	// probe_1 has only 1 question; default ProbeMin=2 → refuse.
	dir, errPath := qSetupDir(t)
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        mycon --> q1
    end
    classDef probe_1 stroke:#4aa3ff
`
	qWriteGraph(t, dir, mmd)

	_, errOut, code := run(t, "answer", "q1", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: batch probe_1 has too few questions (need at least 2)") {
		t.Errorf("want 'too few questions' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestAnswer_TeachMinCount_Exit1(t *testing.T) {
	// teach_3 has 1 question; TM_TEACH_MIN=2 → refuse.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_TEACH_MIN", "2")
	qWriteGraph(t, dir, qTeachAtMaxGraph())

	_, errOut, code := run(t, "answer", "q5", "some answer")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: batch teach_3 has too few questions (need at least 2)") {
		t.Errorf("want 'too few questions' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

// ── Happy path tests ──────────────────────────────────────────────────────────

func TestAnswer_ProbeHappyPath_WithAsked(t *testing.T) {
	// probe_1 (q1, q2) is answerable. Answer q1 with --asked.
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	rawText := `answer with "quotes" and $signs`
	askedWording := "how does X work?"

	out, errOut, code := run(t, "answer", "q1", rawText, "--asked", askedWording)
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify graph structure.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	// a1 must exist with correct fields.
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
	if a1.Class != "pending" {
		t.Errorf("a1.Class: want 'pending', got %q", a1.Class)
	}
	if a1.Label != rawText {
		t.Errorf("a1.Label: want %q, got %q", rawText, a1.Label)
	}
	if a1.Asked != askedWording {
		t.Errorf("a1.Asked: want %q, got %q", askedWording, a1.Asked)
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

	// Lint must pass.
	lintFile(t, file, dir)

	// Event log: one "answer" event with correct fields.
	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("no events in event log")
	}
	last := rows[len(rows)-1]
	if last["ev"] != "answer" {
		t.Errorf("last event ev: want 'answer', got %v", last["ev"])
	}
	if last["q"] != "q1" {
		t.Errorf("event q: want 'q1', got %v", last["q"])
	}
	if last["raw"] != rawText {
		t.Errorf("event raw: want %q, got %v", rawText, last["raw"])
	}
	if last["asked"] != askedWording {
		t.Errorf("event asked: want %q, got %v", askedWording, last["asked"])
	}
}

func TestAnswer_ProbeHappyPath_NoAsked(t *testing.T) {
	// Answer q2 (no --asked). The event must include asked="" per §10.
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerAlreadyAnsweredGraph())
	// answerAlreadyAnsweredGraph has q1 already answered; q2 is unanswered.

	out, errOut, code := run(t, "answer", "q2", "my plain answer")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	// Verify a2 has Asked="" and correct label.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	var a2 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a2" {
			a2 = item.A
			break
		}
	}
	if a2 == nil {
		t.Fatal("a2 not found")
	}
	if a2.Label != "my plain answer" {
		t.Errorf("a2.Label: want 'my plain answer', got %q", a2.Label)
	}
	if a2.Asked != "" {
		t.Errorf("a2.Asked: want '', got %q", a2.Asked)
	}

	lintFile(t, file, dir)

	// Event must include asked="" (empty string, not omitted).
	rows := readEventLog(t, file)
	last := rows[len(rows)-1]
	if last["ev"] != "answer" {
		t.Errorf("last event ev: want 'answer', got %v", last["ev"])
	}
	// asked field must be present (empty string).
	if askedVal, ok := last["asked"]; !ok {
		t.Error("event 'asked' field missing")
	} else if askedVal != "" {
		t.Errorf("event asked: want '', got %v", askedVal)
	}
}

func TestAnswer_TeachHappyPath(t *testing.T) {
	// teach_3 (q5) is answerable. Answer q5 without --asked.
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, qTeachAtMaxGraph())

	out, errOut, code := run(t, "answer", "q5", "teach answer text")
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

	var a5 *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == "a5" {
			a5 = item.A
			break
		}
	}
	if a5 == nil {
		t.Fatal("a5 not found")
	}
	if a5.Class != "pending" {
		t.Errorf("a5.Class: want 'pending', got %q", a5.Class)
	}
	if a5.Label != "teach answer text" {
		t.Errorf("a5.Label: want 'teach answer text', got %q", a5.Label)
	}

	lintFile(t, file, dir)

	rows := readEventLog(t, file)
	last := rows[len(rows)-1]
	if last["ev"] != "answer" {
		t.Errorf("last event ev: want 'answer', got %v", last["ev"])
	}
	if last["q"] != "q5" {
		t.Errorf("event q: want 'q5', got %v", last["q"])
	}
}

func TestAnswer_ReplacementBatch_MinExempt(t *testing.T) {
	// Replacement batch (probe_2 with 1 replacement probe q3) should be
	// answerable even when ProbeMin=2. The replacement probe's incoming edge
	// is from an unclear answer (a1 is unclear in this graph).
	//
	// This is the genuine §8.5 flow: probe_1 graded unclear+pass (no fail),
	// so there is no teaching round and OpenTargets is empty (fail-only).
	// answer q3 reaches the min-count check, which the replacement batch is
	// exempt from — no env hacks needed.
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["ambiguous"]:::unclear
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["correct"]:::pass
        q3["Replacement probe<br/>e266782c2841@src.txt:1-2"]:::probe_2
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        a1 --> q3
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef unclear stroke:#d29922
    classDef pass stroke:#3fb950
`
	file := qWriteGraph(t, dir, mmd)

	// probe_2 has 1 question (below ProbeMin=2) but is a replacement batch → min-exempt.
	out, errOut, code := run(t, "answer", "q3", "replacement answer")
	if code != 0 {
		t.Fatalf("want exit 0 (replacement batch min-exempt), got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}

	lintFile(t, file, dir)
}

// TestAsk_ReplacementBatch_Emitted is a regression test for the §8.5 flow:
// after a probe batch grades unclear+pass (no fail), tm ask must emit the
// replacement batch (probe_2) — not refuse with "teaching round not complete"
// (unclear probes take a replacement, not a teaching round; OpenTargets is
// fail-only) and not refuse on min (replacement batches are min-exempt).
func TestAsk_ReplacementBatch_Emitted(t *testing.T) {
	dir, _ := qSetupDir(t)
	t.Setenv("TM_PROBE_MIN", "2")
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["ambiguous"]:::unclear
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["correct"]:::pass
        q3["Replacement probe<br/>e266782c2841@src.txt:1-2"]:::probe_2
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        a1 --> q3
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef unclear stroke:#d29922
    classDef pass stroke:#3fb950
`
	qWriteGraph(t, dir, mmd)

	out, errOut, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("want exit 0 (emit replacement batch), got %d; stderr:\n%s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "probe_2" {
		t.Fatalf("want first line 'probe_2', got %q", out)
	}
	if !strings.HasPrefix(lines[1], "q3 |") {
		t.Errorf("want q3 emitted, got %q", lines[1])
	}
}

// ── tm check ASKED line test ──────────────────────────────────────────────────

func TestCheck_AskedLine_Present(t *testing.T) {
	// Build a graph with a pending answer that has an Asked wording.
	// Use inline graph rather than fixture so we can control Asked.
	dir, _ := qSetupDir(t)

	// Write the graph manually with an ASKED: field in a1's label.
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 2
        mycon["My concept scope<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["What does the source say<br/>cd3f27ccd149@src.txt:1-3"]:::probe_1
        q2["Second probe<br/>25070e52a6ae@src.txt:2-4"]:::probe_1
        a1["ASKED: how does X work?<br/>the user answered here"]:::pending
        mycon --> q1
        mycon --> q2
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pending stroke-dasharray:4 3
`
	file := qWriteGraph(t, dir, mmd)
	t.Setenv("TM_FILE", file)

	out, errOut, code := run(t, "check", "q1")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "ASKED: how does X work?") {
		t.Errorf("want ASKED line in check output; got:\n%s", out)
	}
	// ASKED must appear after Q: and before SRC.
	qIdx := strings.Index(out, "Q: What does the source say")
	askedIdx := strings.Index(out, "ASKED: how does X work?")
	srcIdx := strings.Index(out, "SRC cd3f27ccd149@src.txt:1-3")
	if qIdx < 0 || askedIdx < 0 || srcIdx < 0 {
		t.Fatalf("missing Q: / ASKED: / SRC in output:\n%s", out)
	}
	if qIdx >= askedIdx || askedIdx >= srcIdx {
		t.Errorf("ASKED must appear between Q: and SRC; Q@%d ASKED@%d SRC@%d", qIdx, askedIdx, srcIdx)
	}
}

func TestCheck_AskedLine_Absent_WhenEmpty(t *testing.T) {
	// An answer with no Asked field must NOT emit an ASKED line.
	tempErrlog(t)
	setupCheckSrcRoot(t)
	fixture := checkProbeFixture(t)
	t.Setenv("TM_FILE", fixture)

	out, _, code := run(t, "check", "q1")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.Contains(out, "ASKED:") {
		t.Errorf("want no ASKED line when Asked=''; got:\n%s", out)
	}
}

// ── End-to-end §10 field completeness test ────────────────────────────────────

func TestAnswer_EventLogFieldsMatchSpec(t *testing.T) {
	// Verify the event row has q, raw, asked fields matching §10, with asked=""
	// always present (even when not supplied), mirroring q event's re:"" convention.
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	_, _, code := run(t, "answer", "q1", "my raw answer", "--asked", "the question wording")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}

	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("event log is empty")
	}
	last := rows[len(rows)-1]

	if last["ev"] != "answer" {
		t.Errorf("ev: want 'answer', got %v", last["ev"])
	}
	if last["q"] != "q1" {
		t.Errorf("q: want 'q1', got %v", last["q"])
	}
	if last["raw"] != "my raw answer" {
		t.Errorf("raw: want 'my raw answer', got %v", last["raw"])
	}
	if last["asked"] != "the question wording" {
		t.Errorf("asked: want 'the question wording', got %v", last["asked"])
	}

	// Verify with absent --asked: asked="" is still present in the row.
	file2 := qWriteGraph(t, dir, answerProbeGraph())
	t.Setenv("TM_FILE", file2)
	_, _, code2 := run(t, "answer", "q1", "another answer")
	if code2 != 0 {
		t.Fatalf("want exit 0, got %d", code2)
	}
	rows2 := readEventLog(t, file2)
	last2 := rows2[len(rows2)-1]
	if _, ok := last2["asked"]; !ok {
		t.Error("asked field must be present even when absent from CLI; got missing")
	}
	if last2["asked"] != "" {
		t.Errorf("asked: want '', got %v", last2["asked"])
	}
}
