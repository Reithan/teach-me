package graph

import (
	"bytes"
	"os"
	"testing"
)

// TestGoldenRoundTrip parses testdata/raft.mmd and verifies that Write
// produces byte-identical output.
func TestGoldenRoundTrip(t *testing.T) {
	t.Helper()
	path := "../../testdata/raft.mmd"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	g, err := Parse(want)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got := Write(g)

	if !bytes.Equal(got, want) {
		// Produce a per-line diff for easier debugging.
		wantLines := bytes.Split(want, []byte("\n"))
		gotLines := bytes.Split(got, []byte("\n"))
		n := len(wantLines)
		if len(gotLines) > n {
			n = len(gotLines)
		}
		t.Errorf("Write output is not byte-identical to %s", path)
		for i := range n {
			var w, g2 []byte
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if i < len(gotLines) {
				g2 = gotLines[i]
			}
			if !bytes.Equal(w, g2) {
				t.Errorf("  line %d:\n    want: %q\n    got:  %q", i+1, w, g2)
			}
		}
	}
}

// TestEscape verifies the escaping of every special character and the
// round-trip identity property.
func TestEscape(t *testing.T) {
	cases := []struct {
		name    string
		plain   string
		escaped string
	}{
		{
			name:    "double quote",
			plain:   `it's the "same" entry`,
			escaped: `it#39;s the #quot;same#quot; entry`,
		},
		{
			// Spec 4.4 shows this string in the Mermaid file to illustrate that
			// escape sequences parse cleanly on Mermaid 11.17.2. Per the spec
			// escaping rule, ' → #39;, " → #quot;, > → #gt;. The % is not in
			// the escape set and passes through unchanged.
			name:    "spec 4.4 sample",
			plain:   `it's the "same" entry (I think) [index, term] -> cmd; 100% sure?`,
			escaped: `it#39;s the #quot;same#quot; entry (I think) [index, term] -#gt; cmd; 100% sure?`,
		},
		{
			name:    "hash sign",
			plain:   `100# sure`,
			escaped: `100#35; sure`,
		},
		{
			name:    "angle brackets",
			plain:   `a < b > c`,
			escaped: `a #lt; b #gt; c`,
		},
		{
			name:    "hash is escaped before other escapes introduce hash",
			plain:   `#quot;`,
			escaped: `#35;quot;`,
		},
		{
			name:    "no special chars",
			plain:   `hello world`,
			escaped: `hello world`,
		},
		{
			name:    "empty string",
			plain:   ``,
			escaped: ``,
		},
		{
			name:    "all special chars",
			plain:   "#\"'<>",
			escaped: "#35;#quot;#39;#lt;#gt;",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Escape(tc.plain)
			if got != tc.escaped {
				t.Errorf("Escape(%q)\n  want %q\n   got %q", tc.plain, tc.escaped, got)
			}
			// Verify Unescape inverts Escape.
			back := Unescape(got)
			if back != tc.plain {
				t.Errorf("Unescape(Escape(%q))\n  want %q\n   got %q", tc.plain, tc.plain, back)
			}
		})
	}
}

// TestUnescape verifies decoding of every escape sequence and that #35; is
// decoded last.
func TestUnescape(t *testing.T) {
	cases := []struct {
		name    string
		escaped string
		plain   string
	}{
		{"quot", "#quot;", `"`},
		{"39", "#39;", `'`},
		{"lt", "#lt;", `<`},
		{"gt", "#gt;", `>`},
		{"35", "#35;", `#`},
		// #35; must decode after the others so a literal #35; in the escaped
		// input does not interfere with decoding #quot; etc.
		{"hash followed by sequence", "#35;quot;", `#quot;`},
		{"empty", "", ""},
		{"no escapes", "hello", "hello"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Unescape(tc.escaped)
			if got != tc.plain {
				t.Errorf("Unescape(%q) = %q, want %q", tc.escaped, got, tc.plain)
			}
		})
	}
}

// TestEscapeNewlineCollapse verifies that newlines in input are collapsed
// to spaces and that this loss is documented.
func TestEscapeNewlineCollapse(t *testing.T) {
	for _, nl := range []string{"\n", "\r", "\r\n"} {
		got := Escape("a" + nl + "b")
		if got != "a b" {
			t.Errorf("Escape with %q newline: got %q, want %q", nl, got, "a b")
		}
	}
}

// TestValidConceptID covers the ID rules in spec 4.3.
func TestValidConceptID(t *testing.T) {
	valid := []string{
		"foo", "log_matching", "commit_rules", "replicated_log", "leader_election",
		"abc123", "a", "z", "a_b_c",
	}
	for _, id := range valid {
		if !ValidConceptID(id) {
			t.Errorf("ValidConceptID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		// wrong pattern
		"", "A", "1foo", "_foo", "FOO", "foo-bar",
		// q/a-prefixed (reserved for questions/answers)
		"q1", "q99", "a1", "a0",
		// block IDs
		"passed", "untested", "testing",
		// Mermaid keywords
		"end", "graph", "flowchart", "subgraph", "class", "classDef", "click",
		"style", "default",
	}
	for _, id := range invalid {
		if ValidConceptID(id) {
			t.Errorf("ValidConceptID(%q) = true, want false", id)
		}
	}
}

// TestValidQuestionAnswerID covers q/a ID shapes.
func TestValidQuestionAnswerID(t *testing.T) {
	qIDs := []string{"q1", "q2", "q99", "q100"}
	aIDs := []string{"a1", "a2", "a99"}
	bad := []string{"", "q", "a", "q0x", "a0x", "Q1", "A1", "1q"}

	for _, id := range qIDs {
		if !ValidQuestionID(id) {
			t.Errorf("ValidQuestionID(%q) = false, want true", id)
		}
		if ValidAnswerID(id) {
			t.Errorf("ValidAnswerID(%q) = true for a q-ID, want false", id)
		}
	}
	for _, id := range aIDs {
		if !ValidAnswerID(id) {
			t.Errorf("ValidAnswerID(%q) = false, want true", id)
		}
		if ValidQuestionID(id) {
			t.Errorf("ValidQuestionID(%q) = true for an a-ID, want false", id)
		}
	}
	for _, id := range bad {
		if ValidQuestionID(id) {
			t.Errorf("ValidQuestionID(%q) = true, want false", id)
		}
		if ValidAnswerID(id) {
			t.Errorf("ValidAnswerID(%q) = true, want false", id)
		}
	}
}

// TestBatchID covers probe_N / teach_N shapes.
func TestBatchID(t *testing.T) {
	valid := []string{"probe_1", "probe_2", "probe_99", "teach_1", "teach_3"}
	for _, id := range valid {
		if !ValidBatchID(id) {
			t.Errorf("ValidBatchID(%q) = false, want true", id)
		}
	}

	invalid := []string{"", "probe_", "teach_", "probe_0x", "Probe_1", "batch_1"}
	for _, id := range invalid {
		if ValidBatchID(id) {
			t.Errorf("ValidBatchID(%q) = true, want false", id)
		}
	}
}

// TestBatchN covers the numeric suffix extraction.
func TestBatchN(t *testing.T) {
	cases := []struct {
		class string
		want  int
	}{
		{"probe_1", 1},
		{"probe_2", 2},
		{"probe_99", 99},
		{"teach_3", 3},
		{"teach_10", 10},
		{"invalid", 0},
	}
	for _, tc := range cases {
		got := BatchN(tc.class)
		if got != tc.want {
			t.Errorf("BatchN(%q) = %d, want %d", tc.class, got, tc.want)
		}
	}
}

// TestEdgePlacement verifies the section 4.2 edge placement rule.
// An edge from a passed concept to an untested concept must be emitted in
// the untested block (the "reopen-style" scenario).
func TestEdgePlacement(t *testing.T) {
	g := &Graph{
		PassedTitle:   "Concepts User understands",
		UntestedTitle: "Concepts User has not been tested on",
		TestingTitle:  "Open tests validating and teaching User understanding",
		PassedConcepts: []*ConceptNode{
			{ID: "alpha", Scope: "Alpha concept", Cites: []string{"x.txt:1-10"}, Block: BlockPassed},
		},
		UntestedConcepts: []*ConceptNode{
			{ID: "beta", Scope: "Beta concept", Cites: []string{"x.txt:11-20"}, Block: BlockUntested},
		},
		// Edge from passed concept to untested concept: home block is untested.
		Edges: []*Edge{
			{From: "alpha", To: "beta", Label: "leads to"},
		},
	}

	out := string(Write(g))

	wantEdge := `alpha --"leads to"--> beta`
	passedEnd := findBlockEnd(out, `subgraph passed`)
	untestedEnd := findBlockEnd(out, `subgraph untested`)

	if passedEnd < 0 || untestedEnd < 0 {
		t.Fatalf("could not locate block boundaries in writer output:\n%s", out)
	}

	passedSection := out[len(`flowchart TB`):passedEnd]
	untestedSection := out[passedEnd:untestedEnd]

	if strContains(passedSection, wantEdge) {
		t.Errorf("edge %q found in passed block; should be in untested", wantEdge)
	}
	if !strContains(untestedSection, wantEdge) {
		t.Errorf("edge %q not found in untested block\noutput:\n%s", wantEdge, out)
	}
}

// TestClassDefRegeneration verifies that classDef lines are regenerated from
// live batch classes, with fixed answer class lines always present.
func TestClassDefRegeneration(t *testing.T) {
	g := &Graph{
		PassedTitle:   "Concepts User understands",
		UntestedTitle: "Concepts User has not been tested on",
		TestingTitle:  "Open tests validating and teaching User understanding",
		PassedConcepts: []*ConceptNode{
			{ID: "alpha", Scope: "A", Cites: []string{"x.txt:1-2"}, Block: BlockPassed},
		},
		TestingItems: []TestingItem{
			{Q: &QuestionNode{ID: "q1", Scope: "Q1", Cite: "x.txt:1-2", Class: "probe_1"}},
			{A: &AnswerNode{ID: "a1", Label: "ans", Class: "pass"}},
			{Q: &QuestionNode{ID: "q2", Scope: "Q2", Cite: "x.txt:1-2", Class: "probe_2"}},
			{Q: &QuestionNode{ID: "q3", Scope: "Q3", Cite: "x.txt:1-2", Class: "teach_3"}},
			{A: &AnswerNode{ID: "a3", Label: "ans3", Class: "fail"}},
		},
	}

	out := string(Write(g))

	wantLines := []string{
		"    classDef probe_1,probe_2 stroke:#4aa3ff",
		"    classDef teach_3 stroke:#c9a227",
		"    classDef pass stroke:#3fb950",
		"    classDef fail stroke:#f85149",
		"    classDef unclear stroke:#d29922",
		"    classDef pending stroke-dasharray:4 3",
	}
	for _, want := range wantLines {
		if !strContains(out, want) {
			t.Errorf("missing classDef line %q in output:\n%s", want, out)
		}
	}
}

// findBlockEnd returns the position just after "    end\n" for the subgraph
// block opened by a line containing marker.
func findBlockEnd(s, marker string) int {
	start := strIndex(s, marker)
	if start < 0 {
		return -1
	}
	end := strIndex(s[start:], "    end\n")
	if end < 0 {
		return -1
	}
	return start + end + len("    end\n")
}

func strIndex(s, sub string) int {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func strContains(s, sub string) bool {
	return strIndex(s, sub) >= 0
}
