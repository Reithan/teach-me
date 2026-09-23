package report_test

import (
	"errors"
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
		start    string
		hops     int
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := buildGraph(tc.passed, tc.untested, tc.edges)
			s := state.LoadFromGraph(g, state.Config{})
			got, err := report.Walk(g, s, tc.start, tc.hops)
			if tc.want == nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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
