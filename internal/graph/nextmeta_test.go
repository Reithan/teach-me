package graph

import (
	"bytes"
	"strings"
	"testing"
)

// minimalGraphWithNextMeta builds a four-block Mermaid graph string with a
// %% tm:next line in the untested block. The indent must match what Write emits.
func minimalGraphWithNextMeta(q, batch int) string {
	var b strings.Builder
	b.WriteString("flowchart TB\n")
	b.WriteString("    subgraph passed[\"P\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    subgraph untested[\"U\"]\n")
	b.WriteString("        %% tm:next q=")
	writeInt(&b, q)
	b.WriteString(" batch=")
	writeInt(&b, batch)
	b.WriteString("\n")
	b.WriteString("    end\n")
	b.WriteString("    subgraph reserve[\"R\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    subgraph testing[\"T\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    classDef pass stroke:#3fb950\n")
	b.WriteString("    classDef fail stroke:#f85149\n")
	b.WriteString("    classDef unclear stroke:#d29922\n")
	b.WriteString("    classDef pending stroke-dasharray:4 3\n")
	return b.String()
}

// ── Parse / write round-trip ─────────────────────────────────────────────────

func TestNextMeta_ParseRoundTrip(t *testing.T) {
	cases := []struct{ q, batch int }{
		{1, 1},
		{5, 3},
		{100, 42},
	}
	for _, tc := range cases {
		src := minimalGraphWithNextMeta(tc.q, tc.batch)
		g, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse q=%d batch=%d: %v", tc.q, tc.batch, err)
		}
		if g.NextMeta == nil {
			t.Fatalf("NextMeta is nil after parsing q=%d batch=%d", tc.q, tc.batch)
		}
		if g.NextMeta.Q != tc.q {
			t.Errorf("NextMeta.Q: want %d, got %d", tc.q, g.NextMeta.Q)
		}
		if g.NextMeta.Batch != tc.batch {
			t.Errorf("NextMeta.Batch: want %d, got %d", tc.batch, g.NextMeta.Batch)
		}

		// Write and re-parse: must be identical.
		got := Write(g)
		if !bytes.Equal(got, []byte(src)) {
			t.Errorf("Write output differs from source\nwant: %q\n got: %q", src, got)
		}
	}
}

func TestNextMeta_AbsentInFileRemainsNil(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	g, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if g.NextMeta != nil {
		t.Errorf("NextMeta should be nil for a file without the line; got %+v", g.NextMeta)
	}
	// Write must not insert the line.
	out := Write(g)
	if strings.Contains(string(out), "tm:next") {
		t.Errorf("Write must not emit tm:next when NextMeta is nil; got:\n%s", out)
	}
}

// ── Parse errors ─────────────────────────────────────────────────────────────

func parseNextMetaError(t *testing.T, data []byte) error {
	t.Helper()
	_, err := Parse(data)
	return err
}

func TestNextMeta_ParseRejectsDuplicate(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=2 batch=2
        %% tm:next q=3 batch=3
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for duplicate tm:next; got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected 'duplicate' in error; got: %v", err)
	}
}

func TestNextMeta_ParseRejectsOutsideUntested_Passed(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
        %% tm:next q=2 batch=2
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:next in passed block; got nil")
	}
	if !strings.Contains(err.Error(), "outside untested") {
		t.Errorf("expected 'outside untested' in error; got: %v", err)
	}
}

func TestNextMeta_ParseRejectsOutsideUntested_Testing(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
        %% tm:next q=2 batch=2
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:next in testing block; got nil")
	}
	if !strings.Contains(err.Error(), "outside untested") {
		t.Errorf("expected 'outside untested' in error; got: %v", err)
	}
}

func TestNextMeta_ParseRejectsMalformed_NoQ(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next batch=2
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:next with no q= field; got nil")
	}
}

func TestNextMeta_ParseRejectsMalformed_ZeroValue(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=0 batch=2
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:next q=0; got nil")
	}
	if !strings.Contains(err.Error(), ">= 1") {
		t.Errorf("expected '>= 1' in error; got: %v", err)
	}
}

func TestNextMeta_ParseRejectsMalformed_NonDigit(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=abc batch=2
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	err := parseNextMetaError(t, []byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:next q=abc; got nil")
	}
}

// ── Writer ordering ──────────────────────────────────────────────────────────

func TestNextMeta_WrittenBeforeGateLines(t *testing.T) {
	// NextMeta must appear before any %% tm:gate lines.
	g := &Graph{
		PassedTitle:   "P",
		UntestedTitle: "U",
		ReserveTitle:  "R",
		TestingTitle:  "T",
		NextMeta:      &NextMeta{Q: 5, Batch: 3},
		UntestedMetas: []GateMeta{{Concept: "c1", Base: 2}},
	}
	out := string(Write(g))
	nextPos := strings.Index(out, "tm:next")
	gatePos := strings.Index(out, "tm:gate")
	if nextPos < 0 {
		t.Fatal("tm:next not found in output")
	}
	if gatePos < 0 {
		t.Fatal("tm:gate not found in output")
	}
	if nextPos > gatePos {
		t.Errorf("tm:next appears after tm:gate; want it before\noutput:\n%s", out)
	}
}

// ── %% tm:format parse / write round-trip ────────────────────────────────────

// minimalGraphWithFormatMeta builds a graph string with %% tm:format in the
// untested block. If also has %% tm:next when nextQ > 0.
func minimalGraphWithFormatMeta(n, nextQ, nextBatch int) string {
	var b strings.Builder
	b.WriteString("flowchart TB\n")
	b.WriteString("    subgraph passed[\"P\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    subgraph untested[\"U\"]\n")
	b.WriteString("        %% tm:format ")
	writeInt(&b, n)
	b.WriteString("\n")
	if nextQ > 0 {
		b.WriteString("        %% tm:next q=")
		writeInt(&b, nextQ)
		b.WriteString(" batch=")
		writeInt(&b, nextBatch)
		b.WriteString("\n")
	}
	b.WriteString("    end\n")
	b.WriteString("    subgraph reserve[\"R\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    subgraph testing[\"T\"]\n")
	b.WriteString("    end\n")
	b.WriteString("    classDef pass stroke:#3fb950\n")
	b.WriteString("    classDef fail stroke:#f85149\n")
	b.WriteString("    classDef unclear stroke:#d29922\n")
	b.WriteString("    classDef pending stroke-dasharray:4 3\n")
	return b.String()
}

func TestFormatMeta_ParseRoundTrip(t *testing.T) {
	cases := []struct {
		n, nextQ, nextBatch int
	}{
		{1, 0, 0},
		{2, 0, 0},
		{2, 5, 3},
	}
	for _, tc := range cases {
		src := minimalGraphWithFormatMeta(tc.n, tc.nextQ, tc.nextBatch)
		g, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse n=%d nextQ=%d: %v", tc.n, tc.nextQ, err)
		}
		if g.Format == nil {
			t.Fatalf("Format is nil after parsing n=%d", tc.n)
		}
		if g.Format.N != tc.n {
			t.Errorf("Format.N: want %d, got %d", tc.n, g.Format.N)
		}

		// Write and re-parse: must be identical.
		got := Write(g)
		if !bytes.Equal(got, []byte(src)) {
			t.Errorf("Write output differs from source\nwant: %q\n got: %q", src, got)
		}
	}
}

func TestFormatMeta_AbsentMeansFormatOne(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
	g, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if g.Format != nil {
		t.Errorf("Format should be nil for a file without the line; got %+v", g.Format)
	}
	if g.FormatN() != 1 {
		t.Errorf("FormatN() should return 1 when absent; got %d", g.FormatN())
	}
	// Write must not insert the line.
	out := Write(g)
	if strings.Contains(string(out), "tm:format") {
		t.Errorf("Write must not emit tm:format when Format is nil; got:\n%s", out)
	}
}

func TestFormatMeta_FormatNReturnsN(t *testing.T) {
	g := &Graph{Format: &FormatMeta{N: 2}}
	if got := g.FormatN(); got != 2 {
		t.Errorf("FormatN: want 2, got %d", got)
	}
}

// ── tm:format parse errors ────────────────────────────────────────────────────

func TestFormatMeta_ParseRejectsDuplicate(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:format 1
        %% tm:format 2
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected parse error for duplicate tm:format; got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected 'duplicate' in error; got: %v", err)
	}
}

func TestFormatMeta_ParseRejectsAfterNextMeta(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:next q=2 batch=2
        %% tm:format 2
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:format after tm:next; got nil")
	}
	if !strings.Contains(err.Error(), "first meta") {
		t.Errorf("expected 'first meta' in error; got: %v", err)
	}
}

func TestFormatMeta_ParseRejectsOutsideUntested(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
        %% tm:format 2
    end
    subgraph untested["U"]
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:format outside untested; got nil")
	}
	if !strings.Contains(err.Error(), "outside untested") {
		t.Errorf("expected 'outside untested' in error; got: %v", err)
	}
}

func TestFormatMeta_ParseRejectsMalformed_ZeroValue(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:format 0
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:format 0; got nil")
	}
	if !strings.Contains(err.Error(), ">= 1") {
		t.Errorf("expected '>= 1' in error; got: %v", err)
	}
}

func TestFormatMeta_ParseRejectsMalformed_NonDigit(t *testing.T) {
	src := `flowchart TB
    subgraph passed["P"]
    end
    subgraph untested["U"]
        %% tm:format abc
    end
    subgraph reserve["R"]
    end
    subgraph testing["T"]
    end
`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected parse error for tm:format abc; got nil")
	}
}

// ── Writer ordering ───────────────────────────────────────────────────────────

func TestFormatMeta_WrittenBeforeNextMetaAndGateLines(t *testing.T) {
	g := &Graph{
		PassedTitle:   "P",
		UntestedTitle: "U",
		ReserveTitle:  "R",
		TestingTitle:  "T",
		Format:        &FormatMeta{N: 2},
		NextMeta:      &NextMeta{Q: 5, Batch: 3},
		UntestedMetas: []GateMeta{{Concept: "c1", Base: 2}},
	}
	out := string(Write(g))
	fmtPos := strings.Index(out, "tm:format")
	nextPos := strings.Index(out, "tm:next")
	gatePos := strings.Index(out, "tm:gate")
	if fmtPos < 0 {
		t.Fatal("tm:format not found in output")
	}
	if nextPos < 0 {
		t.Fatal("tm:next not found in output")
	}
	if gatePos < 0 {
		t.Fatal("tm:gate not found in output")
	}
	if fmtPos > nextPos {
		t.Errorf("tm:format appears after tm:next; want it before\noutput:\n%s", out)
	}
	if fmtPos > gatePos {
		t.Errorf("tm:format appears after tm:gate; want it before\noutput:\n%s", out)
	}
}
