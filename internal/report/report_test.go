package report_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/report"
	"github.com/reithan/teach-me/internal/state"
)

// buildGraph creates a minimal Graph with the given passed and untested concept
// IDs and edges. Edges are [from, to] pairs.
func buildGraph(passed, untested []string, edges [][2]string) *graph.Graph {
	g := &graph.Graph{}
	for _, id := range passed {
		g.PassedConcepts = append(g.PassedConcepts, &graph.ConceptNode{
			ID: id, Scope: id + " scope", Block: graph.BlockPassed,
		})
	}
	for _, id := range untested {
		g.UntestedConcepts = append(g.UntestedConcepts, &graph.ConceptNode{
			ID: id, Scope: id + " scope", Block: graph.BlockUntested,
		})
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, &graph.Edge{From: e[0], To: e[1], Label: "req"})
	}
	return g
}

func ids(infos []report.ConceptInfo) []string {
	out := make([]string, len(infos))
	for i, ci := range infos {
		out[i] = ci.Node.ID
	}
	return out
}

func TestWalk(t *testing.T) {
	tests := []struct {
		name     string
		passed   []string
		untested []string
		edges    [][2]string
		start    string   // used when allMode=false
		hops     int      // hops for Walk; depth for WalkAll
		allMode  bool     // use WalkAll instead of Walk
		want     []string // nil means expect error
	}{
		{
			name:     "linear topo order",
			untested: []string{"a", "b", "c"},
			edges:    [][2]string{{"a", "b"}, {"b", "c"}},
			start:    "c", hops: -1,
			want: []string{"a", "b", "c"},
		},
		{
			name:     "diamond topo order",
			untested: []string{"root", "left", "right", "leaf"},
			edges: [][2]string{
				{"root", "left"},
				{"root", "right"},
				{"left", "leaf"},
				{"right", "leaf"},
			},
			start: "leaf", hops: -1,
			want: []string{"root", "left", "right", "leaf"},
		},
		{
			name:     "hops bound",
			untested: []string{"a", "b", "c"},
			edges:    [][2]string{{"a", "b"}, {"b", "c"}},
			start:    "c", hops: 1,
			want: []string{"b", "c"},
		},
		{
			name:     "hops zero returns start only",
			untested: []string{"a", "b"},
			edges:    [][2]string{{"a", "b"}},
			start:    "b", hops: 0,
			want: []string{"b"},
		},
		{
			name:     "mixed passed and untested",
			passed:   []string{"p1"},
			untested: []string{"u1", "u2"},
			edges:    [][2]string{{"p1", "u1"}, {"u1", "u2"}},
			start:    "u2", hops: -1,
			want: []string{"p1", "u1", "u2"},
		},
		{
			name:     "unknown concept returns error",
			untested: []string{"a"},
			start:    "nope", hops: -1,
			want: nil,
		},
		// WalkAll rows
		{
			name:     "WalkAll default depth 5 excludes depth-6 concept",
			untested: []string{"a", "b", "c", "d", "e", "f", "g"},
			edges: [][2]string{
				{"a", "b"},
				{"b", "c"},
				{"c", "d"},
				{"d", "e"},
				{"e", "f"},
				{"f", "g"},
			},
			hops: -1, allMode: true,
			want: []string{"a", "b", "c", "d", "e", "f"},
		},
		{
			name:     "WalkAll depth limit excludes deep concepts",
			untested: []string{"a", "b", "c", "d"},
			edges:    [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}},
			hops:     1, allMode: true,
			want: []string{"a", "b"},
		},
		{
			name:     "WalkAll hops zero returns roots only",
			untested: []string{"a", "b", "c"},
			edges:    [][2]string{{"a", "b"}, {"b", "c"}},
			hops:     0, allMode: true,
			want: []string{"a"},
		},
		{
			name:     "WalkAll multi-root ordering",
			untested: []string{"a", "b", "c"},
			edges:    [][2]string{{"a", "c"}, {"b", "c"}},
			hops:     -1, allMode: true,
			want: []string{"a", "b", "c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := buildGraph(tc.passed, tc.untested, tc.edges)
			s := state.LoadFromGraph(g, state.Config{})
			var got []report.ConceptInfo
			if tc.allMode {
				got = report.WalkAll(g, s, tc.hops)
			} else {
				var err error
				got, err = report.Walk(g, s, tc.start, tc.hops)
				if tc.want == nil {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if strings.Join(ids(got), ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", ids(got), tc.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	// stubReader returns fixed text and drift flag.
	type readerResult struct {
		text    string
		drifted bool
		err     error
	}
	makeReader := func(res readerResult) report.TextReader {
		return func(_, _ string) (string, bool, error) {
			return res.text, res.drifted, res.err
		}
	}

	tests := []struct {
		name     string
		setup    func() ([]report.ConceptInfo, report.Options, report.TextReader)
		contains []string
		absent   []string
	}{
		{
			name: "outline basic: headings, state, footnotes",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a", "b"}, [][2]string{{"a", "b"}})
				g.UntestedConcepts[0].Cites = []string{"abc@src.txt:1-5"}
				g.UntestedConcepts[1].Cites = []string{"def@src.txt:6-10"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "b", -1)
				return c, report.Options{}, nil
			},
			contains: []string{"## a: a scope", "## b: b scope", "[^1]: abc@src.txt:1-5", "[^2]: def@src.txt:6-10", "Sources: [^1]"},
		},
		{
			name: "outline URI cite: footnote is a Markdown link",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc123def456@https://example.com/page:1-5"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				return c, report.Options{}, nil
			},
			contains: []string{"[^1]: [abc123def456@https://example.com/page:1-5](https://example.com/page)"},
		},
		{
			name: "outline GAP and fail summaries",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := &graph.Graph{}
				cn := &graph.ConceptNode{ID: "lm", Scope: "Log matching", GAP: "ignores term", Block: graph.BlockUntested}
				g.UntestedConcepts = []*graph.ConceptNode{cn}
				q1 := &graph.QuestionNode{ID: "q1", Scope: "s", Cite: "x.txt:1-5", Class: "probe_1"}
				a1 := &graph.AnswerNode{ID: "a1", Class: "fail", Label: "missed term"}
				g.TestingItems = []graph.TestingItem{{Q: q1}, {A: a1}}
				g.Edges = []*graph.Edge{{From: "lm", To: "q1"}, {From: "q1", To: "a1"}}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "lm", -1)
				return c, report.Options{}, nil
			},
			contains: []string{"GAP: ignores term", "- missed term"},
		},
		{
			name: "passed-only drops untested, empty returns empty string",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				return c, report.Options{PassedOnly: true}, nil
			},
			// all untested → Render returns ""
			absent: []string{"## a"},
		},
		{
			name: "fulltext basic: anchor, Source, fenced block",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc@src.txt:1-3"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				r := makeReader(readerResult{text: "line 1\nline 2", drifted: false})
				return c, report.Options{Fulltext: true, SrcRoot: "/any"}, r
			},
			contains: []string{`<a id="a">`, "Source: abc@src.txt:1-3", "```\nline 1\nline 2\n```"},
		},
		{
			name: "fulltext repeated citation: back-link, reader called once",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a", "b"}, [][2]string{{"a", "b"}})
				same := "abc@src.txt:1-3"
				g.UntestedConcepts[0].Cites = []string{same}
				g.UntestedConcepts[1].Cites = []string{same}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "b", -1)
				r := makeReader(readerResult{text: "text"})
				return c, report.Options{Fulltext: true, SrcRoot: "/any"}, r
			},
			contains: []string{"see [#a](#a)", "```\ntext\n```"},
		},
		{
			name: "fulltext drift: DRIFT line between Source and fenced block",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc@src.txt:1-3"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				r := makeReader(readerResult{text: "old", drifted: true})
				return c, report.Options{Fulltext: true, SrcRoot: "/any"}, r
			},
			contains: []string{"Source: abc@src.txt:1-3\nDRIFT abc@src.txt:1-3\n```\nold\n```"},
		},
		{
			name: "fulltext unreadable: visible error, no fenced block",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc@src.txt:1-3"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				r := makeReader(readerResult{err: errors.New("access denied")})
				return c, report.Options{Fulltext: true, SrcRoot: "/any"}, r
			},
			contains: []string{"Source unreadable: access denied"},
			absent:   []string{"```"},
		},
		{
			name: "fulltext window over by lines: cut with more: trailer",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc123def456@src.txt:10-20"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				r := makeReader(readerResult{text: "R1\nR2\nR3\nR4\nR5"})
				return c, report.Options{Fulltext: true, SrcRoot: "/any", WindowLineMax: 3, WindowCharMax: 8000}, r
			},
			contains: []string{"```\nR1\nR2\nR3\n```", "more: tm src src.txt 13-20"},
			absent:   []string{"R4"},
		},
		{
			name: "fulltext window over by chars: whole-line cut",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{"abc123def456@src.txt:1-3"}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				r := makeReader(readerResult{text: "abcd\nabcd\nabcd"})
				return c, report.Options{Fulltext: true, SrcRoot: "/any", WindowLineMax: 200, WindowCharMax: 5}, r
			},
			contains: []string{"```\nabcd\n```", "more: tm src src.txt 2-3"},
		},
		{
			name: "fulltext window is one budget across a concept's citations",
			setup: func() ([]report.ConceptInfo, report.Options, report.TextReader) {
				g := buildGraph(nil, []string{"a"}, nil)
				g.UntestedConcepts[0].Cites = []string{
					"aaa111bbb222@src.txt:1-10",
					"ccc333ddd444@src.txt:11-20",
				}
				s := state.LoadFromGraph(g, state.Config{})
				c, _ := report.Walk(g, s, "a", -1)
				ten := make([]string, 10)
				for i := range ten {
					ten[i] = fmt.Sprintf("M%d", i+1)
				}
				r := makeReader(readerResult{text: strings.Join(ten, "\n")})
				// 10 lines from the first citation plus 5 from the second reach the
				// 15-line budget; the window is cut inside the second citation.
				return c, report.Options{Fulltext: true, SrcRoot: "/any", WindowLineMax: 15, WindowCharMax: 80000}, r
			},
			contains: []string{"Source: aaa111bbb222@src.txt:1-10", "more: tm src src.txt 16-20"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			concepts, opts, reader := tc.setup()
			out := report.Render(concepts, opts, reader)
			for _, s := range tc.contains {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q;\ngot:\n%s", s, out)
				}
			}
			for _, s := range tc.absent {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q;\ngot:\n%s", s, out)
				}
			}
		})
	}
}

// TestWalkAll_ExcludesReserveConcepts verifies that WalkAll omits reserve
// concepts and edges involving them (spec §6: reserve concepts are omitted).
func TestWalkAll_ExcludesReserveConcepts(t *testing.T) {
	g := &graph.Graph{}
	// Passed
	g.PassedConcepts = []*graph.ConceptNode{
		{ID: "p1", Scope: "passed 1", Block: graph.BlockPassed},
	}
	// Untested
	g.UntestedConcepts = []*graph.ConceptNode{
		{ID: "u1", Scope: "untested 1", Block: graph.BlockUntested},
	}
	// Reserve
	g.ReserveConcepts = []*graph.ConceptNode{
		{ID: "r1", Scope: "reserve 1", Block: graph.BlockReserve},
	}
	// Edges: p1→u1 (normal), r1→u1 (involves reserve)
	g.Edges = []*graph.Edge{
		{From: "p1", To: "u1", Label: "requires"},
		{From: "r1", To: "u1", Label: "bounds"},
	}

	s := state.LoadFromGraph(g, state.Config{})
	got := report.WalkAll(g, s, -1)

	// Reserve concept must not appear.
	for _, ci := range got {
		if ci.Node.ID == "r1" {
			t.Errorf("WalkAll returned reserve concept %q; it should be excluded", ci.Node.ID)
		}
	}
	// Passed and untested concepts must appear.
	found := map[string]bool{}
	for _, ci := range got {
		found[ci.Node.ID] = true
	}
	if !found["p1"] {
		t.Errorf("WalkAll did not return passed concept p1; got %v", ids(got))
	}
	if !found["u1"] {
		t.Errorf("WalkAll did not return untested concept u1; got %v", ids(got))
	}
}
