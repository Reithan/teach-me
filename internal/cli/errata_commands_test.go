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
// graded-pass probe questions. It returns the graph file path. No errata
// is applied; callers add one when they need it.
//
// Sets TM_FILE, TM_SRC_ROOT, TM_PROBE_MIN=nProbes.
func errataPassedFixture(t *testing.T, nProbes int) (gfile string) {
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
	return gfile
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

// ── tm errata tests ─────────────────────────────────────────────────────────

// errataConcept returns mycon's scope and citations from gfile and whether it
// is in passed.
func errataConcept(t *testing.T, gfile string) (scope string, cites []string, passed bool) {
	t.Helper()
	data, err := os.ReadFile(gfile)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}
	for _, list := range []struct {
		nodes  []*graph.ConceptNode
		passed bool
	}{{g.UntestedConcepts, false}, {g.PassedConcepts, true}} {
		for _, c := range list.nodes {
			if c.ID == "mycon" {
				return c.Scope, c.Cites, list.passed
			}
		}
	}
	t.Fatal("mycon not found in untested or passed")
	return "", nil, false
}

// reopenWithQuestion reopens the passed mycon and drafts one ungraded probe
// (q2) under it, so mycon is untested and has questions.
func reopenWithQuestion(t *testing.T, _ string) {
	t.Helper()
	if _, _, c := run(t, "reopen", "mycon", "reopened"); c != 0 {
		t.Fatalf("reopen: exit %d", c)
	}
	if _, errOut, c := run(t, "q", "mycon", "src.txt:1-3", "Probe again"); c != 0 {
		t.Fatalf("q: exit %d; stderr:\n%s", c, errOut)
	}
}

// TestErrata covers tm errata on every concept state it accepts, the logged
// errata event, and its refusals.
func TestErrata(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, gfile string)
		args       []string // after "errata"
		wantCode   int
		wantErr    []string
		wantPassed bool
		wantSrc    bool   // --src re-pointed the citation to src.txt:2-4
		wantQ      string // an ungraded question that must survive the errata
	}{
		{
			name:       "passed concept stays passed",
			args:       []string{"mycon", "Corrected scope", "old scope named the wrong line"},
			wantPassed: true,
		},
		{
			name:  "untested concept with questions keeps its questions",
			setup: reopenWithQuestion,
			args:  []string{"mycon", "Corrected scope", "old scope named the wrong line"},
			wantQ: "q2",
		},
		{
			name:       "--src re-points the citation and logs the move",
			args:       []string{"mycon", "Corrected scope", "old scope named the wrong line", "--src", "src.txt:2-4"},
			wantPassed: true,
			wantSrc:    true,
		},
		{
			name:     "empty reason exits 3",
			args:     []string{"mycon", "Corrected scope", ""},
			wantCode: 3,
			wantErr: []string{
				"err: errata needs a reason",
				`fix: tm errata mycon "<scope>" "<why the old scope was wrong>"`,
			},
		},
		{
			name:     "question ID exits 1",
			setup:    reopenWithQuestion,
			args:     []string{"q2", "Corrected scope", "reason"},
			wantCode: 1,
			wantErr:  []string{"err: q2 is not a concept"},
		},
		{
			name:     "unknown ID exits 3",
			args:     []string{"nope", "Corrected scope", "reason"},
			wantCode: 3,
			wantErr:  []string{`err: unknown ID "nope"`},
		},
		{
			name: "reserve concept exits 3 as unknown, as edit does",
			setup: func(t *testing.T, _ string) {
				if _, _, c := run(t, "add", "spare", "src.txt:1-2", "Spare scope"); c != 0 {
					t.Fatalf("add: exit %d", c)
				}
				if _, _, c := run(t, "reserve", "spare"); c != 0 {
					t.Fatalf("reserve: exit %d", c)
				}
			},
			args:     []string{"spare", "Corrected scope", "reason"},
			wantCode: 3,
			wantErr:  []string{`err: unknown ID "spare"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempErrlog(t)
			gfile := errataPassedFixture(t, 1)
			if tc.setup != nil {
				tc.setup(t, gfile)
			}

			out, errOut, code := run(t, append([]string{"errata"}, tc.args...)...)
			if code != tc.wantCode {
				t.Fatalf("exit: want %d, got %d; stderr:\n%s", tc.wantCode, code, errOut)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errOut, want) {
					t.Errorf("want %q in stderr; got:\n%s", want, errOut)
				}
			}
			if code != 0 {
				if findEvent(t, gfile, "errata") != nil {
					t.Error("a refused errata must log nothing")
				}
				return
			}
			if strings.TrimSpace(out) != "ok" {
				t.Errorf("want stdout 'ok', got %q", out)
			}

			scope, cites, passed := errataConcept(t, gfile)
			if scope != "Corrected scope" {
				t.Errorf("scope: want 'Corrected scope', got %q", scope)
			}
			if passed != tc.wantPassed {
				t.Errorf("passed: want %v, got %v", tc.wantPassed, passed)
			}
			if tc.wantQ != "" {
				if out, _, _ := run(t, "show", "mycon"); !strings.Contains(out, tc.wantQ) {
					t.Errorf("questions must stay after errata; show:\n%s", out)
				}
			}

			if findEvent(t, gfile, "edit") != nil {
				t.Error("errata must not log an edit event")
			}
			ev := findEvent(t, gfile, "errata")
			if ev == nil {
				t.Fatal("no errata event")
			}
			if ev["id"] != "mycon" || ev["before"] != "My concept scope" ||
				ev["after"] != "Corrected scope" || ev["reason"] != "old scope named the wrong line" {
				t.Errorf("errata event: got %v", ev)
			}
			_, hasSrcBefore := ev["src_before"]
			if tc.wantSrc {
				if len(cites) != 1 || !strings.HasSuffix(cites[0], "src.txt:2-4") {
					t.Errorf("cites: want a src.txt:2-4 citation, got %v", cites)
				}
				if !hasSrcBefore || !strings.HasSuffix(fmt.Sprint(ev["src_after"]), "src.txt:2-4") {
					t.Errorf("errata src_before/src_after: got %v / %v", ev["src_before"], ev["src_after"])
				}
			} else if hasSrcBefore {
				t.Errorf("src_before must be absent without --src; got %v", ev["src_before"])
			}
		})
	}
}

// TestEdit_RejectsErrataFlag verifies tm edit has no --errata flag, so roles
// that hold tm edit cannot rewrite a graded concept through it.
func TestEdit_RejectsErrataFlag(t *testing.T) {
	tempErrlog(t)
	gfile := errataPassedFixture(t, 1)

	_, errOut, code := run(t, "edit", "mycon", "Corrected scope", "--errata", "reason")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if findEvent(t, gfile, "edit") != nil {
		t.Error("a refused edit must log nothing")
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
				gfile := errataPassedFixture(t, 2)
				if _, _, c := run(t, "errata", "mycon", "Corrected scope", "old scope was wrong"); c != 0 {
					t.Fatalf("errata: exit %d", c)
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
				gfile := errataPassedFixture(t, 1)
				run(t, "reopen", "mycon", "reopened") //nolint:errcheck
				return gfile
			},
			wantCode: 1, wantErr: "not passed",
		},
		{
			name: "passed concept without errata refuses",
			setup: func(t *testing.T) string {
				gfile := errataPassedFixture(t, 1)
				return gfile
			},
			wantCode: 1, wantErr: "no errata for mycon since it passed",
		},
		{
			name: "an edit event carrying a reason is not errata",
			setup: func(t *testing.T) string {
				gfile := errataPassedFixture(t, 1)
				f, err := os.OpenFile(gfile+".jsonl", os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close() //nolint:errcheck
				line := `{"ev":"edit","id":"mycon","before":"My concept scope","after":"Other","errata":"why","reason":"why"}` + "\n"
				if _, err := f.WriteString(line); err != nil {
					t.Fatal(err)
				}
				return gfile
			},
			wantCode: 1, wantErr: "no errata for mycon since it passed",
		},
		{
			name: "unknown concept exits 3",
			setup: func(t *testing.T) string {
				gfile := errataPassedFixture(t, 1)
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
				gfile := errataPassedFixture(t, 1)
				run(t, "errata", "mycon", "Corrected scope", "old scope wrong") //nolint:errcheck
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
				gfile := errataPassedFixture(t, 1)
				run(t, "errata", "mycon", "Corrected scope", "old scope wrong") //nolint:errcheck
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
				gfile := errataPassedFixture(t, 1)
				run(t, "reopen", "mycon", "reopened") //nolint:errcheck
				return gfile
			},
			args:     []string{"--errata", "mycon", "keep", "Summary"},
			wantCode: 1, wantErr: "not passed",
		},
		{
			name: "passed concept without errata refuses",
			setup: func(t *testing.T) string {
				gfile := errataPassedFixture(t, 1)
				return gfile
			},
			args:     []string{"--errata", "mycon", "keep", "Summary"},
			wantCode: 1, wantErr: "no errata for mycon since it passed",
		},
		{
			name: "invalid verdict exits 3",
			setup: func(t *testing.T) string {
				gfile := errataPassedFixture(t, 1)
				run(t, "errata", "mycon", "Corrected scope", "old scope wrong") //nolint:errcheck
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
