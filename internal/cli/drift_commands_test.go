package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// setupDriftFixture builds a minimal graph in dir with:
//   - src.txt (5 lines)
//   - g.mmd with one concept and one hashed question citation
//
// Returns the graph path and the src path.
// Caller must set TM_FILE=graphPath and TM_PROBE_MIN=1.
func setupDriftFixture(t *testing.T, dir string) (graphPath, srcPath string) {
	t.Helper()

	// Write src.txt.
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	srcPath = filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Create graph.
	graphPath = filepath.Join(dir, "g.mmd")
	out, errOut, code := run(t, "new", graphPath)
	if code != 0 {
		t.Fatalf("new: want 0, got %d; stdout=%s stderr=%s", code, out, errOut)
	}

	t.Setenv("TM_FILE", graphPath)

	// Add concept with citation to lines 1-5.
	out, errOut, code = run(t, "add", "mycon", "src.txt:1-5", "My concept scope")
	if code != 0 {
		t.Fatalf("add: want 0, got %d; stdout=%s stderr=%s", code, out, errOut)
	}

	// Add one question (TM_PROBE_MIN=1).
	out, errOut, code = run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say")
	if code != 0 {
		t.Fatalf("q: want 0, got %d; stdout=%s stderr=%s", code, out, errOut)
	}
	return graphPath, srcPath
}

// driftSrc modifies line 1 of src.txt in dir so hashes no longer match.
func driftSrc(t *testing.T, srcPath string) {
	t.Helper()
	modified := "line 1 MODIFIED\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(srcPath, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildPassedConceptSession builds a full session and returns the qid and
// concept ID that has been passed. The caller should have set TM_PROBE_MIN=1.
//
// State: concept passed, grade event logged, src.txt still original.
func buildPassedConceptSession(t *testing.T, dir string) (graphPath, srcPath string) {
	t.Helper()

	graphPath, srcPath = setupDriftFixture(t, dir)

	// Determine qid from the graph.
	out, _, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("ask: want 0, got %d; out=%s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("ask: want at least 2 lines; got %q", out)
	}
	// Format: "probe_1\nq1 | ..."
	qid := strings.Fields(lines[1])[0]

	// Answer the question.
	out, errOut, code := run(t, "answer", qid, "My answer here")
	if code != 0 {
		t.Fatalf("answer: want 0, got %d; stdout=%s stderr=%s", code, out, errOut)
	}

	// Grade it pass (grade does not require TM_ROLE=grader when not set).
	out, errOut, code = run(t, "grade", qid, "pass", "Good answer")
	if code != 0 {
		t.Fatalf("grade: want 0, got %d; stdout=%s stderr=%s", code, out, errOut)
	}

	return graphPath, srcPath
}

// ── answer drift refusal ──────────────────────────────────────────────────────

// TestAnswer_DriftRefusal verifies that tm answer refuses with exit 1 and the
// correct err:/fix: lines when the question's citation has drifted.
func TestAnswer_DriftRefusal(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")

	setupDriftFixture(t, dir)

	// Find the question ID.
	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	// Verify answer works before drift.
	outBefore, errBefore, codeBefore := run(t, "answer", qid, "Test answer")
	if codeBefore != 0 {
		t.Fatalf("answer before drift: want 0, got %d; stdout=%s stderr=%s", codeBefore, outBefore, errBefore)
	}

	// We need a fresh question without an answer to test the drift refusal.
	// Re-create the session with a different question.
	dir2 := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath2, srcPath2 := setupDriftFixture(t, dir2)
	t.Setenv("TM_FILE", graphPath2)
	t.Setenv("TM_SRC_ROOT", dir2)

	out2, _, _ := run(t, "ask", "mycon")
	lines2 := strings.Split(strings.TrimSpace(out2), "\n")
	qid2 := strings.Fields(lines2[1])[0]

	// Drift the source.
	driftSrc(t, srcPath2)

	_, errOut, code := run(t, "answer", qid2, "My drifted answer")
	if code != 1 {
		t.Fatalf("answer after drift: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, qid2+" citation has drifted") {
		t.Errorf("want drift refusal message; got: %s", errOut)
	}
	if !strings.Contains(errOut, "tm drop "+qid2) {
		t.Errorf("want fix line with 'tm drop %s'; got: %s", qid2, errOut)
	}
}

// TestAnswer_NoDriftPassthrough verifies that tm answer succeeds when the
// citation has not drifted.
func TestAnswer_NoDriftPassthrough(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	setupDriftFixture(t, dir)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	_, errOut, code := run(t, "answer", qid, "My answer")
	if code != 0 {
		t.Fatalf("answer without drift: want 0, got %d; stderr=%s", code, errOut)
	}
}

// ── check drift refusal ───────────────────────────────────────────────────────

// TestCheck_DriftRefusal verifies that tm check refuses with exit 1 when the
// question's citation has drifted.
func TestCheck_DriftRefusal(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	// Answer (before drift).
	_, _, _ = run(t, "answer", qid, "My answer")

	// Now drift the source.
	driftSrc(t, srcPath)

	_, errOut, code := run(t, "check", qid)
	if code != 1 {
		t.Fatalf("check after drift: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, qid+" citation has drifted") {
		t.Errorf("want drift refusal; got: %s", errOut)
	}
	if !strings.Contains(errOut, "tm drop "+qid) {
		t.Errorf("want fix line with 'tm drop'; got: %s", errOut)
	}
}

// TestCheck_NoDriftPassthrough verifies that tm check succeeds when no drift.
func TestCheck_NoDriftPassthrough(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, _ := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	_, _, _ = run(t, "answer", qid, "My answer")

	_, errOut, code := run(t, "check", qid)
	if code != 0 {
		t.Fatalf("check without drift: want 0, got %d; stderr=%s", code, errOut)
	}
}

// ── drop question drift ───────────────────────────────────────────────────────

// TestDrop_Question_Drifted verifies that tm drop <qid> succeeds on a drifted
// ungraded question, emits exit 0, and logs a drop event with reason: drift.
func TestDrop_Question_Drifted(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	// Drift the source BEFORE dropping.
	driftSrc(t, srcPath)

	outDrop, errOut, code := run(t, "drop", qid)
	if code != 0 {
		t.Fatalf("drop drifted question: want 0, got %d; stdout=%s stderr=%s", code, outDrop, errOut)
	}
	if strings.TrimSpace(outDrop) != "ok" {
		t.Errorf("want 'ok', got %q", outDrop)
	}

	// Verify the event log has a drop event with reason: drift.
	rows := readEventLog(t, graphPath)
	var dropRow map[string]any
	for _, r := range rows {
		if r["ev"] == "drop" {
			dropRow = r
			break
		}
	}
	if dropRow == nil {
		t.Fatal("no drop event in event log")
	}
	if dropRow["id"] != qid {
		t.Errorf("drop event id: want %q, got %v", qid, dropRow["id"])
	}
	if dropRow["reason"] != "drift" {
		t.Errorf("drop event reason: want 'drift', got %v", dropRow["reason"])
	}
	node, ok := dropRow["node"].(map[string]any)
	if !ok {
		t.Fatalf("drop event node is not a map: %T", dropRow["node"])
	}
	if node["scope"] == "" {
		t.Error("drop event node.scope is empty")
	}
}

// TestDrop_Question_Graded_Refuse verifies that tm drop refuses a graded
// question (already graded, drift doesn't matter).
func TestDrop_Question_Graded_Refuse(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	// Answer and grade the question.
	_, _, _ = run(t, "answer", qid, "My answer")
	_, _, _ = run(t, "grade", qid, "fail", "Not quite right")

	// Now drift the source.
	driftSrc(t, srcPath)

	_, errOut, code := run(t, "drop", qid)
	if code != 1 {
		t.Fatalf("drop graded question: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "already graded") {
		t.Errorf("want 'already graded' err; got: %s", errOut)
	}
}

// TestDrop_Question_NotDrifted_Refuse verifies that tm drop refuses when
// the citation has not drifted (already covered but explicit).
func TestDrop_Question_NotDrifted_Refuse(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, _ := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	_, errOut, code := run(t, "drop", qid)
	if code != 1 {
		t.Fatalf("drop non-drifted: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "citation has not drifted") {
		t.Errorf("want 'citation has not drifted'; got: %s", errOut)
	}
}

// ── --re after drop restores batch ───────────────────────────────────────────

// TestDrop_QuestionDrift_ReAfterDrop verifies that after dropping a drifted
// question, tm q --re <qid> works and creates a replacement question, and
// grading it pass completes the batch without any verdict consequences.
func TestDrop_QuestionDrift_ReAfterDrop(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	// Write a second (non-drifting) src range for the replacement question.
	// Lines 4-5 won't be affected when we drift line 1.
	out, _, _ := run(t, "ask", "mycon")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	qid := strings.Fields(lines[1])[0]

	// Drift the source.
	driftSrc(t, srcPath)

	// Drop the drifted question.
	_, errOut, code := run(t, "drop", qid)
	if code != 0 {
		t.Fatalf("drop: want 0, got %d; stderr=%s", code, errOut)
	}

	// Restore src.txt so the replacement question's citation can be hashed.
	// Use lines 4-5 which were not modified.
	restoredContent := "line 1 MODIFIED\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(srcPath, []byte(restoredContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add replacement question with --re targeting the dropped question.
	outQ, errOut, code := run(t, "q", "mycon", "src.txt:4-5", "Replacement question", "--re", qid)
	if code != 0 {
		t.Fatalf("q --re: want 0, got %d; stdout=%s stderr=%s", code, outQ, errOut)
	}
	newQID := strings.TrimSpace(outQ)

	// Answer and grade the replacement question.
	_, _, _ = run(t, "answer", newQID, "Replacement answer")
	outGrade, errOut, code := run(t, "grade", newQID, "pass", "Good")
	if code != 0 {
		t.Fatalf("grade replacement: want 0, got %d; stdout=%s stderr=%s", code, outGrade, errOut)
	}

	// Verify status: concept should now be passed.
	outStatus, _, code := run(t, "status")
	if code != 0 {
		t.Fatalf("status: want 0, got %d", code)
	}
	// After pass, the passed count should be 1.
	if !strings.Contains(outStatus, "passed 1") {
		t.Errorf("want concept passed; status: %s", outStatus)
	}
}

// ── recite ───────────────────────────────────────────────────────────────────

// TestRecite_HashMatch verifies that tm recite succeeds when the new range
// hashes to the same content as an existing citation.
func TestRecite_HashMatch(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	// Write src.txt: original at lines 1-3, new at lines 6-8 (same text).
	// Lines 1-3: "line 1\nline 2\nline 3"
	// Lines 6-8: same text moved to different position.
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\nline 1\nline 2\nline 3\n"
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	graphPath := filepath.Join(dir, "g.mmd")
	run(t, "new", graphPath)
	t.Setenv("TM_FILE", graphPath)

	// Add concept with citation at lines 1-3.
	run(t, "add", "mycon", "src.txt:1-3", "My concept")

	// Recite to lines 6-8 (same content, so same hash).
	outRecite, errOut, code := run(t, "recite", "mycon", "src.txt:6-8")
	if code != 0 {
		t.Fatalf("recite: want 0, got %d; stdout=%s stderr=%s", code, outRecite, errOut)
	}
	if strings.TrimSpace(outRecite) != "ok" {
		t.Errorf("recite: want 'ok', got %q", outRecite)
	}

	// Verify event log has recite event with before/after.
	rows := readEventLog(t, graphPath)
	var reciteRow map[string]any
	for _, r := range rows {
		if r["ev"] == "recite" {
			reciteRow = r
			break
		}
	}
	if reciteRow == nil {
		t.Fatal("no recite event in log")
	}
	if reciteRow["id"] != "mycon" {
		t.Errorf("recite event id: want 'mycon', got %v", reciteRow["id"])
	}
	before, _ := reciteRow["before"].(string)
	after, _ := reciteRow["after"].(string)
	if before == "" {
		t.Error("recite event before is empty")
	}
	if after == "" {
		t.Error("recite event after is empty")
	}
	// The after citation should point to lines 6-8.
	if !strings.Contains(after, "src.txt:6-8") {
		t.Errorf("recite after should contain src.txt:6-8; got %q", after)
	}
}

// TestRecite_HashMismatch verifies that tm recite refuses with exit 1 and the
// correct error when the new range has different content.
func TestRecite_HashMismatch(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	srcContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	graphPath := filepath.Join(dir, "g.mmd")
	run(t, "new", graphPath)
	t.Setenv("TM_FILE", graphPath)
	run(t, "add", "mycon", "src.txt:1-3", "My concept")

	// Try recite to a range with different content.
	_, errOut, code := run(t, "recite", "mycon", "src.txt:3-5")
	if code != 1 {
		t.Fatalf("recite mismatch: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "recite hash mismatch") {
		t.Errorf("want 'recite hash mismatch'; got: %s", errOut)
	}
	if !strings.Contains(errOut, "tm reopen") {
		t.Errorf("want fix suggesting tm reopen; got: %s", errOut)
	}
}

// TestRecite_MultiCitation verifies that tm recite selects the correct
// citation by hash when a concept has multiple citations.
func TestRecite_MultiCitation(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()

	// src.txt: two distinct blocks; each appears twice (moved).
	// Block A: "line 1\nline 2" at :1-2 and at :7-8
	// Block B: "line 3\nline 4" at :3-4 and at :5-6
	srcContent := "line 1\nline 2\nline 3\nline 4\nline 3\nline 4\nline 1\nline 2\n"
	srcPath := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(srcPath, []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)

	// Create a graph manually with two citations on mycon.
	// We need to use tm add with one cite, then edit manually to add a second.
	// Alternatively, write the graph directly.
	graphContent := `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>src.txt:1-2<br/>src.txt:3-4"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pending stroke-dasharray:4 3
`
	graphPath := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(graphPath, []byte(graphContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rehash the graph to add hashes.
	outRehash, errOut, code := run(t, "rehash", graphPath)
	if code != 0 {
		t.Fatalf("rehash: want 0, got %d; stdout=%s stderr=%s", code, outRehash, errOut)
	}
	t.Setenv("TM_FILE", graphPath)

	// Recite the second citation (src.txt:3-4) to its new location (src.txt:5-6).
	// Hash of "line 3\nline 4" should match both :3-4 and :5-6.
	outRecite, errOut, code := run(t, "recite", "mycon", "src.txt:5-6")
	if code != 0 {
		t.Fatalf("recite second cite: want 0, got %d; stdout=%s stderr=%s", code, outRecite, errOut)
	}
	if strings.TrimSpace(outRecite) != "ok" {
		t.Errorf("want 'ok', got %q", outRecite)
	}

	// Recite the first citation (src.txt:1-2) to its new location (src.txt:7-8).
	outRecite2, errOut, code := run(t, "recite", "mycon", "src.txt:7-8")
	if code != 0 {
		t.Fatalf("recite first cite: want 0, got %d; stdout=%s stderr=%s", code, outRecite2, errOut)
	}
}

// ── reopen --src ──────────────────────────────────────────────────────────────

// TestReopen_WithSrc verifies that tm reopen --src updates the citation and
// logs src_before and src_after in the reopen event.
func TestReopen_WithSrc(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := buildPassedConceptSession(t, dir)
	_, _ = graphPath, srcPath
	t.Setenv("TM_FILE", graphPath)

	// Write a new source file for the updated citation.
	newSrc := "new line 1\nnew line 2\nnew line 3\n"
	newSrcPath := filepath.Join(dir, "new_src.txt")
	if err := os.WriteFile(newSrcPath, []byte(newSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reopen with --src pointing to new_src.txt:1-3.
	_, errOut, code := run(t, "reopen", "mycon", "GAP: source updated", "--src", "new_src.txt:1-3")
	if code != 0 {
		t.Fatalf("reopen --src: want 0, got %d; stderr=%s", code, errOut)
	}

	// Verify event log has reopen event with src_before and src_after.
	rows := readEventLog(t, graphPath)
	var reopenRow map[string]any
	for _, r := range rows {
		if r["ev"] == "reopen" {
			reopenRow = r
		}
	}
	if reopenRow == nil {
		t.Fatal("no reopen event in log")
	}
	srcBefore, _ := reopenRow["src_before"].(string)
	srcAfter, _ := reopenRow["src_after"].(string)
	if srcBefore == "" {
		t.Error("reopen event missing src_before")
	}
	if srcAfter == "" {
		t.Error("reopen event missing src_after")
	}
	if !strings.Contains(srcAfter, "new_src.txt:1-3") {
		t.Errorf("src_after should contain new_src.txt:1-3; got %q", srcAfter)
	}
}

// TestReopen_WithoutSrc verifies reopen without --src still works (no regression).
func TestReopen_WithoutSrc(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, _ := buildPassedConceptSession(t, dir)
	t.Setenv("TM_FILE", graphPath)

	_, errOut, code := run(t, "reopen", "mycon", "Some gap")
	if code != 0 {
		t.Fatalf("reopen: want 0, got %d; stderr=%s", code, errOut)
	}

	// Verify no src_before/src_after in event.
	rows := readEventLog(t, graphPath)
	var reopenRow map[string]any
	for _, r := range rows {
		if r["ev"] == "reopen" {
			reopenRow = r
		}
	}
	if reopenRow == nil {
		t.Fatal("no reopen event")
	}
	if _, ok := reopenRow["src_before"]; ok {
		t.Error("reopen without --src should not have src_before")
	}
	if _, ok := reopenRow["src_after"]; ok {
		t.Error("reopen without --src should not have src_after")
	}
}

// ── check --drift ─────────────────────────────────────────────────────────────

// TestCheckDrift_Payload verifies that tm check --drift prints the §9.1 recheck
// payload. Builds a passed concept from scratch, then drifts the source.
func TestCheckDrift_Payload(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := buildPassedConceptSession(t, dir)
	t.Setenv("TM_FILE", graphPath)

	// Before drift: check --drift should show matching SRC_GRADED and SRC_CURRENT.
	outBefore, errOut, code := run(t, "check", "--drift", "mycon")
	if code != 0 {
		t.Fatalf("check --drift before drift: want 0, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(outBefore, "Q ") {
		t.Errorf("want Q line in payload; got:\n%s", outBefore)
	}
	if !strings.Contains(outBefore, "CITE ") {
		t.Errorf("want CITE line in payload; got:\n%s", outBefore)
	}
	if !strings.Contains(outBefore, "SRC_GRADED") {
		t.Errorf("want SRC_GRADED in payload; got:\n%s", outBefore)
	}
	if !strings.Contains(outBefore, "SRC_CURRENT") {
		t.Errorf("want SRC_CURRENT in payload; got:\n%s", outBefore)
	}
	if !strings.Contains(outBefore, "A: ") {
		t.Errorf("want A: line in payload; got:\n%s", outBefore)
	}
	if !strings.Contains(outBefore, "VERDICT: ") {
		t.Errorf("want VERDICT: line in payload; got:\n%s", outBefore)
	}
	// No DRIFT line before modification.
	if strings.Contains(outBefore, "DRIFT ") {
		t.Errorf("no DRIFT expected before source modification; got:\n%s", outBefore)
	}

	// Drift the source.
	driftSrc(t, srcPath)

	// After drift: DRIFT line should appear before the drifted block.
	outAfter, errOut, code := run(t, "check", "--drift", "mycon")
	if code != 0 {
		t.Fatalf("check --drift after drift: want 0, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(outAfter, "DRIFT ") {
		t.Errorf("want DRIFT line after source modification; got:\n%s", outAfter)
	}
	// Rubric and grade command line must be present.
	if !strings.Contains(outAfter, "tm grade --drift mycon") {
		t.Errorf("want grade --drift command line; got:\n%s", outAfter)
	}
}

// TestCheckDrift_MissingLog verifies that tm check --drift exits 1 with
// "no event log" when the log file does not exist.
func TestCheckDrift_MissingLog(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, _ := setupDriftFixture(t, dir)
	t.Setenv("TM_FILE", graphPath)

	// Delete the event log if it exists.
	_ = os.Remove(graphPath + ".jsonl")

	_, errOut, code := run(t, "check", "--drift", "mycon")
	if code != 1 {
		t.Fatalf("check --drift no log: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "no event log for mycon") {
		t.Errorf("want 'no event log' message; got: %s", errOut)
	}
	if !strings.Contains(errOut, "tm reopen") {
		t.Errorf("want fix suggesting tm reopen; got: %s", errOut)
	}
}

// ── grade --drift ─────────────────────────────────────────────────────────────

// TestGradeDrift_Keep verifies that tm grade --drift <concept> keep re-hashes
// the concept citation and logs a recheck event.
func TestGradeDrift_Keep(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := buildPassedConceptSession(t, dir)
	t.Setenv("TM_FILE", graphPath)

	// Drift the source.
	driftSrc(t, srcPath)

	// grade --drift keep.
	_, errOut, code := run(t, "grade", "--drift", "mycon", "keep", "Source change does not affect the scope")
	if code != 0 {
		t.Fatalf("grade --drift keep: want 0, got %d; stderr=%s", code, errOut)
	}

	// Verify a recheck event was logged.
	rows := readEventLog(t, graphPath)
	var recheckRow map[string]any
	for _, r := range rows {
		if r["ev"] == "recheck" {
			recheckRow = r
		}
	}
	if recheckRow == nil {
		t.Fatal("no recheck event in log")
	}
	if recheckRow["concept"] != "mycon" {
		t.Errorf("recheck concept: want 'mycon', got %v", recheckRow["concept"])
	}
	if recheckRow["verdict"] != "keep" {
		t.Errorf("recheck verdict: want 'keep', got %v", recheckRow["verdict"])
	}
}

// TestGradeDrift_Reopen verifies that tm grade --drift <concept> reopen
// moves the concept back to untested and logs a recheck event.
func TestGradeDrift_Reopen(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, srcPath := buildPassedConceptSession(t, dir)
	t.Setenv("TM_FILE", graphPath)

	// Drift the source.
	driftSrc(t, srcPath)

	// grade --drift reopen.
	_, errOut, code := run(t, "grade", "--drift", "mycon", "reopen", "Answer no longer holds")
	if code != 0 {
		t.Fatalf("grade --drift reopen: want 0, got %d; stderr=%s", code, errOut)
	}

	// Verify recheck event.
	rows := readEventLog(t, graphPath)
	var recheckRow map[string]any
	for _, r := range rows {
		if r["ev"] == "recheck" {
			recheckRow = r
		}
	}
	if recheckRow == nil {
		t.Fatal("no recheck event in log")
	}
	if recheckRow["verdict"] != "reopen" {
		t.Errorf("recheck verdict: want 'reopen', got %v", recheckRow["verdict"])
	}

	// Verify concept is now untested (status shows it as open/blocked).
	outStatus, _, _ := run(t, "status")
	if strings.Contains(outStatus, "passed 1") {
		t.Errorf("concept should no longer be passed after reopen; status: %s", outStatus)
	}
}

// ── role checks ───────────────────────────────────────────────────────────────

// TestRecite_GraderBanned verifies that TM_ROLE=grader cannot run recite.
func TestRecite_GraderBanned(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_ROLE", "grader")
	_, errOut, code := run(t, "recite", "mycon", "src.txt:1-3")
	if code != 1 {
		t.Fatalf("recite as grader: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "not available when TM_ROLE=grader") {
		t.Errorf("want role refusal; got: %s", errOut)
	}
}

// TestGradeDrift_TeacherBanned verifies that TM_ROLE=teacher cannot run grade --drift.
func TestGradeDrift_TeacherBanned(t *testing.T) {
	tempErrlog(t)
	t.Setenv("TM_ROLE", "teacher")
	_, errOut, code := run(t, "grade", "--drift", "mycon", "keep", "Summary")
	if code != 1 {
		t.Fatalf("grade --drift as teacher: want 1, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "not available when TM_ROLE=teacher") {
		t.Errorf("want role refusal; got: %s", errOut)
	}
}

// TestGradeDrift_InvalidVerdict verifies that grade --drift with an invalid
// verdict exits 3.
func TestGradeDrift_InvalidVerdict(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	t.Setenv("TM_PROBE_MIN", "1")
	graphPath, _ := buildPassedConceptSession(t, dir)
	t.Setenv("TM_FILE", graphPath)

	_, errOut, code := run(t, "grade", "--drift", "mycon", "pass", "Summary")
	if code != 3 {
		t.Fatalf("grade --drift invalid verdict: want 3, got %d; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "--drift verdict must be keep or reopen") {
		t.Errorf("want verdict error; got: %s", errOut)
	}
}
