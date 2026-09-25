package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/lint"
)

// defaultCfg returns a Config with the spec defaults and no SrcRoot.
func defaultCfg() lint.Config {
	return lint.Config{ProbeMin: 2, ProbeMax: 5, TeachMin: 1, TeachMax: 3}
}

// cfgWithSrc returns a Config with the given SrcRoot and spec defaults.
func cfgWithSrc(dir string) lint.Config {
	c := defaultCfg()
	c.SrcRoot = dir
	return c
}

// hasMsg reports whether viols contains a violation with the exact message msg.
func hasMsg(viols []lint.Violation, msg string) bool {
	for _, v := range viols {
		if v.Msg == msg {
			return true
		}
	}
	return false
}

// hasMsgContaining reports whether any violation message contains sub.
func hasMsgContaining(viols []lint.Violation, sub string) bool {
	for _, v := range viols {
		if strings.Contains(v.Msg, sub) {
			return true
		}
	}
	return false
}

// hasMsgAtLine reports whether viols contains a violation on lineNum with msg.
func hasMsgAtLine(viols []lint.Violation, lineNum int, msg string) bool {
	for _, v := range viols {
		if v.Line == lineNum && v.Msg == msg {
			return true
		}
	}
	return false
}

// makeLines generates n numbered text lines "line 1\nline 2\n..." as a byte
// slice, suitable for populating citation source files in tests.
func makeLines(n int) []byte {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return []byte(b.String())
}

// TestParseFailure verifies that a bad file yields a single file-level
// "invalid graph: …" violation and no further checks are run.
func TestParseFailure(t *testing.T) {
	data := []byte("not a valid flowchart")
	viols := lint.Check(data, defaultCfg())
	if len(viols) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(viols), viols)
	}
	if !strings.HasPrefix(viols[0].Msg, "invalid graph: ") {
		t.Errorf("unexpected message: %q", viols[0].Msg)
	}
	if viols[0].Line != 0 {
		t.Errorf("expected Line==0, got %d", viols[0].Line)
	}
}

// check4 ─────────────────────────────────────────────────────────────────────

// graph with foo declared in both passed and untested.
const dupConceptGraph = `flowchart TB
    subgraph passed["P"]
        foo["Concept A"]
    end
    subgraph untested["U"]
        foo["Concept B"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck4_DuplicateConceptDeclaration(t *testing.T) {
	viols := lint.Check([]byte(dupConceptGraph), defaultCfg())
	want := `duplicate declaration of node "foo"`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// graph with q1 declared twice in testing.
const dupQGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        q1["Q1b"]:::probe_1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck4_DuplicateQuestionDeclaration(t *testing.T) {
	viols := lint.Check([]byte(dupQGraph), defaultCfg())
	want := `duplicate declaration of node "q1"`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check5 ─────────────────────────────────────────────────────────────────────

// Declaration on line 6 appears after an edge on line 5 inside the passed block.
const declAfterEdgeGraph = `flowchart TB
    subgraph passed["P"]
        foo["A"]
        bar["B"]
        foo --"x"--> bar
        baz["C"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck5_DeclarationAfterEdge(t *testing.T) {
	viols := lint.Check([]byte(declAfterEdgeGraph), defaultCfg())
	want := `declaration follows an edge in block passed`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
	// Confirm line number: baz is on line 6.
	if !hasMsgAtLine(viols, 6, want) {
		t.Errorf("expected violation on line 6; got %v", viols)
	}
}

// foo→baz edge is written in the passed block but belongs in untested (baz is untested).
const wrongBlockGraph = `flowchart TB
    subgraph passed["P"]
        foo["A"]
        foo --"y"--> baz
    end
    subgraph untested["U"]
        baz["C"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck5_EdgeInWrongBlock(t *testing.T) {
	viols := lint.Check([]byte(wrongBlockGraph), defaultCfg())
	want := `edge foo --> baz belongs in block untested`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// The edge "ghost" endpoint is not a declared node.
const unknownNodeGraph = `flowchart TB
    subgraph passed["P"]
        foo["A"]
        foo --> ghost
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck5_UnknownNode(t *testing.T) {
	viols := lint.Check([]byte(unknownNodeGraph), defaultCfg())
	want := `edge references unknown node "ghost"`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check7 ─────────────────────────────────────────────────────────────────────

const badBatchClassGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        q1["Q"]:::bad_class
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck7_InvalidBatchClass(t *testing.T) {
	viols := lint.Check([]byte(badBatchClassGraph), defaultCfg())
	want := `question "q1" has invalid batch class "bad_class"`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

const badAnswerClassGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        a1["A"]:::not_an_answer_class
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck7_InvalidAnswerClass(t *testing.T) {
	viols := lint.Check([]byte(badAnswerClassGraph), defaultCfg())
	want := `answer "a1" has invalid answer class "not_an_answer_class"`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check8 ─────────────────────────────────────────────────────────────────────

// a1 has no incoming edge.
const answerNoIncomingGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        a1["A"]:::pass
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck8_AnswerMissingIncoming(t *testing.T) {
	viols := lint.Check([]byte(answerNoIncomingGraph), defaultCfg())
	want := `answer "a1" must have exactly one incoming edge`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// a1 (N=1) is fed by q2 (N=2) instead of q1.
const answerWrongSourceGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q2["Q2"]:::probe_1
        a1["A1"]:::pass
        c1 --> q2
        q2 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck8_AnswerWrongSource(t *testing.T) {
	viols := lint.Check([]byte(answerWrongSourceGraph), defaultCfg())
	want := `answer "a1" must be fed by q1`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// q1 (probe) has incoming from a3 whose class is "fail" (not "unclear").
const probeFromFailAnswerGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a3["A3"]:::fail
        a3 --> q1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck8_ProbeFromFailAnswer(t *testing.T) {
	viols := lint.Check([]byte(probeFromFailAnswerGraph), defaultCfg())
	want := `probe "q1" must originate at its concept or an unclear answer`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// q1 (teach_1) has incoming from c1 (concept), not an answer.
const teachFromConceptGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::teach_1
        c1 --> q1
    end
    classDef teach_1 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck8_TeachFromConcept(t *testing.T) {
	viols := lint.Check([]byte(teachFromConceptGraph), defaultCfg())
	want := `teach question "q1" must follow an answer`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// q3 (teach) is fed by a1 (pass answer), meaning the root probe q1 has a pass
// answer — chain does not end at failed or unclear.
const chainPassedProbeGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a1["A1"]:::pass
        q3["Q3"]:::teach_1
        c1 --> q1
        q1 --> a1
        a1 --> q3
    end
    classDef probe_1 stroke:#4aa3ff
    classDef teach_1 stroke:#c9a227
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck8_ChainMustEndAtFailedProbe(t *testing.T) {
	viols := lint.Check([]byte(chainPassedProbeGraph), defaultCfg())
	want := `question "q3" chain must end at a failed or unclear probe`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check9 ─────────────────────────────────────────────────────────────────────

// probe_1 has questions resolving to two different concepts.
const batchMultiConceptGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept 1"]
        c2["Concept 2"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        q2["Q2"]:::probe_1
        c1 --> q1
        c2 --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck9_BatchSpansMultipleConcepts(t *testing.T) {
	viols := lint.Check([]byte(batchMultiConceptGraph), defaultCfg())
	want := `batch probe_1 spans multiple concepts`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// probe_1 has 6 questions (exceeds ProbeMax=5) and answers, triggering both max and min checks.
// (We expect "exceeds max 5"; the below-min check does not fire because size>min.)
const batchExceedsMaxGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        q2["Q2"]:::probe_1
        q3["Q3"]:::probe_1
        q4["Q4"]:::probe_1
        q5["Q5"]:::probe_1
        q6["Q6"]:::probe_1
        a1["A1"]:::pass
        c1 --> q1
        c1 --> q2
        c1 --> q3
        c1 --> q4
        c1 --> q5
        c1 --> q6
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck9_BatchExceedsMax(t *testing.T) {
	viols := lint.Check([]byte(batchExceedsMaxGraph), defaultCfg())
	want := `batch probe_1 has 6 questions, exceeds max 5`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// probe_1 has 1 question with an answer — below ProbeMin=2.
const batchBelowMinGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a1["A1"]:::pass
        c1 --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck9_BatchBelowMin(t *testing.T) {
	viols := lint.Check([]byte(batchBelowMinGraph), defaultCfg())
	want := `batch probe_1 has 1 questions, below min 2`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Replacement probe batch (all questions from unclear answers) is exempt from min/max.
const replacementBatchGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a1["A1"]:::unclear
        q2["Q2"]:::probe_2
        c1 --> q1
        q1 --> a1
        a1 --> q2
    end
    classDef probe_1 stroke:#4aa3ff
    classDef probe_2 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck9_ReplacementBatchExempt(t *testing.T) {
	viols := lint.Check([]byte(replacementBatchGraph), defaultCfg())
	// probe_2 has 1 question (below ProbeMin=2) but it is a replacement batch;
	// no size violation should be emitted for probe_2.
	for _, v := range viols {
		if strings.Contains(v.Msg, "probe_2") && (strings.Contains(v.Msg, "below min") || strings.Contains(v.Msg, "exceeds max")) {
			t.Errorf("replacement batch probe_2 should be exempt from size checks; got: %s", v.Msg)
		}
	}
}

// check10 ────────────────────────────────────────────────────────────────────

// Concept edge with an empty relation label.
const noRelLabelGraph = `flowchart TB
    subgraph passed["P"]
        a_concept["A"]
    end
    subgraph untested["U"]
        b_concept["B"]
        a_concept --> b_concept
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck10_NoRelationLabel(t *testing.T) {
	viols := lint.Check([]byte(noRelLabelGraph), defaultCfg())
	want := `concept edge a_concept --> b_concept has no relation label`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Concept edges form a cycle: a→b→c→a.
const conceptCycleGraph = `flowchart TB
    subgraph passed["P"]
        a_concept["A"]
    end
    subgraph untested["U"]
        b_concept["B"]
        c_concept["C"]
        a_concept --"x"--> b_concept
        b_concept --"y"--> c_concept
        c_concept --"z"--> a_concept
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck10_ConceptCycle(t *testing.T) {
	viols := lint.Check([]byte(conceptCycleGraph), defaultCfg())
	want := `concept edges must form a DAG`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check11 ────────────────────────────────────────────────────────────────────
//
// check11 is now static: it checks citation syntax, hash presence, and rejects
// raw '"' in locators. File existence and bounds are validated at write time
// (tm add/q/edit) and via tm lint --drift, not by static lint.

func makeConceptGraph(conceptCite string) []byte {
	return []byte(fmt.Sprintf(`flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["%s"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`, conceptCite))
}

// TestCheck11_HashlessConceptCitation verifies that a hashless citation on a
// concept node produces a lint violation.
func TestCheck11_HashlessConceptCitation(t *testing.T) {
	data := makeConceptGraph(`Concept<br/>raft.txt:1-5`)
	viols := lint.Check(data, defaultCfg())
	if !hasMsgContaining(viols, `concept "c1"`) || !hasMsgContaining(viols, `missing a hash`) {
		t.Errorf("expected hashless citation error for concept; got %v", viols)
	}
}

// TestCheck11_HashlessQuestionCitation verifies that a hashless citation on a
// question node produces a lint violation.
func TestCheck11_HashlessQuestionCitation(t *testing.T) {
	data := []byte(`flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        c1["Concept"]
    end
    subgraph testing["T"]
        q1["Q<br/>src.txt:1-3"]:::probe_1
        c1 --> q1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`)
	viols := lint.Check(data, defaultCfg())
	if !hasMsgContaining(viols, `question "q1"`) || !hasMsgContaining(viols, `missing a hash`) {
		t.Errorf("expected hashless citation error for question q1; got %v", viols)
	}
}

// TestCheck11_HashedCitationNoError verifies that a properly hashed citation
// passes lint without any file I/O.
func TestCheck11_HashedCitationNoError(t *testing.T) {
	// This file does not exist — lint must not attempt to open it.
	data := makeConceptGraph(`Concept<br/>3f9a1c2b7e0d@nonexistent.txt:1-5`)
	viols := lint.Check(data, defaultCfg())
	// Filter to check11-specific messages only (hash/quote violations).
	var check11Viols []lint.Violation
	for _, v := range viols {
		if strings.Contains(v.Msg, "missing a hash") || strings.Contains(v.Msg, "locator contains") {
			check11Viols = append(check11Viols, v)
		}
	}
	if len(check11Viols) != 0 {
		t.Errorf("expected no check11 violations for hashed citation; got %v", check11Viols)
	}
}

// TestCheck11_InvalidCitationSyntax verifies that a syntactically invalid
// citation still produces a parse-error violation.
func TestCheck11_InvalidCitationSyntax(t *testing.T) {
	// "raft.txt" has no colon → parse error.
	data := makeConceptGraph(`Concept<br/>raft.txt`)
	viols := lint.Check(data, defaultCfg())
	if !hasMsgContaining(viols, `concept "c1"`) {
		t.Errorf("expected citation parse error for concept; got %v", viols)
	}
}

// check12 ────────────────────────────────────────────────────────────────────

// Passed concept with a non-empty GAP.
const passedGAPGraph = `flowchart TB
    subgraph passed["P"]
        c1["Concept<br/>GAP: some gap"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck12_PassedConceptHasGAP(t *testing.T) {
	viols := lint.Check([]byte(passedGAPGraph), defaultCfg())
	want := `passed concept "c1" has a GAP`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Passed concept c1 has a gate meta line targeting it.
const passedGateLineGraph = `flowchart TB
    subgraph passed["P"]
        c1["Concept"]
    end
    subgraph untested["U"]
        %% tm:gate c1 base=1
        c2["Other"]
        c1 --"x"--> c2
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck12_PassedConceptHasGateLine(t *testing.T) {
	viols := lint.Check([]byte(passedGateLineGraph), defaultCfg())
	want := `passed concept "c1" has a gate line`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Passed concept c1 has a question q1 that resolves to it.
const passedHasQuestionsGraph = `flowchart TB
    subgraph passed["P"]
        c1["Concept"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a1["A1"]:::fail
        c1 --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck12_PassedConceptHasOpenQuestions(t *testing.T) {
	viols := lint.Check([]byte(passedHasQuestionsGraph), defaultCfg())
	want := `passed concept "c1" has open questions`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Sorting ─────────────────────────────────────────────────────────────────────

// TestSortOrder verifies that violations with a positive line sort before
// file-level (Line==0) violations, and that relative order is stable.
func TestSortOrder(t *testing.T) {
	// This graph triggers a check5 violation (line 5) and check4 violations (Line=0).
	const g = `flowchart TB
    subgraph passed["P"]
        foo["A"]
        foo --"x"--> foo
        bar["B"]
    end
    subgraph untested["U"]
        foo["A2"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	viols := lint.Check([]byte(g), defaultCfg())
	// Find first Line>0 and first Line==0 positions.
	firstPositive := -1
	lastZero := -1
	for i, v := range viols {
		if v.Line > 0 && firstPositive < 0 {
			firstPositive = i
		}
		if v.Line == 0 {
			lastZero = i
		}
	}
	if firstPositive >= 0 && lastZero >= 0 && firstPositive > lastZero {
		t.Errorf("line>0 violations must come before line==0; got %v", viols)
	}
}

// Valid regressions ───────────────────────────────────────────────────────────

// TestValidRegression verifies that the checked-in golden fixtures parse and
// lint without any violations when given valid citation source files.
func TestValidRegression(t *testing.T) {
	dir := t.TempDir()

	// raft.mmd cites raft.txt with lines up to 300.
	if err := os.WriteFile(filepath.Join(dir, "raft.txt"), makeLines(300), 0o644); err != nil {
		t.Fatal(err)
	}
	// gated.mmd cites algo.txt with lines up to 150.
	if err := os.WriteFile(filepath.Join(dir, "algo.txt"), makeLines(150), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../../testdata/raft.mmd",
		"../../testdata/gated_valid.mmd",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			viols := lint.Check(data, cfgWithSrc(dir))
			if len(viols) != 0 {
				t.Errorf("%s: expected zero violations, got %d:", path, len(viols))
				for _, v := range viols {
					if v.Line > 0 {
						t.Errorf("  line %d: %s", v.Line, v.Msg)
					} else {
						t.Errorf("  %s", v.Msg)
					}
				}
			}
		})
	}
}

// check1 (UTF-8) ──────────────────────────────────────────────────────────────

// TestCheck1_InvalidUTF8 verifies that a file with invalid UTF-8 bytes returns
// a single file-level violation before any parsing is attempted.
func TestCheck1_InvalidUTF8(t *testing.T) {
	data := []byte("flowchart TB\n    subgraph passed[\"\xff\"]\n    end\n")
	viols := lint.Check(data, defaultCfg())
	if len(viols) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(viols), viols)
	}
	if viols[0].Msg != "file is not valid UTF-8" {
		t.Errorf("unexpected message: %q", viols[0].Msg)
	}
	if viols[0].Line != 0 {
		t.Errorf("expected Line==0, got %d", viols[0].Line)
	}
}

// check6 (valid concept IDs) ──────────────────────────────────────────────────

const invalidConceptIDGraph = `flowchart TB
    subgraph passed["P"]
        BadID["invalid uppercase ID"]
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck6_InvalidConceptID(t *testing.T) {
	viols := lint.Check([]byte(invalidConceptIDGraph), defaultCfg())
	if !hasMsgContaining(viols, `invalid concept ID`) {
		t.Errorf("expected invalid-concept-ID violation; got %v", viols)
	}
}

// check7 (concepts none) ──────────────────────────────────────────────────────

const conceptWithClassGraph = `flowchart TB
    subgraph passed["P"]
        foo["Concept A"]:::someclass
    end
    subgraph untested["U"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck7_ConceptMustNotHaveClass(t *testing.T) {
	viols := lint.Check([]byte(conceptWithClassGraph), defaultCfg())
	if !hasMsgContaining(viols, `must not carry a class`) {
		t.Errorf("expected concept-with-class violation; got %v", viols)
	}
}

// ── Reserve block lint checks ─────────────────────────────────────────────────

const reserveConceptWithClassGraph = `flowchart TB
    subgraph passed["P"]
        c2["Concept2"]
    end
    subgraph untested["U"]
        c3["Other"]
    end
    subgraph reserve["Concepts held in reserve"]
        c1["ReserveConcept"]:::someclass
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck7_ReserveConceptMustNotHaveClass(t *testing.T) {
	viols := lint.Check([]byte(reserveConceptWithClassGraph), defaultCfg())
	if !hasMsgContaining(viols, `must not carry a class`) {
		t.Errorf("expected concept-with-class violation for reserve concept; got %v", viols)
	}
}

// Reserve concept c1 has a gate meta line targeting it in the untested block.
const reserveConceptWithGateLineGraph = `flowchart TB
    subgraph passed["P"]
        c2["Concept2"]
    end
    subgraph untested["U"]
        %% tm:gate c1 base=1
        c3["Other"]
    end
    subgraph reserve["Concepts held in reserve"]
        c1["ReserveConcept"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck12_ReserveConceptHasGateLine(t *testing.T) {
	viols := lint.Check([]byte(reserveConceptWithGateLineGraph), defaultCfg())
	want := `reserve concept "c1" has a gate line`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// Reserve concept c1 has a question in testing.
const reserveConceptHasQuestionsGraph = `flowchart TB
    subgraph passed["P"]
        c2["Concept2"]
    end
    subgraph untested["U"]
        c3["Other"]
    end
    subgraph reserve["Concepts held in reserve"]
        c1["ReserveConcept"]
    end
    subgraph testing["T"]
        q1["Q1"]:::probe_1
        a1["A1"]:::fail
        c1 --> q1
        q1 --> a1
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck12_ReserveConceptHasQuestions(t *testing.T) {
	viols := lint.Check([]byte(reserveConceptHasQuestionsGraph), defaultCfg())
	want := `reserve concept "c1" has questions`
	if !hasMsg(viols, want) {
		t.Errorf("expected %q; got %v", want, viols)
	}
}

// check5 (edge in wrong block involving reserve) ───────────────────────────────

// An edge from a passed concept to a reserve concept belongs in the reserve
// block (§4.2), but here it is written in the passed block.
const reserveEdgeInWrongBlockGraph = `flowchart TB
    subgraph passed["P"]
        c1["Concept1"]
        c1 --"x"--> c2
    end
    subgraph untested["U"]
    end
    subgraph reserve["Concepts held in reserve"]
        c2["Concept2"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck5_ReserveEdgeInWrongBlock(t *testing.T) {
	viols := lint.Check([]byte(reserveEdgeInWrongBlockGraph), defaultCfg())
	if !hasMsgContaining(viols, "reserve") {
		t.Errorf("expected edge-in-wrong-block violation mentioning reserve; got %v", viols)
	}
}

// ── Check 15: tm:next counter validation ────────────────────────────────────

// A valid graph with %% tm:next where counters exceed all IDs in the file.
const check15ValidGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=4 batch=3
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
        q1["Q1<br/>3f9a1c2b7e0d@src.txt:1-2"]:::probe_1
        q2["Q2<br/>3f9a1c2b7e0d@src.txt:1-2"]:::probe_1
        a1["A1"]:::pass
        a2["A2"]:::pass
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck15_ValidCounters_NoViolation(t *testing.T) {
	viols := lint.Check([]byte(check15ValidGraph), defaultCfg())
	for _, v := range viols {
		if strings.Contains(v.Msg, "tm:next") {
			t.Errorf("unexpected check15 violation: %q", v.Msg)
		}
	}
}

func TestCheck15_Absent_NoViolation(t *testing.T) {
	// A graph without tm:next must not trigger check15.
	const noNextGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
        q1["Q1<br/>3f9a1c2b7e0d@src.txt:1-2"]:::probe_1
        a1["A1"]:::pass
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	viols := lint.Check([]byte(noNextGraph), defaultCfg())
	for _, v := range viols {
		if strings.Contains(v.Msg, "tm:next") {
			t.Errorf("unexpected check15 violation when line absent: %q", v.Msg)
		}
	}
}

// q counter equals a qN suffix (not strictly greater) → violation.
const check15QTooLowGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=2 batch=5
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
        q2["Q2<br/>3f9a1c2b7e0d@src.txt:1-2"]:::probe_1
        a2["A2"]:::pass
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck15_QTooLow_Violation(t *testing.T) {
	viols := lint.Check([]byte(check15QTooLowGraph), defaultCfg())
	if !hasMsgContaining(viols, "tm:next") || !hasMsgContaining(viols, "q2") || !hasMsgContaining(viols, "raise the counters") {
		t.Errorf("expected check15 violation for q counter too low; got %v", viols)
	}
}

// batch counter equals a probe_N suffix (not strictly greater) → violation.
const check15BatchTooLowGraph = `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=5 batch=1
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
        q1["Q1<br/>3f9a1c2b7e0d@src.txt:1-2"]:::probe_1
        a1["A1"]:::pass
    end
    classDef probe_1 stroke:#4aa3ff
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

func TestCheck15_BatchTooLow_Violation(t *testing.T) {
	viols := lint.Check([]byte(check15BatchTooLowGraph), defaultCfg())
	if !hasMsgContaining(viols, "tm:next") || !hasMsgContaining(viols, "probe_1") || !hasMsgContaining(viols, "raise the counters") {
		t.Errorf("expected check15 violation for batch counter too low; got %v", viols)
	}
}

// ── check 17: %% tm:format ────────────────────────────────────────────────────

func minimalFormatGraph(n int) string {
	fmtLine := ""
	if n >= 0 {
		fmtLine = fmt.Sprintf("        %%%% tm:format %d\n", n)
	}
	return fmt.Sprintf(`flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
%s    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`, fmtLine)
}

func TestCheck17_FormatAboveBinaryRefuses(t *testing.T) {
	// A graph with %% tm:format 3 on a binary that knows format 2 should refuse.
	src := minimalFormatGraph(3)
	viols := lint.Check([]byte(src), defaultCfg())
	if !hasMsgContaining(viols, "above this binary") {
		t.Errorf("expected 'above this binary' violation; got: %v", viols)
	}
	if !hasMsgContaining(viols, "fix: upgrade tm") {
		t.Errorf("expected 'fix: upgrade tm' in violation; got: %v", viols)
	}
}

func TestCheck17_FormatAtBinaryAccepts(t *testing.T) {
	// Format 2 == CurrentFormat → no violation.
	src := minimalFormatGraph(2)
	viols := lint.Check([]byte(src), defaultCfg())
	for _, v := range viols {
		if strings.Contains(v.Msg, "above this binary") {
			t.Errorf("unexpected format violation for format 2: %v", viols)
		}
	}
}

func TestCheck17_FormatBelowBinaryAccepts(t *testing.T) {
	// Format 1 < CurrentFormat → no violation (migrate matter, not a lint failure).
	src := minimalFormatGraph(1)
	viols := lint.Check([]byte(src), defaultCfg())
	for _, v := range viols {
		if strings.Contains(v.Msg, "above this binary") {
			t.Errorf("unexpected format violation for format 1: %v", viols)
		}
	}
}

func TestCheck17_FormatAbsentAccepts(t *testing.T) {
	// No %% tm:format line → format 1 implicitly; no violation.
	src := minimalFormatGraph(-1)
	viols := lint.Check([]byte(src), defaultCfg())
	for _, v := range viols {
		if strings.Contains(v.Msg, "above this binary") {
			t.Errorf("unexpected format violation when line absent: %v", viols)
		}
	}
}

// check14 ─────────────────────────────────────────────────────────────────────

// TestCheck14_GitLocatorRawColon verifies that a git: locator with a raw ":"
// in the path (which should be percent-encoded as %3A) produces a lint
// violation even though cite.Parse accepts the citation (it splits on the
// last colon to extract the range, so the raw colon ends up in the locator).
func TestCheck14_GitLocatorRawColon(t *testing.T) {
	// Citation: hash@git:r@sha1234567890:file:raw:1-5
	// cite.Parse splits on last ":" → locator = "git:r@sha1234567890:file:raw"
	// cite.ParseGit("git:r@sha1234567890:file:raw") → rawPath="file:raw" → error.
	data := makeConceptGraph(`Concept<br/>abcdef012345@git:r@sha1234567890:file:raw:1-5`)
	viols := lint.Check(data, defaultCfg())
	found := false
	for _, v := range viols {
		if strings.Contains(v.Msg, "raw :") || strings.Contains(v.Msg, "percent-encode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected git: structural error violation; got %v", viols)
	}
}

// check16 ─────────────────────────────────────────────────────────────────────

// makeConceptGraphWithCite returns a minimal graph where concept c1 cites the
// given raw citation string, for testing check16 with a custom citation.
func makeConceptGraphWithCite(rawCite string) []byte {
	escaped := strings.ReplaceAll(rawCite, `"`, `#quot;`)
	return []byte(`flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:format 2
        c1["Concept scope<br/>` + escaped + `"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`)
}

// cfgWithAids returns a config with the given AidsDir and spec defaults.
func cfgWithAids(aidsDir string) lint.Config {
	c := defaultCfg()
	c.AidsDir = aidsDir
	return c
}

// TestCheck16_CitationInsideAidsDir verifies that a citation resolving inside
// the configured aids dir produces a violation with the expected fix text.
func TestCheck16_CitationInsideAidsDir(t *testing.T) {
	data := makeConceptGraphWithCite("aids/doc.txt:1-3")
	viols := lint.Check(data, cfgWithAids("aids"))
	if !hasMsgContaining(viols, "cites aid") {
		t.Errorf("expected check16 violation for aid citation; got %v", viols)
	}
	if !hasMsgContaining(viols, "tm aid") {
		t.Errorf("expected 'tm aid' in fix text; got %v", viols)
	}
}

// TestCheck16_SiblingPrefixNoViolation verifies that a citation inside aids2/
// (a directory sharing a prefix with aids/) is not refused.
func TestCheck16_SiblingPrefixNoViolation(t *testing.T) {
	data := makeConceptGraphWithCite("aids2/doc.txt:1-3")
	viols := lint.Check(data, cfgWithAids("aids"))
	for _, v := range viols {
		if strings.Contains(v.Msg, "cites aid") {
			t.Errorf("unexpected check16 violation for sibling-prefix path; got %v", v.Msg)
		}
	}
}

// TestCheck16_URINotRefused verifies that a URI citation is never treated as
// an aid-dir citation regardless of the configured aids-dir.
func TestCheck16_URINotRefused(t *testing.T) {
	data := makeConceptGraphWithCite("3f9a1c2b7e0d@https://example.com/aids/doc.txt:1-3")
	viols := lint.Check(data, cfgWithAids("aids"))
	for _, v := range viols {
		if strings.Contains(v.Msg, "cites aid") {
			t.Errorf("unexpected check16 violation for URI citation; got %v", v.Msg)
		}
	}
}

// TestCheck16_NoAidsDirSkipped verifies that check16 produces no violations
// when cfg.AidsDir is empty (feature disabled).
func TestCheck16_NoAidsDirSkipped(t *testing.T) {
	data := makeConceptGraphWithCite("aids/doc.txt:1-3")
	viols := lint.Check(data, defaultCfg()) // AidsDir == ""
	for _, v := range viols {
		if strings.Contains(v.Msg, "cites aid") {
			t.Errorf("unexpected check16 violation when AidsDir is empty; got %v", v.Msg)
		}
	}
}
