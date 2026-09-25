package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Shared fixture ────────────────────────────────────────────────────────────

// errataPassedFixture builds a session where mycon is passed with nProbes
// graded-pass probe questions. It returns the graph file path and the src dir.
// No errata edit is applied; callers add one when they need it.
//
// Sets TM_FILE, TM_SRC_ROOT, TM_PROBE_MIN=nProbes.
func errataPassedFixture(t *testing.T, nProbes int) (gfile, srcDir string) {
	t.Helper()
	dir := t.TempDir()
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_PROBE_MIN", fmt.Sprintf("%d", nProbes))
	t.Chdir(dir)

	gfile = filepath.Join(dir, "g.mmd")
	if _, _, code := run(t, "new", gfile); code != 0 {
		t.Fatalf("new: exit %d", code)
	}
	t.Setenv("TM_FILE", gfile)
	if _, _, code := run(t, "add", "mycon", "src.txt:1-5", "My concept scope"); code != 0 {
		t.Fatalf("add: exit %d", code)
	}
	for i := 0; i < nProbes; i++ {
		if _, _, code := run(t, "q", "mycon", "src.txt:1-3", fmt.Sprintf("Probe %d", i+1)); code != 0 {
			t.Fatalf("q: exit %d", code)
		}
	}

	out, _, code := run(t, "ask", "mycon")
	if code != 0 {
		t.Fatalf("ask: exit %d", code)
	}
	var qids []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
		if f := strings.Fields(l); len(f) > 0 {
			qids = append(qids, f[0])
		}
	}
	if len(qids) != nProbes {
		t.Fatalf("ask: want %d probes, got %d; out:\n%s", nProbes, len(qids), out)
	}
	for _, qid := range qids {
		if _, _, c := run(t, "answer", qid, "My answer"); c != 0 {
			t.Fatalf("answer %s: exit %d", qid, c)
		}
	}
	for _, qid := range qids {
		if _, _, c := run(t, "grade", qid, "pass", "Good"); c != 0 {
			t.Fatalf("grade %s: exit %d", qid, c)
		}
	}
	return gfile, dir
}

// conceptInPassed reports whether concept is in the passed block of gfile.
func conceptInPassed(t *testing.T, gfile, concept string) bool {
	t.Helper()
	data, err := os.ReadFile(gfile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	for _, c := range g.PassedConcepts {
		if c.ID == concept {
			return true
		}
	}
	return false
}

// findEvent returns the last event of the given name from gfile's log, or nil.
func findEvent(t *testing.T, gfile, ev string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, r := range readEventLog(t, gfile) {
		if r["ev"] == ev {
			found = r
		}
	}
	return found
}

// ── tm edit --errata tests ──────────────────────────────────────────────────

// TestEdit_ErrataHasQuestions verifies --errata rewrites the scope of a concept
// that has questions while leaving the questions in place, and logs the reason.
func TestEdit_ErrataHasQuestions(t *testing.T) {
	probeFixture := checkProbeFixture(t) // resolve before t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)
	tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupCheckSrcRoot(t)
	copyFixtureTo(t, probeFixture, dir)
	file := filepath.Join(dir, "g.mmd")

	out, errOut, code := run(t, "edit", "mycon", "Corrected scope", "--errata", "line 3 was wrong")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want stdout 'ok', got %q", out)
	}

	data, _ := os.ReadFile(file)
	g, _ := graph.Parse(data)
	scope, questionCount := "", 0
	for _, c := range g.UntestedConcepts {
		if c.ID == "mycon" {
			scope = c.Scope
		}
	}
	for _, item := range g.TestingItems {
		if item.Q != nil {
			questionCount++
		}
	}
	if scope != "Corrected scope" {
		t.Errorf("scope: want 'Corrected scope', got %q", scope)
	}
	if questionCount == 0 {
		t.Error("questions must remain after errata edit")
	}

	editRow := findEvent(t, file, "edit")
	if editRow == nil {
		t.Fatal("no edit event")
	}
	if editRow["errata"] != "line 3 was wrong" {
		t.Errorf("edit errata: want 'line 3 was wrong', got %v", editRow["errata"])
	}
	if editRow["after"] != "Corrected scope" {
		t.Errorf("edit after: want 'Corrected scope', got %v", editRow["after"])
	}
}

// TestEdit_ErrataPassedConcept verifies --errata rewrites a passed concept's
// scope, keeps it in passed, and logs before/after/errata.
func TestEdit_ErrataPassedConcept(t *testing.T) {
	tempErrlog(t)
	gfile, _ := errataPassedFixture(t, 1)

	out, errOut, code := run(t, "edit", "mycon", "Corrected scope", "--errata", "old scope named the wrong line")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
	if !conceptInPassed(t, gfile, "mycon") {
		t.Error("mycon must stay in passed after errata edit")
	}

	editRow := findEvent(t, gfile, "edit")
	if editRow == nil {
		t.Fatal("no edit event")
	}
	if editRow["before"] != "My concept scope" {
		t.Errorf("edit before: want 'My concept scope', got %v", editRow["before"])
	}
	if editRow["after"] != "Corrected scope" {
		t.Errorf("edit after: want 'Corrected scope', got %v", editRow["after"])
	}
	if editRow["errata"] != "old scope named the wrong line" {
		t.Errorf("edit errata: got %v", editRow["errata"])
	}
}

// TestEdit_ErrataEmptyReason verifies an empty --errata reason exits 3.
func TestEdit_ErrataEmptyReason(t *testing.T) {
	tempErrlog(t)
	_, _ = errataPassedFixture(t, 1)

	_, errOut, code := run(t, "edit", "mycon", "Corrected scope", "--errata", "")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: --errata needs a reason") {
		t.Errorf("want '--errata needs a reason' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm edit mycon") {
		t.Errorf("want fix line; got:\n%s", errOut)
	}
}

// TestEdit_PlainPassedStillRefuses verifies edit without --errata still refuses
// a passed concept with the old message.
func TestEdit_PlainPassedStillRefuses(t *testing.T) {
	tempErrlog(t)
	_, _ = errataPassedFixture(t, 1)

	_, errOut, code := run(t, "edit", "mycon", "Corrected scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon is passed") {
		t.Errorf("want 'is passed' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen mycon") {
		t.Errorf("want reopen fix; got:\n%s", errOut)
	}
}

// TestEdit_ErrataSrc verifies --errata --src re-points the citation and logs
// src_before/src_after.
func TestEdit_ErrataSrc(t *testing.T) {
	tempErrlog(t)
	gfile, _ := errataPassedFixture(t, 1)

	_, errOut, code := run(t, "edit", "mycon", "Corrected scope", "--errata", "cite the right range", "--src", "src.txt:2-4")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}

	data, _ := os.ReadFile(gfile)
	g, _ := graph.Parse(data)
	for _, c := range g.PassedConcepts {
		if c.ID == "mycon" {
			if len(c.Cites) != 1 || !strings.HasSuffix(c.Cites[0], "src.txt:2-4") {
				t.Errorf("cites: want a src.txt:2-4 citation, got %v", c.Cites)
			}
		}
	}

	editRow := findEvent(t, gfile, "edit")
	if editRow["src_after"] == nil || !strings.HasSuffix(fmt.Sprint(editRow["src_after"]), "src.txt:2-4") {
		t.Errorf("edit src_after: want src.txt:2-4, got %v", editRow["src_after"])
	}
	if editRow["src_before"] == nil {
		t.Error("edit src_before must be logged when --src re-points the citation")
	}
}

// ── tm check --errata tests ──────────────────────────────────────────────────

// TestCheckErrata covers the errata recheck payload and its refusals.
func TestCheckErrata(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T) string // returns graph file
		wantCode int
		wantOut  []string
		wantErr  string
	}{
		{
			name: "payload shape on a passed concept with two graded questions",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 2)
				if _, _, c := run(t, "edit", "mycon", "Corrected scope", "--errata", "old scope was wrong"); c != 0 {
					t.Fatalf("edit --errata: exit %d", c)
				}
				return gfile
			},
			wantCode: 0,
			wantOut: []string{
				"SCOPE_BEFORE My concept scope",
				"SCOPE_AFTER Corrected scope",
				"REASON old scope was wrong",
				"Q ", "CITE ", "SRC", "A: ", "VERDICT: ",
				"tm grade --errata mycon keep|reopen",
			},
		},
		{
			name: "untested concept refuses not-passed",
			setup: func(t *testing.T) string {
				gfile, dir := errataPassedFixture(t, 1)
				_ = dir
				run(t, "reopen", "mycon", "reopened") //nolint:errcheck
				return gfile
			},
			wantCode: 1, wantErr: "not passed",
		},
		{
			name: "passed concept without an errata edit refuses",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				return gfile
			},
			wantCode: 1, wantErr: "no errata edit for mycon since it passed",
		},
		{
			name: "unknown concept exits 3",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				return gfile
			},
			wantCode: 3, wantErr: "unknown concept",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			gfile := tc.setup(t)
			t.Setenv("TM_FILE", gfile)

			concept := "mycon"
			if strings.Contains(tc.name, "unknown") {
				concept = "nope"
			}
			out, errOut, code := run(t, "check", "--errata", concept)
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

// ── tm grade --errata tests ──────────────────────────────────────────────────

// TestGradeErrata covers keep, reopen, and the errata refusals.
func TestGradeErrata(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T) string
		args     []string // positionals after "grade"
		wantCode int
		wantErr  string
		check    func(t *testing.T, gfile string)
	}{
		{
			name: "keep logs recheck kind=errata and leaves the pass",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				run(t, "edit", "mycon", "Corrected scope", "--errata", "old scope wrong") //nolint:errcheck
				return gfile
			},
			args:     []string{"--errata", "mycon", "keep", "Answers still hold"},
			wantCode: 0,
			check: func(t *testing.T, gfile string) {
				if !conceptInPassed(t, gfile, "mycon") {
					t.Error("mycon must stay passed after keep")
				}
				ev := findEvent(t, gfile, "recheck")
				if ev == nil {
					t.Fatal("no recheck event")
				}
				if ev["verdict"] != "keep" || ev["kind"] != "errata" {
					t.Errorf("recheck: want verdict=keep kind=errata, got verdict=%v kind=%v", ev["verdict"], ev["kind"])
				}
			},
		},
		{
			name: "reopen moves concept to untested with summary as GAP",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				run(t, "edit", "mycon", "Corrected scope", "--errata", "old scope wrong") //nolint:errcheck
				return gfile
			},
			args:     []string{"--errata", "mycon", "reopen", "Verdict depended on the wrong detail"},
			wantCode: 0,
			check: func(t *testing.T, gfile string) {
				if conceptInPassed(t, gfile, "mycon") {
					t.Error("mycon must leave passed after reopen")
				}
				outShow, _, _ := run(t, "show", "mycon")
				if !strings.Contains(outShow, "gap: Verdict depended on the wrong detail") {
					t.Errorf("show mycon: want the GAP; got:\n%s", outShow)
				}
				ev := findEvent(t, gfile, "recheck")
				if ev == nil || ev["kind"] != "errata" || ev["verdict"] != "reopen" {
					t.Errorf("recheck: want kind=errata verdict=reopen, got %v", ev)
				}
			},
		},
		{
			name: "untested concept refuses not-passed",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				run(t, "reopen", "mycon", "reopened") //nolint:errcheck
				return gfile
			},
			args:     []string{"--errata", "mycon", "keep", "Summary"},
			wantCode: 1, wantErr: "not passed",
		},
		{
			name: "passed concept without an errata edit refuses",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				return gfile
			},
			args:     []string{"--errata", "mycon", "keep", "Summary"},
			wantCode: 1, wantErr: "no errata edit for mycon since it passed",
		},
		{
			name: "invalid verdict exits 3",
			setup: func(t *testing.T) string {
				gfile, _ := errataPassedFixture(t, 1)
				run(t, "edit", "mycon", "Corrected scope", "--errata", "old scope wrong") //nolint:errcheck
				return gfile
			},
			args:     []string{"--errata", "mycon", "pass", "Summary"},
			wantCode: 3, wantErr: "--errata verdict must be keep or reopen",
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
