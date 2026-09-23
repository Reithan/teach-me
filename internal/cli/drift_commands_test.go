package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Shared fixture ────────────────────────────────────────────────────────────

// driftPassedFixture builds a fully-graded session (concept passed, grade event
// logged) in a fresh temp dir and returns the graph file path and the src dir so
// callers can modify src.txt to simulate drift.
//
// Sets TM_FILE, TM_SRC_ROOT, TM_PROBE_MIN=1.
func driftPassedFixture(t *testing.T) (graphFile, srcDir string) {
	t.Helper()
	dir := t.TempDir()
	// Same content as setupCheckSrcRoot so hashes are consistent.
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	// Chdir to the temp dir so tm new writes .tmconfig there, not to the
	// package source directory.
	t.Chdir(dir)

	gfile := filepath.Join(dir, "g.mmd")
	if _, _, code := run(t, "new", gfile); code != 0 {
		t.Fatalf("new: exit %d", code)
	}
	t.Setenv("TM_FILE", gfile)
	if _, _, code := run(t, "add", "mycon", "src.txt:1-5", "My concept scope"); code != 0 {
		t.Fatalf("add: exit %d", code)
	}
	if _, _, code := run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say"); code != 0 {
		t.Fatalf("q: exit %d", code)
	}
	out, _, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("ask: exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("ask: want ≥2 lines; got %q", out)
	}
	qid := strings.Fields(lines[1])[0]
	if _, _, code = run(t, "answer", qid, "My answer"); code != 0 {
		t.Fatalf("answer: exit %d", code)
	}
	if _, _, code = run(t, "grade", qid, "pass", "Good"); code != 0 {
		t.Fatalf("grade: exit %d", code)
	}
	return gfile, dir
}

// driftSrc overwrites line 1 of src.txt so stored hashes no longer match.
func driftSrc(t *testing.T, dir string) {
	t.Helper()
	content := "line 1 MODIFIED\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// qidFromAsk returns the first question ID from `tm ask <concept>` output.
func qidFromAsk(t *testing.T) string {
	t.Helper()
	out, _, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("ask mycon: exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("ask mycon: want ≥2 lines; got %q", out)
	}
	return strings.Fields(lines[1])[0]
}

// ── TestDrift_AnswerAndCheckRefusal ───────────────────────────────────────────

// TestDrift_AnswerAndCheckRefusal verifies that both tm answer and tm check
// refuse with exit 1 when the question's citation has drifted, and pass
// through normally when it has not.
func TestDrift_AnswerAndCheckRefusal(t *testing.T) {
	cases := []struct {
		name     string
		doDrift  bool   // modify src.txt before the command
		cmd      string // "answer" or "check"
		wantCode int
		wantErr  string // non-empty only when wantCode != 0
		wantFix  string
	}{
		{
			name: "answer passes through on clean citation",
			cmd:  "answer", doDrift: false, wantCode: 0,
		},
		{
			name: "answer refuses with exit 1 on drifted citation",
			cmd:  "answer", doDrift: true, wantCode: 1,
			wantErr: "citation has drifted",
			wantFix: "tm drop",
		},
		{
			name: "check passes through on clean citation",
			cmd:  "check", doDrift: false, wantCode: 0,
		},
		{
			name: "check refuses with exit 1 on drifted citation",
			cmd:  "check", doDrift: true, wantCode: 1,
			wantErr: "citation has drifted",
			wantFix: "tm drop",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			dir := t.TempDir()
			content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
			if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TM_SRC_ROOT", dir)
			t.Setenv("TM_PROBE_MIN", "1")
			gfile := filepath.Join(dir, "g.mmd")
			t.Chdir(dir)
			run(t, "new", gfile)
			t.Setenv("TM_FILE", gfile)
			run(t, "add", "mycon", "src.txt:1-5", "My concept")
			run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say")
			qid := qidFromAsk(t)

			if tc.cmd == "check" {
				// check requires a pending answer.
				run(t, "answer", qid, "pending answer")
			}
			if tc.doDrift {
				driftSrc(t, dir)
			}

			var args []string
			if tc.cmd == "answer" {
				args = []string{"answer", qid, "My response"}
			} else {
				args = []string{"check", qid}
			}

			_, errOut, code := run(t, args...)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr=%s", tc.wantCode, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want %q in stderr; got: %s", tc.wantErr, errOut)
			}
			if tc.wantFix != "" && !strings.Contains(errOut, tc.wantFix) {
				t.Errorf("want %q in fix line; got: %s", tc.wantFix, errOut)
			}
		})
	}
}

// ── TestDrift_DropQuestion ────────────────────────────────────────────────────

// TestDrift_DropQuestion covers every row of the drop-question-drift spec
// (§7): accept on drifted+ungraded, refuse on graded or non-drifted, and
// verify that the unclear answer enables the --re replacement path.
func TestDrift_DropQuestion(t *testing.T) {
	type row struct {
		name       string
		setup      func(t *testing.T, gfile, srcDir string) (dropArg string)
		wantCode   int
		wantErr    string
		checkEvent func(t *testing.T, gfile string)
	}

	cases := []row{
		{
			name: "accepts drifted ungraded question and logs reason:drift",
			setup: func(t *testing.T, _ string, srcDir string) string {
				qid := qidFromAsk(t)
				driftSrc(t, srcDir)
				return qid
			},
			wantCode: 0,
			checkEvent: func(t *testing.T, gfile string) {
				t.Helper()
				rows := readEventLog(t, gfile)
				var found map[string]any
				for _, r := range rows {
					if r["ev"] == "drop" {
						found = r
					}
				}
				if found == nil {
					t.Fatal("no drop event")
				}
				if found["reason"] != "drift" {
					t.Errorf("reason: want 'drift', got %v", found["reason"])
				}
				n, _ := found["node"].(map[string]any)
				if n == nil {
					t.Error("drop event missing node")
				}
			},
		},
		{
			name: "refuses already-graded question",
			setup: func(t *testing.T, _ string, srcDir string) string {
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My answer")
				run(t, "grade", qid, "fail", "Wrong")
				driftSrc(t, srcDir)
				return qid
			},
			wantCode: 1,
			wantErr:  "already graded",
		},
		{
			name: "refuses when citation has not drifted",
			setup: func(t *testing.T, _, _ string) string {
				return qidFromAsk(t)
			},
			wantCode: 1,
			wantErr:  "citation has not drifted",
		},
		{
			// When there is an existing pending answer: the drop replaces it
			// in place (no duplicate a<N> IDs), and the event carries the
			// pending answer text in the "answer" field.
			name: "pending answer is replaced in place and captured in event",
			setup: func(t *testing.T, _ string, srcDir string) string {
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My pending answer text")
				driftSrc(t, srcDir)
				return qid
			},
			wantCode: 0,
			checkEvent: func(t *testing.T, gfile string) {
				t.Helper()
				// The event must carry the pending answer text.
				rows := readEventLog(t, gfile)
				var found map[string]any
				for _, r := range rows {
					if r["ev"] == "drop" {
						found = r
					}
				}
				if found == nil {
					t.Fatal("no drop event")
				}
				if found["answer"] != "My pending answer text" {
					t.Errorf("event answer: want 'My pending answer text', got %v", found["answer"])
				}
				// The graph must contain exactly one a<N> node (no duplicate IDs).
				data, err := os.ReadFile(gfile)
				if err != nil {
					t.Fatal(err)
				}
				const tombstoneLabel = "dropped: citation drifted"
				if count := strings.Count(string(data), tombstoneLabel); count != 1 {
					t.Errorf("want exactly 1 tombstone in graph, found %d; graph:\n%s", count, data)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			dir := t.TempDir()
			content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
			if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TM_SRC_ROOT", dir)
			t.Setenv("TM_PROBE_MIN", "1")
			gfile := filepath.Join(dir, "g.mmd")
			t.Chdir(dir)
			run(t, "new", gfile)
			t.Setenv("TM_FILE", gfile)
			run(t, "add", "mycon", "src.txt:1-5", "My concept")
			run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say")

			dropArg := tc.setup(t, gfile, dir)
			_, errOut, code := run(t, "drop", dropArg)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr=%s", tc.wantCode, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want %q in stderr; got: %s", tc.wantErr, errOut)
			}
			if tc.checkEvent != nil {
				tc.checkEvent(t, gfile)
			}
		})
	}
}

// TestDrift_DropQuestion_ReAfterDrop verifies that after a drift-drop the
// unclear answer enables --re and the replacement batch resolves cleanly.
func TestDrift_DropQuestion_ReAfterDrop(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	// src.txt: "line 1…5" + "line 1…5 (dup)" so replacement cite hashes fine.
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	gfile := filepath.Join(dir, "g.mmd")
	t.Chdir(dir)
	run(t, "new", gfile)
	t.Setenv("TM_FILE", gfile)
	run(t, "add", "mycon", "src.txt:1-5", "My concept")
	run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say")
	qid := qidFromAsk(t)

	// Drift the source then drop the question.
	driftSrc(t, dir)
	if _, errOut, code := run(t, "drop", qid); code != 0 {
		t.Fatalf("drop: exit %d; stderr=%s", code, errOut)
	}

	// Restore modified content so new question cite hashes.
	content2 := "line 1 MODIFIED\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content2), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add replacement question targeting lines 4-5.
	outQ, errOut, code := run(t, "q", "mycon", "src.txt:4-5", "Replacement question", "--re", qid)
	if code != 0 {
		t.Fatalf("q --re: exit %d; stdout=%s stderr=%s", code, outQ, errOut)
	}
	newQID := strings.TrimSpace(outQ)

	run(t, "answer", newQID, "Replacement answer")
	if _, errOut, code := run(t, "grade", newQID, "pass", "Good"); code != 0 {
		t.Fatalf("grade replacement: exit %d; stderr=%s", code, errOut)
	}

	// Concept must now be passed with no fail consequences from the drop.
	outStatus, _, _ := run(t, "status")
	if !strings.Contains(outStatus, "passed 1") {
		t.Errorf("want concept passed; status: %s", outStatus)
	}
}

// TestDrift_DropQuestion_ReplacementUnclear verifies that grading the
// replacement probe "unclear" is recorded as "unclear" (not "fail").
//
// When a question is drift-dropped, its tombstone answer carries DroppedLabel.
// state.RootProbeUnclear must return false for the tombstone so that the
// replacement probe's unclear verdict is not re-classified as fail.
func TestDrift_DropQuestion_ReplacementUnclear(t *testing.T) {
	tempErrlog(t)
	dir := t.TempDir()
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", "1")
	gfile := filepath.Join(dir, "g.mmd")
	t.Chdir(dir)
	run(t, "new", gfile)
	t.Setenv("TM_FILE", gfile)
	run(t, "add", "mycon", "src.txt:1-5", "My concept")
	run(t, "q", "mycon", "src.txt:1-3", "What does line 1 say")
	qid := qidFromAsk(t)

	// Drift the source then drop the question.
	driftSrc(t, dir)
	if _, errOut, code := run(t, "drop", qid); code != 0 {
		t.Fatalf("drop: exit %d; stderr=%s", code, errOut)
	}

	// Update content to reflect the new source state.
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte("line 1 MODIFIED\nline 2\nline 3\nline 4\nline 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Replace the dropped question targeting lines 4-5.
	outQ, errOut, code := run(t, "q", "mycon", "src.txt:4-5", "Replacement question", "--re", qid)
	if code != 0 {
		t.Fatalf("q --re: exit %d; stdout=%s stderr=%s", code, outQ, errOut)
	}
	newQID := strings.TrimSpace(outQ)

	run(t, "answer", newQID, "Unclear answer")
	_, errOut, code = run(t, "grade", newQID, "unclear", "Not sure")
	if code != 0 {
		t.Fatalf("grade unclear: exit %d; stderr=%s", code, errOut)
	}

	// Verify the replacement is recorded as "unclear", not "fail".
	rows := readEventLog(t, gfile)
	var found map[string]any
	for _, r := range rows {
		if r["ev"] == "grade" {
			if v, _ := r["q"].(string); v == newQID {
				found = r
			}
		}
	}
	if found == nil {
		t.Fatal("no grade event for replacement question")
	}
	// "recorded" carries the class actually written to the graph.
	// When RootProbeUnclear correctly returns false for the tombstone,
	// recorded must be "unclear" — not re-classified as "fail".
	rec, _ := found["recorded"].(string)
	if rec != "unclear" {
		t.Errorf("grade recorded: want 'unclear', got %q; tombstone should not propagate fail semantics", rec)
	}
}

// ── TestDrift_Recite ──────────────────────────────────────────────────────────

// TestDrift_Recite covers all recite behaviors: hash match on an untested and a
// passed concept, mismatch, multi-citation selection, unknown concept, and hash
// error on a nonexistent file.
func TestDrift_Recite(t *testing.T) {
	cases := []struct {
		name string
		// setup returns: graphFile, newRange to pass to recite, and a boolean
		// indicating whether the concept should be graded to passed first.
		setup    func(t *testing.T, dir string) (gfile, reciteArg string)
		wantCode int
		wantErr  string
		// wantAfter is a substring that should appear in the recite event's "after" field.
		wantAfter string
	}{
		{
			name: "re-points untested concept citation to duplicate range",
			setup: func(t *testing.T, dir string) (string, string) {
				// src.txt: "line A\nline B" at :1-2 and at :5-6.
				content := "line A\nline B\nline C\nline D\nline A\nline B\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-2", "My concept")
				return gfile, "src.txt:5-6"
			},
			wantCode: 0, wantAfter: "src.txt:5-6",
		},
		{
			name: "re-points passed concept citation to duplicate range",
			setup: func(t *testing.T, dir string) (string, string) {
				// :1-2 and :5-6 share the same content; concept gets graded to passed.
				content := "line A\nline B\nline C\nline D\nline A\nline B\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_PROBE_MIN", "1")
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-2", "My concept")
				run(t, "q", "mycon", "src.txt:3-4", "Question on lines 3-4")
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My answer")
				run(t, "grade", qid, "pass", "Good")
				return gfile, "src.txt:5-6"
			},
			wantCode: 0, wantAfter: "src.txt:5-6",
		},
		{
			name: "refuses with exit 1 when new range content differs",
			setup: func(t *testing.T, dir string) (string, string) {
				content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-2", "My concept")
				return gfile, "src.txt:3-4" // different content → hash mismatch
			},
			wantCode: 1, wantErr: "recite hash mismatch",
		},
		{
			name: "selects the correct citation from a multi-citation concept",
			setup: func(t *testing.T, dir string) (string, string) {
				// Block A at :1-2 and :5-6; block B at :3-4 (duplicated at :7-8 in setup).
				content := "line A\nline B\nline C\nline D\nline A\nline B\nline C\nline D\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				// Write a two-citation graph manually and rehash it.
				mmd := "flowchart TB\n    subgraph passed[\"Passed\"]\n    end\n    subgraph untested[\"Untested\"]\n        mycon[\"My concept<br/>src.txt:1-2<br/>src.txt:3-4\"]\n    end\n    subgraph testing[\"Testing\"]\n    end\n    classDef pending stroke-dasharray:4 3\n"
				gfile := filepath.Join(dir, "g.mmd")
				if err := os.WriteFile(gfile, []byte(mmd), 0o644); err != nil {
					t.Fatal(err)
				}
				run(t, "rehash", gfile)
				t.Setenv("TM_FILE", gfile)
				// Recite the second citation (src.txt:3-4 → "line C\nline D") to :7-8.
				return gfile, "src.txt:7-8"
			},
			wantCode: 0,
		},
		{
			name: "refuses with exit 3 for unknown concept",
			setup: func(t *testing.T, dir string) (string, string) {
				content := "line 1\nline 2\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				return gfile, "src.txt:1-2"
			},
			// Override concept arg in test body below.
			wantCode: 3, wantErr: "unknown concept",
		},
		{
			name: "refuses with exit 3 when target file does not exist",
			setup: func(t *testing.T, dir string) (string, string) {
				content := "line 1\nline 2\n"
				if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("TM_SRC_ROOT", dir)
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-2", "My concept")
				return gfile, "nonexistent.txt:1-2"
			},
			wantCode: 3, wantErr: "cannot hash",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			dir := t.TempDir()
			gfile, reciteArg := tc.setup(t, dir)

			concept := "mycon"
			if tc.wantErr == "unknown concept" {
				concept = "no_such_concept"
			}

			_, errOut, code := run(t, "recite", concept, reciteArg)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr=%s", tc.wantCode, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want %q in stderr; got: %s", tc.wantErr, errOut)
			}
			if tc.wantAfter != "" {
				rows := readEventLog(t, gfile)
				var found map[string]any
				for _, r := range rows {
					if r["ev"] == "recite" {
						found = r
					}
				}
				if found == nil {
					t.Fatal("no recite event logged")
				}
				after, _ := found["after"].(string)
				if !strings.Contains(after, tc.wantAfter) {
					t.Errorf("recite event after: want %q, got %q", tc.wantAfter, after)
				}
			}
		})
	}
}

// ── TestDrift_ReopenSrc ───────────────────────────────────────────────────────

// TestDrift_ReopenSrc verifies that reopen --src updates the citation and
// logs src_before/src_after, while reopen without --src logs neither field.
func TestDrift_ReopenSrc(t *testing.T) {
	cases := []struct {
		name       string
		withSrc    bool
		wantBefore bool
		wantAfter  bool
	}{
		{
			name:    "reopen without --src omits src_before and src_after",
			withSrc: false, wantBefore: false, wantAfter: false,
		},
		{
			name:    "reopen with --src logs src_before and src_after in event",
			withSrc: true, wantBefore: true, wantAfter: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			gfile, dir := driftPassedFixture(t)

			var args []string
			if tc.withSrc {
				// Write a new source file for the re-pointed citation.
				newContent := "new line 1\nnew line 2\nnew line 3\n"
				if err := os.WriteFile(filepath.Join(dir, "new_src.txt"), []byte(newContent), 0o644); err != nil {
					t.Fatal(err)
				}
				args = []string{"reopen", "mycon", "Gap: source updated", "--src", "new_src.txt:1-3"}
			} else {
				args = []string{"reopen", "mycon", "Gap: some gap"}
			}

			_, errOut, code := run(t, args...)
			if code != 0 {
				t.Fatalf("reopen: exit %d; stderr=%s", code, errOut)
			}

			rows := readEventLog(t, gfile)
			var ev map[string]any
			for _, r := range rows {
				if r["ev"] == "reopen" {
					ev = r
				}
			}
			if ev == nil {
				t.Fatal("no reopen event")
			}
			_, hasBefore := ev["src_before"]
			_, hasAfter := ev["src_after"]
			if hasBefore != tc.wantBefore {
				t.Errorf("src_before present=%v, want %v", hasBefore, tc.wantBefore)
			}
			if hasAfter != tc.wantAfter {
				t.Errorf("src_after present=%v, want %v", hasAfter, tc.wantAfter)
			}
		})
	}
}

// ── TestDrift_CheckDrift ──────────────────────────────────────────────────────

// TestDrift_CheckDrift verifies the §9.1 recheck payload produced by
// tm check --drift, including missing-log and unknown-concept refusals.
func TestDrift_CheckDrift(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T) (gfile, concept string)
		wantCode int
		wantOut  []string
		wantErr  string
	}{
		{
			name: "missing event log exits 1 with no-event-log message",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
				os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644) //nolint:errcheck
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_PROBE_MIN", "1")
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-5", "My concept")
				os.Remove(gfile + ".jsonl") //nolint:errcheck
				return gfile, "mycon"
			},
			wantCode: 1, wantErr: "no event log for mycon",
		},
		{
			name: "unknown concept exits 3",
			setup: func(t *testing.T) (string, string) {
				gfile, _ := driftPassedFixture(t)
				return gfile, "no_such_concept"
			},
			wantCode: 3, wantErr: "unknown concept",
		},
		{
			name: "clean source emits Q/CITE/SRC_GRADED/SRC_CURRENT/A/VERDICT blocks without DRIFT",
			setup: func(t *testing.T) (string, string) {
				gfile, _ := driftPassedFixture(t)
				return gfile, "mycon"
			},
			wantCode: 0,
			wantOut:  []string{"Q ", "CITE ", "SRC_GRADED", "SRC_CURRENT", "A: ", "VERDICT: "},
		},
		{
			name: "drifted source adds DRIFT line before the affected block",
			setup: func(t *testing.T) (string, string) {
				gfile, srcDir := driftPassedFixture(t)
				driftSrc(t, srcDir)
				return gfile, "mycon"
			},
			wantCode: 0,
			wantOut:  []string{"DRIFT ", "tm grade --drift mycon"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			gfile, concept := tc.setup(t)
			t.Setenv("TM_FILE", gfile)

			out, errOut, code := run(t, "check", "--drift", concept)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr=%s", tc.wantCode, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want %q in stderr; got: %s", tc.wantErr, errOut)
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("want %q in output; got:\n%s", want, out)
				}
			}
		})
	}
}

// ── TestDrift_GradeDrift ──────────────────────────────────────────────────────

// TestDrift_GradeDrift covers all grade --drift behaviors: keep and reopen on
// passed and untested concepts, verdict validation, and role/unknown-concept refusals.
func TestDrift_GradeDrift(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T) string // returns graphFile
		args     []string                  // positionals after "grade"
		wantCode int
		wantErr  string
		check    func(t *testing.T, gfile string)
	}{
		{
			name: "keep rehashes passed-concept citations and logs recheck event",
			setup: func(t *testing.T) string {
				gfile, srcDir := driftPassedFixture(t)
				driftSrc(t, srcDir)
				return gfile
			},
			args:     []string{"--drift", "mycon", "keep", "Source changed but scope holds"},
			wantCode: 0,
			check: func(t *testing.T, gfile string) {
				rows := readEventLog(t, gfile)
				var ev map[string]any
				for _, r := range rows {
					if r["ev"] == "recheck" {
						ev = r
					}
				}
				if ev == nil {
					t.Fatal("no recheck event")
				}
				if ev["verdict"] != "keep" {
					t.Errorf("verdict: want 'keep', got %v", ev["verdict"])
				}
				if ev["concept"] != "mycon" {
					t.Errorf("concept: want 'mycon', got %v", ev["concept"])
				}
			},
		},
		{
			name: "reopen moves passed concept to untested and logs recheck event",
			setup: func(t *testing.T) string {
				gfile, srcDir := driftPassedFixture(t)
				driftSrc(t, srcDir)
				return gfile
			},
			args:     []string{"--drift", "mycon", "reopen", "Scope no longer valid"},
			wantCode: 0,
			check: func(t *testing.T, gfile string) {
				rows := readEventLog(t, gfile)
				var ev map[string]any
				for _, r := range rows {
					if r["ev"] == "recheck" {
						ev = r
					}
				}
				if ev == nil {
					t.Fatal("no recheck event")
				}
				if ev["verdict"] != "reopen" {
					t.Errorf("verdict: want 'reopen', got %v", ev["verdict"])
				}
				// Concept must be back in untested.
				outStatus, _, _ := run(t, "status")
				if strings.Contains(outStatus, "passed 1") {
					t.Error("concept should be untested after reopen")
				}
			},
		},
		{
			name: "keep on untested concept succeeds and logs recheck event",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
				os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644) //nolint:errcheck
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_PROBE_MIN", "1")
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-5", "My concept")
				run(t, "q", "mycon", "src.txt:1-3", "Q1")
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My answer")
				run(t, "grade", qid, "fail", "Wrong")
				driftSrc(t, dir)
				return gfile
			},
			args:     []string{"--drift", "mycon", "keep", "Source drift; scope unchanged"},
			wantCode: 0,
		},
		{
			name: "reopen on untested concept refuses with exit 1 not-passed",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
				os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644) //nolint:errcheck
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_PROBE_MIN", "1")
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-5", "My concept")
				run(t, "q", "mycon", "src.txt:1-3", "Q1")
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My answer")
				run(t, "grade", qid, "fail", "Wrong")
				return gfile
			},
			args:     []string{"--drift", "mycon", "reopen", "Summary"},
			wantCode: 1, wantErr: "not passed",
		},
		{
			name: "unknown concept exits 3",
			setup: func(t *testing.T) string {
				gfile, _ := driftPassedFixture(t)
				return gfile
			},
			args:     []string{"--drift", "no_such", "keep", "Summary"},
			wantCode: 3, wantErr: "unknown concept",
		},
		{
			name: "verdict keep without --drift exits 3 with invalid-verdict message",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
				os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644) //nolint:errcheck
				t.Setenv("TM_SRC_ROOT", dir)
				t.Setenv("TM_PROBE_MIN", "1")
				gfile := filepath.Join(dir, "g.mmd")
				t.Chdir(dir)
				run(t, "new", gfile)
				t.Setenv("TM_FILE", gfile)
				run(t, "add", "mycon", "src.txt:1-5", "My concept")
				run(t, "q", "mycon", "src.txt:1-3", "Q1")
				qid := qidFromAsk(t)
				run(t, "answer", qid, "My answer")
				return gfile
			},
			// Pass qid as first positional (not --drift concept).
			args:     []string{"q1", "keep", "Summary"},
			wantCode: 3, wantErr: "verdict must be pass, fail, or unclear",
		},
		{
			name: "verdict pass with --drift exits 3 with must-be-keep-or-reopen message",
			setup: func(t *testing.T) string {
				gfile, _ := driftPassedFixture(t)
				return gfile
			},
			args:     []string{"--drift", "mycon", "pass", "Summary"},
			wantCode: 3, wantErr: "--drift verdict must be keep or reopen",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			gfile := tc.setup(t)
			t.Setenv("TM_FILE", gfile)

			args := append([]string{"grade"}, tc.args...)
			_, errOut, code := run(t, args...)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr=%s", tc.wantCode, code, errOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("want %q in stderr; got: %s", tc.wantErr, errOut)
			}
			if tc.check != nil {
				tc.check(t, gfile)
			}
		})
	}
}
