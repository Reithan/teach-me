package graph

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestGoldenRoundTrip parses each golden .mmd file under testdata/ and verifies
// that Write produces byte-identical output (canonical round-trip).
func TestGoldenRoundTrip(t *testing.T) {
	goldens := []string{
		"../../testdata/raft.mmd",
		"../../testdata/gated.mmd",
	}
	for _, path := range goldens {
		t.Run(path, func(t *testing.T) {
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
		})
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

// TestQuestionN covers numeric suffix extraction for both q- and a-prefixed IDs.
func TestQuestionN(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"q1", 1},
		{"q5", 5},
		{"q99", 99},
		{"a1", 1},
		{"a42", 42},
		// degenerate inputs
		{"", 0},
		{"q", 0},
		{"a", 0},
	}
	for _, tc := range cases {
		got := QuestionN(tc.s)
		if got != tc.want {
			t.Errorf("QuestionN(%q) = %d, want %d", tc.s, got, tc.want)
		}
	}
}

// TestIsAnswerClass covers all four valid classes and common invalid values.
func TestIsAnswerClass(t *testing.T) {
	valid := []string{"pending", "pass", "fail", "unclear"}
	for _, c := range valid {
		if !IsAnswerClass(c) {
			t.Errorf("IsAnswerClass(%q) = false, want true", c)
		}
	}

	invalid := []string{"", "probe_1", "teach_1", "PASS", "Pending", "oos", "unknown"}
	for _, c := range invalid {
		if IsAnswerClass(c) {
			t.Errorf("IsAnswerClass(%q) = true, want false", c)
		}
	}
}

// TestEdgeHomeBlockCases covers the corner cases of the 4.2 rule:
// both endpoints unknown, and only one endpoint known.
func TestEdgeHomeBlockCases(t *testing.T) {
	blocks := map[string]Block{
		"alpha": BlockPassed,
		"beta":  BlockUntested,
		"gamma": BlockTesting,
	}

	cases := []struct {
		name string
		from string
		to   string
		want Block
	}{
		// both known: later block wins
		{"passed->passed", "alpha", "alpha", BlockPassed},
		{"passed->untested", "alpha", "beta", BlockUntested},
		{"passed->testing", "alpha", "gamma", BlockTesting},
		{"untested->testing", "beta", "gamma", BlockTesting},
		// only From known
		{"from-known only (passed)", "alpha", "unknown", BlockPassed},
		{"from-known only (untested)", "beta", "unknown", BlockUntested},
		// only To known
		{"to-known only (passed)", "unknown", "alpha", BlockPassed},
		{"to-known only (testing)", "unknown", "gamma", BlockTesting},
		// both unknown: defaults to BlockPassed
		{"both unknown", "unknown1", "unknown2", BlockPassed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Edge{From: tc.from, To: tc.to}
			got := edgeHomeBlock(e, blocks)
			if got != tc.want {
				t.Errorf("edgeHomeBlock(%q->%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// TestWriteInt covers both the n==0 fast path and the digit-extraction loop.
func TestWriteInt(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{100, "100"},
		{3, "3"},
		{12345, "12345"},
		{1000000, "1000000"},
	}
	for _, tc := range cases {
		var b strings.Builder
		writeInt(&b, tc.n)
		if got := b.String(); got != tc.want {
			t.Errorf("writeInt(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestWriteGateMeta verifies the exact output format of writeGateMeta.
func TestWriteGateMeta(t *testing.T) {
	cases := []struct {
		m    GateMeta
		want string
	}{
		{
			m:    GateMeta{Concept: "foo", Base: 3},
			want: "        %% tm:gate foo base=3\n",
		},
		{
			m:    GateMeta{Concept: "bar", Base: 0},
			want: "        %% tm:gate bar base=0\n",
		},
		{
			m:    GateMeta{Concept: "consensus", Base: 10},
			want: "        %% tm:gate consensus base=10\n",
		},
	}
	for _, tc := range cases {
		var b strings.Builder
		writeGateMeta(&b, tc.m)
		if got := b.String(); got != tc.want {
			t.Errorf("writeGateMeta(%+v)\n  got  %q\n  want %q", tc.m, got, tc.want)
		}
	}
}

// TestWriteLeadingComments verifies that each comment is emitted at indent2.
func TestWriteLeadingComments(t *testing.T) {
	cases := []struct {
		comments []string
		want     string
	}{
		{nil, ""},
		{[]string{}, ""},
		{
			[]string{"%% first"},
			"        %% first\n",
		},
		{
			[]string{"%% first", "%% second"},
			"        %% first\n        %% second\n",
		},
	}
	for _, tc := range cases {
		var b strings.Builder
		writeLeadingComments(&b, tc.comments)
		if got := b.String(); got != tc.want {
			t.Errorf("writeLeadingComments(%v)\n  got  %q\n  want %q", tc.comments, got, tc.want)
		}
	}
}

// TestParseGateMetaDirect calls parseGateMeta directly to cover error paths.
func TestParseGateMetaDirect(t *testing.T) {
	// success case
	m, err := parseGateMeta("%% tm:gate consensus base=3", nil)
	if err != nil {
		t.Fatalf("parseGateMeta: unexpected error: %v", err)
	}
	if m.Concept != "consensus" || m.Base != 3 {
		t.Errorf("parseGateMeta: got %+v, want concept=consensus base=3", m)
	}

	// wrong number of fields (only one field after prefix)
	_, err = parseGateMeta("%% tm:gate foo", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid tm:gate line") {
		t.Errorf("parseGateMeta single field: want 'invalid tm:gate line' error, got %v", err)
	}

	// missing base= prefix (two fields but second doesn't start with base=)
	_, err = parseGateMeta("%% tm:gate foo notbase=1", nil)
	if err == nil || !strings.Contains(err.Error(), "missing base=") {
		t.Errorf("parseGateMeta missing base=: want 'missing base=' error, got %v", err)
	}

	// invalid base value (non-digit)
	_, err = parseGateMeta("%% tm:gate foo base=abc", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid base value") {
		t.Errorf("parseGateMeta bad base: want 'invalid base value' error, got %v", err)
	}

	// empty base value
	_, err = parseGateMeta("%% tm:gate foo base=", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid base value") {
		t.Errorf("parseGateMeta empty base: want 'invalid base value' error, got %v", err)
	}
}

// minimalGraph returns a minimal well-formed graph string using the three
// empty blocks. The caller replaces individual block content via fmt.Sprintf
// or string replacement.
func minimalGraphWith(passed, untested, testing string) string {
	return "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" + passed + "    end\n" +
		"    subgraph untested[\"U\"]\n" + untested + "    end\n" +
		"    subgraph testing[\"T\"]\n" + testing + "    end\n"
}

// TestParseErrors covers error paths in Parse / parseBlock / parseSubgraphHeader.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // expected substring in the error message
	}{
		{
			name:  "empty file",
			input: "",
			want:  "expected \"flowchart TB\"",
		},
		{
			name:  "wrong flowchart direction",
			input: "flowchart LR\n",
			want:  "expected \"flowchart TB\"",
		},
		{
			name:  "EOF before passed block",
			input: "flowchart TB\n",
			want:  "expected subgraph passed",
		},
		{
			name:  "non-subgraph line where subgraph expected",
			input: "flowchart TB\nfoo[\"bar\"]\n",
			want:  "expected subgraph passed",
		},
		{
			name:  "invalid subgraph header (no bracket title)",
			input: "flowchart TB\n    subgraph passed\n    end\n",
			want:  "invalid subgraph header",
		},
		{
			name:  "wrong block order (untested before passed)",
			input: "flowchart TB\n    subgraph untested[\"U\"]\n    end\n    subgraph passed[\"P\"]\n    end\n    subgraph testing[\"T\"]\n    end\n",
			want:  "expected block \"passed\"",
		},
		{
			name:  "unclosed subgraph (missing end)",
			input: "flowchart TB\n    subgraph passed[\"P\"]\n        foo[\"Foo<br/>x.txt:1\"]\n",
			want:  "unclosed subgraph passed",
		},
		{
			name:  "tm:gate meta in passed block",
			input: minimalGraphWith("        %% tm:gate foo base=1\n", "", ""),
			want:  "tm:gate meta outside untested block",
		},
		{
			name:  "tm:gate meta in testing block",
			input: minimalGraphWith("", "", "        %% tm:gate foo base=1\n"),
			want:  "tm:gate meta outside untested block",
		},
		{
			name:  "invalid tm:gate line (one field only)",
			input: minimalGraphWith("", "        %% tm:gate foo\n", ""),
			want:  "invalid tm:gate line",
		},
		{
			name:  "invalid tm:gate line (missing base= key)",
			input: minimalGraphWith("", "        %% tm:gate foo notbase=1\n", ""),
			want:  "missing base=",
		},
		{
			name:  "invalid tm:gate base value (non-digit)",
			input: minimalGraphWith("", "        %% tm:gate foo base=abc\n", ""),
			want:  "invalid base value",
		},
		{
			name:  "unrecognized line in block (no brackets, no arrow)",
			input: minimalGraphWith("        notanode\n", "", ""),
			want:  "unrecognized line in block passed",
		},
		{
			name:  "unexpected line after all blocks",
			input: minimalGraphWith("", "", "") + "junk line\n",
			want:  "unexpected line after blocks",
		},
		{
			name:  "unclosed frontmatter",
			input: "---\nconfig:\n  key: value\n",
			want:  "unclosed frontmatter",
		},
		{
			name:  "question declared outside testing block",
			input: minimalGraphWith("        q1[\"Q<br/>x.txt:1\"]:::probe_1\n", "", ""),
			want:  "question",
		},
		{
			name:  "concept declared in testing block",
			input: minimalGraphWith("", "", "        foo[\"Foo<br/>x.txt:1\"]\n"),
			want:  "concept",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestParseBlankLineInsideBlock verifies that blank lines within a subgraph
// body are silently skipped (they do not survive round-trip, which is correct).
func TestParseBlankLineInsideBlock(t *testing.T) {
	input := "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"        foo[\"Foo<br/>x.txt:1-5\"]\n" +
		"\n" +
		"        bar[\"Bar<br/>x.txt:6-10\"]\n" +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"    end\n"
	g, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(g.PassedConcepts) != 2 {
		t.Errorf("want 2 concepts, got %d", len(g.PassedConcepts))
	}
	if g.PassedConcepts[0].ID != "foo" || g.PassedConcepts[1].ID != "bar" {
		t.Errorf("unexpected concept IDs: %v, %v", g.PassedConcepts[0].ID, g.PassedConcepts[1].ID)
	}
}

// TestMultiCiteRoundTrip verifies that multiple citations on a concept node are
// written as a single comma-separated field (§4.4) and parsed back correctly.
func TestMultiCiteRoundTrip(t *testing.T) {
	cites := []string{"a.txt:1-10", "b.txt:20-30", "c.txt:5-15"}
	cn := &ConceptNode{
		ID:    "alpha",
		Scope: "Alpha concept",
		Cites: cites,
		Block: BlockUntested,
	}
	label := conceptLabel(cn)
	// The cites must appear as exactly one field (no bare "<br/>" between individual cites).
	parts := strings.Split(label, "<br/>")
	if len(parts) != 2 {
		t.Fatalf("want 2 label fields (scope + cites), got %d: %v", len(parts), parts)
	}
	// Round-trip: parse produces the same Cites slice.
	got := parseConceptNode("alpha", BlockUntested, label, "", nil)
	if len(got.Cites) != len(cites) {
		t.Fatalf("cites after round-trip: want %v, got %v", cites, got.Cites)
	}
	for i, c := range cites {
		if got.Cites[i] != c {
			t.Errorf("cites[%d]: want %q, got %q", i, c, got.Cites[i])
		}
	}
}

// TestAnswerNodeAskedRoundTrip verifies that AnswerNode.Asked survives a
// write→parse round-trip, including wordings that contain characters that
// require escaping (quotes, <br/>, $).
func TestAnswerNodeAskedRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		asked string
		label string
		oos   bool
	}{
		{
			name:  "asked only",
			asked: "how does X work?",
			label: "the answer body",
		},
		{
			name:  "asked with special chars",
			asked: `"quoted" $var <br/> text`,
			label: "body text",
		},
		{
			name:  "oos and asked",
			asked: "what's the key idea?",
			label: "answer with 'quotes' and $signs",
			oos:   true,
		},
		{
			name:  "no asked",
			asked: "",
			label: "plain body",
		},
		{
			// Body is exactly "OOS" with no prefix flag set; the parser must not
			// treat it as the OOS field (trailing-part guard).
			name:  "body looks like OOS prefix",
			asked: "",
			label: "OOS",
			oos:   false,
		},
		{
			// Body begins with "ASKED: " but Asked is empty; the parser must not
			// treat it as the ASKED field (trailing-part guard).
			name:  "body looks like ASKED prefix",
			asked: "",
			label: "ASKED: this is the answer body",
			oos:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := &Graph{
				PassedTitle:   "P",
				UntestedTitle: "U",
				TestingTitle:  "T",
				TestingItems: []TestingItem{
					{
						A: &AnswerNode{
							ID:    "a1",
							OOS:   tc.oos,
							Asked: tc.asked,
							Label: tc.label,
							Class: "pending",
						},
					},
				},
			}

			// Write and parse back.
			data := Write(g)
			g2, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			var got *AnswerNode
			for _, item := range g2.TestingItems {
				if item.A != nil && item.A.ID == "a1" {
					got = item.A
					break
				}
			}
			if got == nil {
				t.Fatal("a1 not found after round-trip")
			}
			if got.Asked != tc.asked {
				t.Errorf("Asked: want %q, got %q", tc.asked, got.Asked)
			}
			if got.Label != tc.label {
				t.Errorf("Label: want %q, got %q", tc.label, got.Label)
			}
			if got.OOS != tc.oos {
				t.Errorf("OOS: want %v, got %v", tc.oos, got.OOS)
			}
		})
	}
}
