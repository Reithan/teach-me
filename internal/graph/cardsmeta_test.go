package graph

import (
	"strings"
	"testing"
)

// conceptByID returns the concept with the given ID from any block, or nil.
func conceptByID(g *Graph, id string) *ConceptNode {
	for _, list := range [][]*ConceptNode{g.PassedConcepts, g.UntestedConcepts, g.ReserveConcepts} {
		for _, c := range list {
			if c.ID == id {
				return c
			}
		}
	}
	return nil
}

func TestCardsMeta_RoundTripInEveryConceptBlock(t *testing.T) {
	for block, id := range map[string]string{"passed": "cp", "untested": "cu", "reserve": "cr"} {
		src := aidGraphSrc(block, "%% tm:cards "+id)
		g, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("%s: Parse: %v", block, err)
		}
		for _, other := range []string{"cp", "cu", "cr"} {
			if got := conceptByID(g, other).Cards; got != (other == id) {
				t.Errorf("%s: concept %s Cards = %v", block, other, got)
			}
		}
		out := string(Write(g))
		if n := strings.Count(out, "%% tm:cards "+id+"\n"); n != 1 {
			t.Errorf("%s: want one tm:cards line in output, got %d:\n%s", block, n, out)
		}
		g2, err := Parse([]byte(out))
		if err != nil {
			t.Fatalf("%s: re-Parse: %v", block, err)
		}
		if !conceptByID(g2, id).Cards {
			t.Errorf("%s: Cards lost after a write and re-parse", block)
		}
	}
}

func TestCardsMeta_WrittenAfterAidLinesSortedByID(t *testing.T) {
	src := aidGraphSrc("untested", "%% tm:cards cu", "%% tm:aid cu aids/x.png")
	g, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := string(Write(g))
	aid := strings.Index(out, "%% tm:aid cu aids/x.png")
	cards := strings.Index(out, "%% tm:cards cu")
	if aid < 0 || cards < 0 || cards < aid {
		t.Errorf("want the cards line after the aid line:\n%s", out)
	}
}

func TestCardsMeta_Refusals(t *testing.T) {
	cases := []struct {
		name, block string
		lines       []string
		wantErr     string
	}{
		{"unknown concept", "untested", []string{"%% tm:cards nope"}, "tm:cards for unknown concept nope"},
		{"wrong block", "passed", []string{"%% tm:cards cu"}, "tm:cards cu outside its block"},
		{"duplicate", "untested", []string{"%% tm:cards cu", "%% tm:cards cu"}, "duplicate tm:cards cu"},
		{"question id", "testing", []string{"%% tm:cards q1"}, "invalid tm:cards line"},
		{"two ids", "untested", []string{"%% tm:cards cu cp"}, "invalid tm:cards line"},
	}
	for _, tc := range cases {
		_, err := Parse([]byte(aidGraphSrc(tc.block, tc.lines...)))
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want error containing %q, got %v", tc.name, tc.wantErr, err)
		}
	}
}
