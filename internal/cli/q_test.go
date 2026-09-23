package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
)

// ── Graph fixtures for tm q tests ─────────────────────────────────────────────

// qFrontmatter is the standard frontmatter used in inline test fixtures.
const qFrontmatter = `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
`

// qWriteGraph writes mmd content to dir/g.mmd, sets TM_FILE, and returns the path.
func qWriteGraph(t *testing.T, dir, mmd string) string {
	t.Helper()
	file := filepath.Join(dir, "g.mmd")
	if err := os.WriteFile(file, []byte(mmd), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	t.Setenv("TM_FILE", file)
	return file
}

// qSetupDir creates a temp dir, sets TM_ERRORS, TM_SRC_ROOT (src.txt), and
// TM_FILE="", changes working directory. Returns (dir, errlogPath).
func qSetupDir(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	errPath := tempErrlog(t)
	t.Setenv("TM_FILE", "")
	setupSrcFile(t, dir)
	return dir, errPath
}

// qSimpleGraph is a minimal graph with one untested concept and no batches.
func qSimpleGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
}

// qPassedConceptGraph has mycon in the passed block.
func qPassedConceptGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
`
}

// qOpenProbeGraph has probe_1 open: q1 answered pass, q2 unanswered.
func qOpenProbeGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["some passing answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        mycon --> q1
        q1 --> a1
        mycon --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
`
}

// qTeachReadyGraph is a graph ready for a teach round:
// - GAP set on mycon
// - probe_1 resolved (q1=pass, q2=fail)
// - probe_2 fallback with 2 unanswered questions (q3, q4).
func qTeachReadyGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
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

// qTeachReadyNoGapGraph is like qTeachReadyGraph but without a GAP field.
func qTeachReadyNoGapGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
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

// qTeachOpenBatchGraph: teach-ready plus teach_3 open (q5 answered pass, q6 unanswered).
func qTeachOpenBatchGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        q3["Fallback probe 1<br/>e266782c2841@src.txt:1-2"]:::probe_2
        q4["Fallback probe 2<br/>20f437d6f701@src.txt:3-4"]:::probe_2
        q5["Teach question<br/>cd3f27ccd149@src.txt:1-3"]:::teach_3
        a5["good teach answer"]:::pass
        q6["Second teach question<br/>25070e52a6ae@src.txt:2-4"]:::teach_3
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        mycon --> q3
        mycon --> q4
        a2 --> q5
        q5 --> a5
        a2 --> q6
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// qTeachPassTargetGraph: probe_1 has q1=fail (for FailedProbeBatches), q2=pass.
// Using --teach --re q2 must fail because a2 is pass, not fail/unclear.
// probe_2 fallback (q3, q4) satisfies the FallbackProbes check.
func qTeachPassTargetGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["wrong answer"]:::fail
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["also correct"]:::pass
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

// qTeachOOSTargetGraph: teach-ready but q2's answer is OOS.
func qTeachOOSTargetGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["OOS<br/>wrong answer"]:::fail
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

// qReNotUnclearGraph: probe_1 with q1=pass, q2=fail (neither is unclear).
func qReNotUnclearGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// qProbeAtMaxGraph: probe_1 with 2 unanswered questions (for TM_PROBE_MAX=2 test).
func qProbeAtMaxGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
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

// qTeachAtMaxGraph: teach-ready with 1 unanswered teach question in teach_3.
// With TM_MAX_TEACH=1: TeachingSpent fires.
// With TM_TEACH_MAX=1: draft-batch-at-max fires (MaxTeach default=8, so spent=false).
func qTeachAtMaxGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        q3["Fallback probe 1<br/>e266782c2841@src.txt:1-2"]:::probe_2
        q4["Fallback probe 2<br/>20f437d6f701@src.txt:3-4"]:::probe_2
        q5["First teach question<br/>cd3f27ccd149@src.txt:1-3"]:::teach_3
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        mycon --> q3
        mycon --> q4
        a2 --> q5
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef teach_3 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// qAllPassGraph: probe_1 with q1=pass, q2=pass and GAP set.
// FailedProbeBatches is empty. Use for TestQ_NoFailedProbeBatch_Exit1.
func qAllPassGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: some gap<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["also correct"]:::pass
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
`
}

// qNoFallbackGraph: probe_1 resolved with fail, GAP set, but no fallback probe batch.
func qNoFallbackGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: some gap<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// qProbeWrongConceptGraph: two concepts; othercon has probe_1 with q1=unclear and
// q2 unanswered (2 questions satisfies lint min). mycon has no batches.
// Using --re q1 with mycon must fail because q1 belongs to othercon.
func qProbeWrongConceptGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
        othercon["Other concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["ambiguous answer"]:::unclear
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        othercon --> q1
        q1 --> a1
        othercon --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef unclear stroke:#d29922
`
}

// qTeachWrongConceptGraph: mycon is teach-ready; othercon has probe_3 with q5=fail
// and q6 unanswered (2 questions satisfies lint min).
// Using --teach --re q5 with mycon must fail because q5 belongs to othercon.
func qTeachWrongConceptGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>GAP: the key insight was missed<br/>f5ca3875b379@src.txt:1-5"]
        othercon["Other concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["correct answer"]:::pass
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["wrong answer"]:::fail
        q3["Fallback probe 1<br/>e266782c2841@src.txt:1-2"]:::probe_2
        q4["Fallback probe 2<br/>20f437d6f701@src.txt:3-4"]:::probe_2
        q5["Other concept probe<br/>e266782c2841@src.txt:1-2"]:::probe_3
        a5["wrong answer"]:::fail
        q6["Other concept probe 2<br/>20f437d6f701@src.txt:3-4"]:::probe_3
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
        mycon --> q3
        mycon --> q4
        othercon --> q5
        q5 --> a5
        othercon --> q6
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef probe_3 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
`
}

// ── Exit 3 tests ──────────────────────────────────────────────────────────────

func TestQ_UnknownConcept_Exit3(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qSimpleGraph())

	_, errOut, code := run(t, "q", "nosuchconcept", "f5ca3875b379@src.txt:1-5", "some scope")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, `err: unknown concept "nosuchconcept"`) {
		t.Errorf("want unknown concept err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm q") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 3; got %v", rows)
	}
}

func TestQ_BadCitation_Parse_Exit3(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qSimpleGraph())

	_, errOut, code := run(t, "q", "mycon", "not-a-valid-cite", "some scope")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: citation") {
		t.Errorf("want citation err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm q") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 3; got %v", rows)
	}
}

func TestQ_BadCitation_OutOfBounds_Exit3(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qSimpleGraph())

	// src.txt has 10 lines; requesting line 200 is out of bounds.
	_, errOut, code := run(t, "q", "mycon", "src.txt:1-200", "some scope")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: citation") {
		t.Errorf("want citation err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm q") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 3; got %v", rows)
	}
}

func TestQ_UnknownReQID_Exit3(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qSimpleGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "some scope", "--re", "q99")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, `err: unknown question "q99"`) {
		t.Errorf("want unknown question err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm q") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 3 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 3; got %v", rows)
	}
}

// ── Exit 1 tests ──────────────────────────────────────────────────────────────

func TestQ_ConceptPassed_Exit1(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qPassedConceptGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "some scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon is passed") {
		t.Errorf("want 'is passed' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm reopen mycon") {
		t.Errorf("want 'tm reopen' fix; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ConceptGated_Exit1(t *testing.T) {
	// MaxFails=1 + probe_1 resolved with fail → Gated=true.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_MAX_FAILS", "1")
	qWriteGraph(t, dir, qReNotUnclearGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "some scope")
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

func TestQ_ProbeBatchLocked_Exit1(t *testing.T) {
	// Use the raft fixture: probe_2 is locked by teach_3.
	raftPath := raftFixture(t) // resolve before t.Chdir
	dir, errPath := qSetupDir(t)
	setupRaftSrcRoot(t) // override TM_SRC_ROOT → dir with raft.txt
	copyFixtureTo(t, raftPath, dir)

	_, errOut, code := run(t, "q", "log_matching", "6cabae64341e@raft.txt:229-240", "Why the induction needs the base case")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: probe_2 is locked by teach_3") {
		t.Errorf("want 'probe_2 is locked by teach_3' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix:") {
		t.Errorf("want fix: line; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ProbeBatchOpen_Exit1(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qOpenProbeGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "some scope")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: probe_1 is open") {
		t.Errorf("want 'probe_1 is open' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_TeachWithoutRe_Exit1(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachReadyGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: --teach requires --re") {
		t.Errorf("want '--teach requires --re' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_TeachingSpent_Exit1(t *testing.T) {
	// MaxTeach=1 + 1 teach question already → TeachingSpent=true.
	// TeachMax stays at default (3) so draft-batch-at-max does NOT fire first.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_MAX_TEACH", "1")
	qWriteGraph(t, dir, qTeachAtMaxGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: teaching is spent for mycon") {
		t.Errorf("want 'teaching is spent for mycon' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_NoGap_Exit1(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachReadyNoGapGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: mycon has no GAP") {
		t.Errorf("want 'has no GAP' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fix: tm gap mycon") {
		t.Errorf("want 'tm gap mycon' fix; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_NoFailedProbeBatch_Exit1(t *testing.T) {
	// probe_1 resolved with all pass → FailedProbeBatches is empty.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qAllPassGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q1")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: no failed probe batch above base for mycon") {
		t.Errorf("want 'no failed probe batch above base for mycon' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_NoFallbackProbe_Exit1(t *testing.T) {
	// probe_1 resolved with fail, but no fallback probe batch.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qNoFallbackGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: no fallback probe batch at minimum size") {
		t.Errorf("want 'no fallback probe batch at minimum size' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_TeachBatchOpen_Exit1(t *testing.T) {
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachOpenBatchGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: teach_3 is open") {
		t.Errorf("want 'teach_3 is open' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ReTeachNotFailOrUnclear_Exit1(t *testing.T) {
	// --teach --re q2 where q2's answer is pass (not fail/unclear).
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachPassTargetGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q2 answer is") {
		t.Errorf("want 'q2 answer is' err; got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "not fail or unclear") {
		t.Errorf("want 'not fail or unclear' in err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ReTeachWrongConcept_Exit1(t *testing.T) {
	// --teach --re q5 where q5 is a fail answer belonging to othercon, not mycon.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachWrongConceptGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q5")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q5 belongs to othercon, not mycon") {
		t.Errorf("want wrong-concept err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ReTeachOOS_Exit1(t *testing.T) {
	// --teach --re q2 where q2's answer is OOS.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qTeachOOSTargetGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "teach scope", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q2 answer is flagged OOS") {
		t.Errorf("want 'answer is flagged OOS' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ReNotUnclearProbe_Exit1(t *testing.T) {
	// --re q1 where q1's answer is pass (not unclear probe).
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qReNotUnclearGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "replacement scope", "--re", "q1")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 is not an unclear probe") {
		t.Errorf("want 'not an unclear probe' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ReProbeWrongConcept_Exit1(t *testing.T) {
	// --re q1 where q1 is an unclear probe belonging to othercon, not mycon.
	dir, errPath := qSetupDir(t)
	qWriteGraph(t, dir, qProbeWrongConceptGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "replacement scope", "--re", "q1")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: q1 belongs to othercon, not mycon") {
		t.Errorf("want wrong-concept err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_ProbeBatchAtMax_Exit1(t *testing.T) {
	// TM_PROBE_MAX=2 + probe_1 already has 2 unanswered questions.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_PROBE_MAX", "2")
	qWriteGraph(t, dir, qProbeAtMaxGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "extra probe")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: batch probe_1 is at maximum size (2)") {
		t.Errorf("want 'batch probe_1 is at maximum size (2)' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

func TestQ_TeachBatchAtMax_Exit1(t *testing.T) {
	// TM_TEACH_MAX=1 + teach_3 already has 1 question.
	// MaxTeach stays at default (8) so TeachingSpent does NOT fire first.
	dir, errPath := qSetupDir(t)
	t.Setenv("TM_TEACH_MAX", "1")
	qWriteGraph(t, dir, qTeachAtMaxGraph())

	_, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "extra teach", "--teach", "--re", "q2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d; stderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "err: batch teach_3 is at maximum size (1)") {
		t.Errorf("want 'batch teach_3 is at maximum size (1)' err; got:\n%s", errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 || rows[0].Exit != 1 {
		t.Errorf("ERRORS.jsonl: want 1 row exit 1; got %v", rows)
	}
}

// ── Happy path tests ──────────────────────────────────────────────────────────

func TestQ_ProbeHappyPath(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, qSimpleGraph())

	// Add first probe question.
	out, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "First probe question")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	gotQID := strings.TrimSpace(out)
	if gotQID != "q1" {
		t.Errorf("want stdout 'q1', got %q", gotQID)
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

	// q1 must be in testing block with class probe_1.
	var q1 *graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == "q1" {
			q1 = item.Q
			break
		}
	}
	if q1 == nil {
		t.Fatal("q1 not found in testing block")
	}
	if q1.Class != "probe_1" {
		t.Errorf("q1.Class: want 'probe_1', got %q", q1.Class)
	}
	if q1.Scope != "First probe question" {
		t.Errorf("q1.Scope: want 'First probe question', got %q", q1.Scope)
	}
	if q1.Cite != "f5ca3875b379@src.txt:1-5" {
		t.Errorf("q1.Cite: want 'f5ca3875b379@src.txt:1-5', got %q", q1.Cite)
	}

	// Edge mycon → q1 must exist.
	edgeFound := false
	for _, e := range g.Edges {
		if e.From == "mycon" && e.To == "q1" {
			edgeFound = true
			break
		}
	}
	if !edgeFound {
		t.Error("edge mycon→q1 not found")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: one "q" event with correct fields.
	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("no events in event log")
	}
	last := rows[len(rows)-1]
	if last["ev"] != "q" {
		t.Errorf("last event ev: want 'q', got %v", last["ev"])
	}
	if last["q"] != "q1" {
		t.Errorf("event q: want 'q1', got %v", last["q"])
	}
	if last["concept"] != "mycon" {
		t.Errorf("event concept: want 'mycon', got %v", last["concept"])
	}
	if last["batch"] != "probe_1" {
		t.Errorf("event batch: want 'probe_1', got %v", last["batch"])
	}
	if last["kind"] != "probe" {
		t.Errorf("event kind: want 'probe', got %v", last["kind"])
	}
	if last["scope"] != "First probe question" {
		t.Errorf("event scope: want 'First probe question', got %v", last["scope"])
	}
	if last["src"] != "f5ca3875b379@src.txt:1-5" {
		t.Errorf("event src: want 'f5ca3875b379@src.txt:1-5', got %v", last["src"])
	}
	if last["re"] != "" {
		t.Errorf("event re: want '', got %v", last["re"])
	}

	// Add second probe → must go to same batch (probe_1).
	out2, errOut2, code2 := run(t, "q", "mycon", "25070e52a6ae@src.txt:2-4", "Second probe question")
	if code2 != 0 {
		t.Fatalf("second q: want exit 0, got %d; stderr:\n%s", code2, errOut2)
	}
	if strings.TrimSpace(out2) != "q2" {
		t.Errorf("second q: want 'q2', got %q", strings.TrimSpace(out2))
	}
	data2, _ := os.ReadFile(file)
	g2, _ := graph.Parse(data2)
	probeCount := 0
	for _, item := range g2.TestingItems {
		if item.Q != nil && item.Q.Class == "probe_1" {
			probeCount++
		}
	}
	if probeCount != 2 {
		t.Errorf("want 2 probe_1 questions after second q, got %d", probeCount)
	}
}

func TestQ_TeachHappyPath(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, qTeachReadyGraph())

	// Add teach question targeting q2 (fail answer).
	out, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "Teach scope for q2", "--teach", "--re", "q2")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	gotQID := strings.TrimSpace(out)
	if gotQID != "q5" {
		t.Errorf("want 'q5', got %q", gotQID)
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

	// q5 must be in testing block with class teach_3.
	var q5 *graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == "q5" {
			q5 = item.Q
			break
		}
	}
	if q5 == nil {
		t.Fatal("q5 not found in testing block")
	}
	if q5.Class != "teach_3" {
		t.Errorf("q5.Class: want 'teach_3', got %q", q5.Class)
	}
	if q5.Scope != "Teach scope for q2" {
		t.Errorf("q5.Scope: want 'Teach scope for q2', got %q", q5.Scope)
	}

	// Edge a2 → q5 must exist.
	edgeFound := false
	for _, e := range g.Edges {
		if e.From == "a2" && e.To == "q5" {
			edgeFound = true
			break
		}
	}
	if !edgeFound {
		t.Error("edge a2→q5 not found")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: "q" event with kind=teach, batch=teach_3, re="q2".
	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("no events in event log")
	}
	last := rows[len(rows)-1]
	if last["ev"] != "q" {
		t.Errorf("last event ev: want 'q', got %v", last["ev"])
	}
	if last["q"] != "q5" {
		t.Errorf("event q: want 'q5', got %v", last["q"])
	}
	if last["kind"] != "teach" {
		t.Errorf("event kind: want 'teach', got %v", last["kind"])
	}
	if last["batch"] != "teach_3" {
		t.Errorf("event batch: want 'teach_3', got %v", last["batch"])
	}
	if last["re"] != "q2" {
		t.Errorf("event re: want 'q2', got %v", last["re"])
	}
}

func TestQ_ProbeReplacement_HappyPath(t *testing.T) {
	// Probe replacement: --re on a probe whose answer graded unclear.
	dir, _ := qSetupDir(t)
	mmd := qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
    end
    subgraph untested["Concepts User has not been tested on"]
        mycon["My concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
        q1["First probe<br/>e266782c2841@src.txt:1-2"]:::probe_1
        a1["ambiguous answer"]:::unclear
        q2["Second probe<br/>20f437d6f701@src.txt:3-4"]:::probe_1
        a2["correct answer"]:::pass
        mycon --> q1
        q1 --> a1
        mycon --> q2
        q2 --> a2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef unclear stroke:#d29922
    classDef pass stroke:#3fb950
`
	file := qWriteGraph(t, dir, mmd)

	// Add replacement probe for unclear q1.
	out, errOut, code := run(t, "q", "mycon", "f5ca3875b379@src.txt:1-5", "Replacement for unclear q1", "--re", "q1")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	gotQID := strings.TrimSpace(out)
	if gotQID != "q3" {
		t.Errorf("want 'q3', got %q", gotQID)
	}

	// Verify edge a1 → q3.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	g, parseErr := graph.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse graph: %v", parseErr)
	}

	edgeFound := false
	for _, e := range g.Edges {
		if e.From == "a1" && e.To == "q3" {
			edgeFound = true
			break
		}
	}
	if !edgeFound {
		t.Error("edge a1→q3 not found for replacement probe")
	}

	// Lint passes.
	lintFile(t, file, dir)

	// Event log: kind=probe, re="q1".
	rows := readEventLog(t, file)
	if len(rows) == 0 {
		t.Fatal("no events in event log")
	}
	last := rows[len(rows)-1]
	if last["kind"] != "probe" {
		t.Errorf("event kind: want 'probe', got %v", last["kind"])
	}
	if last["re"] != "q1" {
		t.Errorf("event re: want 'q1', got %v", last["re"])
	}
}
