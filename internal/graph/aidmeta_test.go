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

// TestAidMeta_RoundTrip verifies that a single tm:aid line for each node type
// survives parse → checkFn → Write → re-parse unchanged.
func TestAidMeta_RoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		aidBlock string
		aidLine  string
		wantPath string
		// getAids returns the Aids slice for the node under test, fataling if the node is absent.
		getAids func(t *testing.T, g *Graph) []string
	}{
		{
			"passed concept",
			"passed",
			"%% tm:aid cp aids/cp-notes.txt",
			"aids/cp-notes.txt",
			func(t *testing.T, g *Graph) []string {
				t.Helper()
				if len(g.PassedConcepts) == 0 {
					t.Fatal("no passed concepts")
				}
				return g.PassedConcepts[0].Aids
			},
		},
		{
			"untested concept",
			"untested",
			"%% tm:aid cu aids/cu.md",
			"aids/cu.md",
			func(t *testing.T, g *Graph) []string {
				t.Helper()
				if len(g.UntestedConcepts) == 0 {
					t.Fatal("no untested concepts")
				}
				return g.UntestedConcepts[0].Aids
			},
		},
		{
			"reserve concept",
			"reserve",
			"%% tm:aid cr aids/cr.txt",
			"aids/cr.txt",
			func(t *testing.T, g *Graph) []string {
				t.Helper()
				if len(g.ReserveConcepts) == 0 {
					t.Fatal("no reserve concepts")
				}
				return g.ReserveConcepts[0].Aids
			},
		},
		{
			"question",
			"testing",
			"%% tm:aid q1 aids/q1.txt",
			"aids/q1.txt",
			func(t *testing.T, g *Graph) []string {
				t.Helper()
				for _, item := range g.TestingItems {
					if item.Q != nil {
						return item.Q.Aids
					}
				}
				t.Fatal("no question in testing block")
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := aidGraphSrc(tc.aidBlock, tc.aidLine)
			parseAndCheckAids(t, src, func(t *testing.T, g *Graph) {
				t.Helper()
				aids := tc.getAids(t, g)
				if len(aids) != 1 || aids[0] != tc.wantPath {
					t.Errorf("Aids: want [%s], got %v", tc.wantPath, aids)
				}
			})
		})
	}
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
