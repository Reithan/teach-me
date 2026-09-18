package cli_test

import (
	"os"
	"strings"
	"testing"
)

// ── tm show ───────────────────────────────────────────────────────────────────

func TestShow_Concept_Open(t *testing.T) {
	// tm show log_matching: shows the concept record with batches.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "show", "log_matching")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	wantLines := []string{
		"id: log_matching",
		"kind: concept",
		"state: open",
		"scope: Log matching property",
		"gap: treats index match as sufficient, ignores term",
		"src: raft.txt:190-240",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q; got:\n%s", want, out)
		}
	}

	// Must include batch info.
	if !strings.Contains(out, "probe_1 resolved") {
		t.Errorf("want 'probe_1 resolved' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "q2 fail") {
		t.Errorf("want 'q2 fail' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "teach_3 open") {
		t.Errorf("want 'teach_3 open' in output; got:\n%s", out)
	}
}

func TestShow_Concept_Passed(t *testing.T) {
	// tm show leader_election: passed concept, no batches.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "show", "leader_election")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	if !strings.Contains(out, "id: leader_election") {
		t.Errorf("want id: leader_election; got:\n%s", out)
	}
	if !strings.Contains(out, "kind: concept") {
		t.Errorf("want kind: concept; got:\n%s", out)
	}
	if !strings.Contains(out, "state: passed") {
		t.Errorf("want state: passed; got:\n%s", out)
	}
}

func TestShow_Question_WithAnswer(t *testing.T) {
	// tm show q2: question with a graded answer (fail class).
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "show", "q2")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	wantLines := []string{
		"id: q2",
		"kind: q",
		"batch: probe_1",
		"concept: log_matching",
		"scope: Same index and term implies identical prefix",
		"src: raft.txt:202-215",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q; got:\n%s", want, out)
		}
	}
	// Answer section: a2 has class fail.
	if !strings.Contains(out, "answer: a2") {
		t.Errorf("want 'answer: a2'; got:\n%s", out)
	}
	if !strings.Contains(out, "state: fail") {
		t.Errorf("want 'state: fail'; got:\n%s", out)
	}
	if !strings.Contains(out, "Says matching index is enough") {
		t.Errorf("want answer label in output; got:\n%s", out)
	}
}

func TestShow_Question_Unanswered(t *testing.T) {
	// tm show q6: question with no answer.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "show", "q6")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	if !strings.Contains(out, "id: q6") {
		t.Errorf("want 'id: q6'; got:\n%s", out)
	}
	if !strings.Contains(out, "answer: none") {
		t.Errorf("want 'answer: none' for unanswered q6; got:\n%s", out)
	}
}

func TestShow_Answer(t *testing.T) {
	// tm show a2: answer node.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, errOut, code := run(t, "show", "a2")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	if !strings.Contains(out, "id: a2") {
		t.Errorf("want 'id: a2'; got:\n%s", out)
	}
	if !strings.Contains(out, "kind: answer") {
		t.Errorf("want 'kind: answer'; got:\n%s", out)
	}
	if !strings.Contains(out, "question: q2") {
		t.Errorf("want 'question: q2'; got:\n%s", out)
	}
	if !strings.Contains(out, "state: fail") {
		t.Errorf("want 'state: fail'; got:\n%s", out)
	}
}

func TestShow_UnknownID_Exit3(t *testing.T) {
	// Unknown id → exit 3.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	_, errOut, code := run(t, "show", "nonexistent_xyz")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err: unknown id") {
		t.Errorf("want unknown id err; got:\n%s", errOut)
	}
}

func TestShow_NoFile_Exit3(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	t.Chdir(t.TempDir())

	_, errOut, code := run(t, "show", "some_id")
	if code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if !strings.Contains(errOut, "err:") {
		t.Errorf("want err: line; got:\n%s", errOut)
	}
}

// ── tm show --history ─────────────────────────────────────────────────────────

// showHistoryFixture creates a temp .mmd and matching .mmd.jsonl for history tests.
// It returns the path to the .mmd file.
//
// The .mmd.jsonl contains three events:
//   - ev=grade q=q2 (should match when showing q2)
//   - ev=gap concept=log_matching (should match when showing log_matching)
//   - ev=add id=commit_rules (should NOT match q2 or log_matching)
//
// Content fields use Mermaid escaping to test unescaping in show --history.
func showHistoryFixture(t *testing.T) {
	t.Helper()
	setupRaftSrcRoot(t)
	raftPath := raftFixture(t)
	t.Setenv("TM_FILE", raftPath)

	// Write the event log alongside the .mmd file.
	logPath := raftPath + ".jsonl"

	events := `{"t":"2024-01-01T00:00:00Z","ev":"grade","q":"q2","verdict":"fail","summary":"Says matching index is enough; never mentions term","raw":"it#39;s the same #quot;index#quot;","guided":false,"oos":false}
{"t":"2024-01-01T00:01:00Z","ev":"gap","concept":"log_matching","before":"","after":"treats index match as sufficient, ignores term"}
{"t":"2024-01-01T00:02:00Z","ev":"add","id":"commit_rules","scope":"Commit rules","src":"raft.txt:241-300","parents":[],"children":[]}
`
	if err := os.WriteFile(logPath, []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	// Register cleanup to remove the log file so no leakage between tests.
	t.Cleanup(func() { _ = os.Remove(logPath) })
}

func TestShow_History_Question(t *testing.T) {
	// tm show q2 --history: grade event matches q=q2 and is included.
	tempErrlog(t)
	showHistoryFixture(t)

	out, errOut, code := run(t, "show", "q2", "--history")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// Must include the "---" separator.
	if !strings.Contains(out, "---") {
		t.Errorf("want --- separator in history output; got:\n%s", out)
	}

	// grade event for q2 must appear.
	if !strings.Contains(out, `"ev"`) {
		t.Errorf("want ev field in history; got:\n%s", out)
	}
	if !strings.Contains(out, `"grade"`) {
		t.Errorf("want grade event in history; got:\n%s", out)
	}

	// The raw answer must be unescaped: "#39;" → "'" and "#quot;" → '"'.
	if strings.Contains(out, "#39;") || strings.Contains(out, "#quot;") {
		t.Errorf("raw answer must be unescaped in history; got:\n%s", out)
	}
	if !strings.Contains(out, "it's the same") {
		t.Errorf("want unescaped #39; in raw field; got:\n%s", out)
	}

	// The gap and add events must NOT appear (they reference log_matching/commit_rules, not q2).
	if strings.Contains(out, `"gap"`) {
		t.Errorf("gap event must not appear in q2 history; got:\n%s", out)
	}
}

func TestShow_History_Concept(t *testing.T) {
	// tm show log_matching --history: gap event matches concept=log_matching.
	tempErrlog(t)
	showHistoryFixture(t)

	out, errOut, code := run(t, "show", "log_matching", "--history")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	// gap event must appear.
	if !strings.Contains(out, `"gap"`) {
		t.Errorf("want gap event in log_matching history; got:\n%s", out)
	}
	// grade event for q2 must NOT appear in concept history.
	if strings.Contains(out, `"grade"`) {
		t.Errorf("grade event must not appear in log_matching history; got:\n%s", out)
	}
}

func TestShow_History_NoLogFile_NoError(t *testing.T) {
	// tm show --history with no .jsonl file → node record only, no error.
	tempErrlog(t)
	raftFixtureWithSrc(t)

	// Ensure there is no .jsonl file.
	raftPath := raftFixture(t)
	logPath := raftPath + ".jsonl"
	_ = os.Remove(logPath)

	out, errOut, code := run(t, "show", "log_matching", "--history")
	if code != 0 {
		t.Fatalf("want exit 0 when no log file, got %d; stderr:\n%s", code, errOut)
	}
	// Node record must still be present.
	if !strings.Contains(out, "id: log_matching") {
		t.Errorf("want node record in output; got:\n%s", out)
	}
	// No "---" separator since there's no history.
	if strings.Contains(out, "---") {
		t.Errorf("no --- expected when no log file; got:\n%s", out)
	}
	// No error in stderr.
	if strings.Contains(errOut, "err:") {
		t.Errorf("no error expected when no log file; got stderr:\n%s", errOut)
	}
}

func TestShow_History_NoLogFile_FilepathConvention(t *testing.T) {
	// --history reads the file named "<graphfile>.jsonl" per §3.
	// Verify the naming convention by placing events at the right path.
	tempErrlog(t)
	showHistoryFixture(t)

	raftPath := raftFixture(t)
	expectedLogPath := raftPath + ".jsonl"

	// Confirm the log file exists at the expected path.
	if _, err := os.Stat(expectedLogPath); os.IsNotExist(err) {
		t.Fatalf("expected log file at %s to exist", expectedLogPath)
	}

	out, errOut, code := run(t, "show", "q2", "--history")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	// We should see the grade event.
	if !strings.Contains(out, `"grade"`) {
		t.Errorf("want grade event when log file exists at %s; got:\n%s", expectedLogPath, out)
	}
}

func TestShow_GraderRoleAllowed(t *testing.T) {
	// show is exempt from the grader ban; grader role must reach the handler.
	t.Setenv("TM_ROLE", "grader")
	tempErrlog(t)
	raftFixtureWithSrc(t)

	out, _, code := run(t, "show", "q2")
	if code != 0 {
		t.Fatalf("want exit 0 for grader on show, got %d", code)
	}
	if !strings.Contains(out, "id: q2") {
		t.Errorf("want q2 record in output; got:\n%s", out)
	}
}

// ── Shared fixture helpers for status tests ──────────────────────────────────

// raftFixtureWithSrc duplicated here to confirm it is available cross-file.
// (It is defined in status_test.go; this is a compile-check comment only.)
