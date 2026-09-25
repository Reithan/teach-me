package graph

import (
	"strings"
	"testing"
)

// aidGraphSrc builds a minimal 4-block graph with one concept per block,
// one question, and the supplied aid lines injected into the named block.
// aidBlock is "passed", "untested", "reserve", or "testing"; aidLines are raw
// "%% tm:aid <id> <path>" strings (without indentation — the helper adds them).
func aidGraphSrc(aidBlock string, aidLines ...string) string {
	aidStr := func(block string) string {
		if block != aidBlock {
			return ""
		}
		var sb strings.Builder
		for _, l := range aidLines {
			sb.WriteString("        ")
			sb.WriteString(l)
			sb.WriteString("\n")
		}
		return sb.String()
	}

	return "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"        cp[\"passed concept<br/>abc@f.txt:1-2\"]\n" +
		aidStr("passed") +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"        %% tm:format 2\n" +
		"        cu[\"untested concept<br/>abc@f.txt:1-2\"]\n" +
		aidStr("untested") +
		"    end\n" +
		"    subgraph reserve[\"R\"]\n" +
		"        cr[\"reserve concept<br/>abc@f.txt:1-2\"]\n" +
		aidStr("reserve") +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"        q1[\"q scope<br/>abc@f.txt:1-2\"]:::probe_1\n" +
		aidStr("testing") +
		"    end\n" +
		"    classDef probe_1 stroke:#4aa3ff\n" +
		"    classDef pass stroke:#3fb950\n" +
		"    classDef fail stroke:#f85149\n" +
		"    classDef unclear stroke:#d29922\n" +
		"    classDef pending stroke-dasharray:4 3\n"
}

// parseAndCheckAids parses src, applies checkFn, then writes and re-parses to
// verify the aid survives a roundtrip through Write.
func parseAndCheckAids(t *testing.T, src string, checkFn func(t *testing.T, g *Graph)) {
	t.Helper()
	g, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	checkFn(t, g)

	// Roundtrip.
	written := Write(g)
	g2, err := Parse(written)
	if err != nil {
		t.Fatalf("Parse after Write: %v", err)
	}
	checkFn(t, g2)
}

// ── Parse / write round-trip ──────────────────────────────────────────────────

func TestAidMeta_PassedConceptRoundTrip(t *testing.T) {
	src := aidGraphSrc("passed", "%% tm:aid cp aids/cp-notes.txt")
	parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
		t.Helper()
		if len(g.PassedConcepts) == 0 {
			t.Fatal("no passed concepts")
		}
		cn := g.PassedConcepts[0]
		if len(cn.Aids) != 1 || cn.Aids[0] != "aids/cp-notes.txt" {
			t.Errorf("PassedConcept.Aids: want [aids/cp-notes.txt], got %v", cn.Aids)
		}
	})
}

func TestAidMeta_UntestedConceptRoundTrip(t *testing.T) {
	src := aidGraphSrc("untested", "%% tm:aid cu aids/cu.md")
	parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
		t.Helper()
		if len(g.UntestedConcepts) == 0 {
			t.Fatal("no untested concepts")
		}
		cn := g.UntestedConcepts[0]
		if len(cn.Aids) != 1 || cn.Aids[0] != "aids/cu.md" {
			t.Errorf("UntestedConcept.Aids: want [aids/cu.md], got %v", cn.Aids)
		}
	})
}

func TestAidMeta_ReserveConceptRoundTrip(t *testing.T) {
	src := aidGraphSrc("reserve", "%% tm:aid cr aids/cr.txt")
	parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
		t.Helper()
		if len(g.ReserveConcepts) == 0 {
			t.Fatal("no reserve concepts")
		}
		cn := g.ReserveConcepts[0]
		if len(cn.Aids) != 1 || cn.Aids[0] != "aids/cr.txt" {
			t.Errorf("ReserveConcept.Aids: want [aids/cr.txt], got %v", cn.Aids)
		}
	})
}

func TestAidMeta_QuestionRoundTrip(t *testing.T) {
	src := aidGraphSrc("testing", "%% tm:aid q1 aids/q1.txt")
	parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
		t.Helper()
		var q *QuestionNode
		for _, item := range g.TestingItems {
			if item.Q != nil {
				q = item.Q
				break
			}
		}
		if q == nil {
			t.Fatal("no question in testing block")
		}
		if len(q.Aids) != 1 || q.Aids[0] != "aids/q1.txt" {
			t.Errorf("Question.Aids: want [aids/q1.txt], got %v", q.Aids)
		}
	})
}

func TestAidMeta_MultipleAidsPreservesOrder(t *testing.T) {
	src := aidGraphSrc("untested",
		"%% tm:aid cu aids/first.md",
		"%% tm:aid cu aids/second.txt",
	)
	parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
		t.Helper()
		if len(g.UntestedConcepts) == 0 {
			t.Fatal("no untested concepts")
		}
		cn := g.UntestedConcepts[0]
		if len(cn.Aids) != 2 {
			t.Fatalf("want 2 aids, got %v", cn.Aids)
		}
		if cn.Aids[0] != "aids/first.md" || cn.Aids[1] != "aids/second.txt" {
			t.Errorf("wrong aid order: %v", cn.Aids)
		}
	})
}

// ── Parse errors ──────────────────────────────────────────────────────────────

func mustFailParse(t *testing.T, src string, wantSubstr string) {
	t.Helper()
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatalf("expected parse error containing %q; got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("want %q in error; got: %v", wantSubstr, err)
	}
}

func TestAidMeta_ErrorUnknownID(t *testing.T) {
	src := "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"        %% tm:aid nosuchid aids/x.txt\n" +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"    end\n" +
		"    subgraph reserve[\"R\"]\n" +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"    end\n"
	mustFailParse(t, src, "unknown id nosuchid")
}

func TestAidMeta_ErrorOutsideBlock(t *testing.T) {
	// cu is in untested but the aid line is in passed.
	src := "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"        %% tm:aid cu aids/x.txt\n" +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"        cu[\"my concept<br/>abc@f.txt:1-2\"]\n" +
		"    end\n" +
		"    subgraph reserve[\"R\"]\n" +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"    end\n"
	mustFailParse(t, src, "outside its block")
}

func TestAidMeta_ErrorDuplicate(t *testing.T) {
	src := "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"        cu[\"my concept<br/>abc@f.txt:1-2\"]\n" +
		"        %% tm:aid cu aids/x.txt\n" +
		"        %% tm:aid cu aids/x.txt\n" +
		"    end\n" +
		"    subgraph reserve[\"R\"]\n" +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"    end\n"
	mustFailParse(t, src, "duplicate tm:aid")
}

func TestAidMeta_ErrorAnswerIDRejected(t *testing.T) {
	src := "flowchart TB\n" +
		"    subgraph passed[\"P\"]\n" +
		"    end\n" +
		"    subgraph untested[\"U\"]\n" +
		"    end\n" +
		"    subgraph reserve[\"R\"]\n" +
		"    end\n" +
		"    subgraph testing[\"T\"]\n" +
		"        a1[\"answer text\"]:::pass\n" +
		"        %% tm:aid a1 aids/x.txt\n" +
		"    end\n"
	mustFailParse(t, src, "unknown id a1")
}
