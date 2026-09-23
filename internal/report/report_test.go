package report_test

import (
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/report"
	"github.com/reithan/teach-me/internal/state"
)

// buildGraph creates a minimal Graph with the given passed and untested concept
// IDs and edges. Edges are expressed as [2]string{from, to} pairs.
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
		g.Edges = append(g.Edges, &graph.Edge{From: e[0], To: e[1], Label: "requires"})
	}
	return g
}

// ids extracts the concept IDs from a []ConceptInfo slice.
func ids(infos []report.ConceptInfo) []string {
	out := make([]string, len(infos))
	for i, ci := range infos {
		out[i] = ci.Node.ID
	}
	return out
}

func TestWalk_TopoOrder(t *testing.T) {
	// Graph: a → b → c (a is root, c is leaf)
	// Walk from c, unbounded: should return [a, b, c].
	g := buildGraph(nil, []string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"b", "c"}})
	s := state.LoadFromGraph(g, state.Config{})
	got, err := report.Walk(g, s, "c", 0)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	want := []string{"a", "b", "c"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Errorf("Walk order: got %v, want %v", ids(got), want)
	}
}

func TestWalk_HopsLimit(t *testing.T) {
	// Graph: a → b → c, walk from c with hops=1.
	// Hop 1 from c: reaches b. a is 2 hops away, excluded.
	g := buildGraph(nil, []string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"b", "c"}})
	s := state.LoadFromGraph(g, state.Config{})
	got, err := report.Walk(g, s, "c", 1)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	want := []string{"b", "c"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Errorf("Walk hops=1: got %v, want %v", ids(got), want)
	}
}

func TestWalk_DiamondTopoOrder(t *testing.T) {
	// Diamond: root → left, root → right, left → leaf, right → leaf.
	// Declaration order: root, left, right, leaf (all untested).
	// Walk from leaf, unbounded: should be [root, left, right, leaf].
	g := buildGraph(nil, []string{"root", "left", "right", "leaf"},
		[][2]string{{"root", "left"}, {"root", "right"}, {"left", "leaf"}, {"right", "leaf"}})
	s := state.LoadFromGraph(g, state.Config{})
	got, err := report.Walk(g, s, "leaf", 0)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	want := []string{"root", "left", "right", "leaf"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Errorf("Walk diamond: got %v, want %v", ids(got), want)
	}
}

func TestWalk_StartOnly(t *testing.T) {
	// Single concept with no parents: walk returns just itself.
	g := buildGraph(nil, []string{"solo"}, nil)
	s := state.LoadFromGraph(g, state.Config{})
	got, err := report.Walk(g, s, "solo", 0)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	if len(got) != 1 || got[0].Node.ID != "solo" {
		t.Errorf("Walk single: got %v, want [solo]", ids(got))
	}
}

func TestWalk_PassedAndUntestedMixed(t *testing.T) {
	// Passed: p1. Untested: u1, u2. Edges: p1 → u1 → u2.
	// Walk from u2: [p1, u1, u2].
	g := buildGraph([]string{"p1"}, []string{"u1", "u2"},
		[][2]string{{"p1", "u1"}, {"u1", "u2"}})
	s := state.LoadFromGraph(g, state.Config{})
	got, err := report.Walk(g, s, "u2", 0)
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	want := []string{"p1", "u1", "u2"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Errorf("Walk mixed: got %v, want %v", ids(got), want)
	}
}

func TestWalk_UnknownConcept(t *testing.T) {
	g := buildGraph(nil, []string{"a"}, nil)
	s := state.LoadFromGraph(g, state.Config{})
	_, err := report.Walk(g, s, "nope", 0)
	if err == nil {
		t.Fatal("expected error for unknown concept, got nil")
	}
}

func TestRender_OutlineBasic(t *testing.T) {
	g := buildGraph(nil, []string{"a", "b"}, [][2]string{{"a", "b"}})
	// Add citations.
	g.UntestedConcepts[0].Cites = []string{"abc123@src.txt:1-5"}
	g.UntestedConcepts[1].Cites = []string{"def456@src.txt:6-10"}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, err := report.Walk(g, s, "b", 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	out := report.Render(concepts, report.Options{}, nil)

	// Both headings present.
	if !strings.Contains(out, "## a: a scope") {
		t.Errorf("missing heading for a; got:\n%s", out)
	}
	if !strings.Contains(out, "## b: b scope") {
		t.Errorf("missing heading for b; got:\n%s", out)
	}
	// Footnote definitions.
	if !strings.Contains(out, "[^1]: abc123@src.txt:1-5") {
		t.Errorf("missing footnote [^1]; got:\n%s", out)
	}
	if !strings.Contains(out, "[^2]: def456@src.txt:6-10") {
		t.Errorf("missing footnote [^2]; got:\n%s", out)
	}
	// Sources references.
	if !strings.Contains(out, "Sources: [^1]") {
		t.Errorf("missing Sources [^1]; got:\n%s", out)
	}
}

func TestRender_OutlineURICite(t *testing.T) {
	// URI citation should render as a Markdown link in the footnote.
	g := buildGraph(nil, []string{"a"}, nil)
	g.UntestedConcepts[0].Cites = []string{"abc123def456@https://example.com/page:1-5"}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "a", 0)
	out := report.Render(concepts, report.Options{}, nil)

	want := "[^1]: [abc123def456@https://example.com/page:1-5](https://example.com/page)"
	if !strings.Contains(out, want) {
		t.Errorf("URI footnote: want %q in:\n%s", want, out)
	}
}

func TestRender_OutlineGAPAndFailSummaries(t *testing.T) {
	// Add a GAP and a fail answer.
	g := &graph.Graph{}
	cn := &graph.ConceptNode{
		ID: "log_matching", Scope: "Log matching", GAP: "ignores term",
		Block: graph.BlockUntested,
	}
	g.UntestedConcepts = []*graph.ConceptNode{cn}
	q1 := &graph.QuestionNode{ID: "q1", Scope: "q1 scope", Cite: "x.txt:1-5", Class: "probe_1"}
	a1 := &graph.AnswerNode{ID: "a1", Class: "fail", Label: "missed the term check"}
	g.TestingItems = []graph.TestingItem{
		{Q: q1}, {A: a1},
	}
	g.Edges = []*graph.Edge{
		{From: "log_matching", To: "q1"},
		{From: "q1", To: "a1"},
	}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, err := report.Walk(g, s, "log_matching", 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	out := report.Render(concepts, report.Options{}, nil)

	if !strings.Contains(out, "GAP: ignores term") {
		t.Errorf("GAP missing; got:\n%s", out)
	}
	if !strings.Contains(out, "- missed the term check") {
		t.Errorf("fail summary missing; got:\n%s", out)
	}
}

func TestRender_PassedOnly(t *testing.T) {
	// Two concepts: p1 (passed), u1 (untested). Walk from u1. --passed-only should drop u1.
	g := buildGraph([]string{"p1"}, []string{"u1"}, [][2]string{{"p1", "u1"}})
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "u1", 0)
	out := report.Render(concepts, report.Options{PassedOnly: true}, nil)

	if strings.Contains(out, "## u1") {
		t.Errorf("--passed-only should drop u1; got:\n%s", out)
	}
	if !strings.Contains(out, "## p1") {
		t.Errorf("--passed-only should keep p1; got:\n%s", out)
	}
}

func TestRender_PassedOnly_AllDropped(t *testing.T) {
	// All concepts are untested; --passed-only should return empty string.
	g := buildGraph(nil, []string{"a"}, nil)
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "a", 0)
	out := report.Render(concepts, report.Options{PassedOnly: true}, nil)
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestRender_FulltextBasic(t *testing.T) {
	g := buildGraph(nil, []string{"a"}, nil)
	g.UntestedConcepts[0].Cites = []string{"abc123@src.txt:1-3"}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "a", 0)

	reader := func(_, _ string) (string, error) {
		return "line 1\nline 2\nline 3", nil
	}
	out := report.Render(concepts, report.Options{Fulltext: true, SrcRoot: "/any"}, reader)

	if !strings.Contains(out, "<a id=\"a\"></a>") {
		t.Errorf("fulltext missing anchor; got:\n%s", out)
	}
	if !strings.Contains(out, "Source: abc123@src.txt:1-3") {
		t.Errorf("fulltext missing Source line; got:\n%s", out)
	}
	if !strings.Contains(out, "```\nline 1\nline 2\nline 3\n```") {
		t.Errorf("fulltext missing fenced block; got:\n%s", out)
	}
}

func TestRender_FulltextRepeatedCitation(t *testing.T) {
	// Two concepts share the same citation. Second should back-link, not repeat text.
	g := buildGraph(nil, []string{"a", "b"}, [][2]string{{"a", "b"}})
	sameCite := "abc123@src.txt:1-3"
	g.UntestedConcepts[0].Cites = []string{sameCite}
	g.UntestedConcepts[1].Cites = []string{sameCite}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "b", 0)

	calls := 0
	reader := func(_, _ string) (string, error) {
		calls++
		return "cited text", nil
	}
	out := report.Render(concepts, report.Options{Fulltext: true, SrcRoot: "/any"}, reader)

	// reader should only be called once (for the first occurrence).
	if calls != 1 {
		t.Errorf("reader called %d times, want 1", calls)
	}
	// Second concept's section should contain a back-link.
	if !strings.Contains(out, "see [#a](#a)") {
		t.Errorf("repeated cite should back-link to a; got:\n%s", out)
	}
	// The fenced block should appear only once.
	if strings.Count(out, "```\ncited text\n```") != 1 {
		t.Errorf("fenced block should appear once; got:\n%s", out)
	}
}

func TestRender_FulltextDeduplicatedCitations(t *testing.T) {
	// Same citation used twice by same concept: reader called once, text once.
	g := buildGraph(nil, []string{"a"}, nil)
	sameCite := "abc123@src.txt:1-3"
	g.UntestedConcepts[0].Cites = []string{sameCite, sameCite}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "a", 0)

	calls := 0
	reader := func(_, _ string) (string, error) {
		calls++
		return "cited text", nil
	}
	out := report.Render(concepts, report.Options{Fulltext: true, SrcRoot: "/any"}, reader)
	if calls != 1 {
		t.Errorf("reader called %d times for same cite, want 1", calls)
	}
	if strings.Count(out, "```\ncited text\n```") != 1 {
		t.Errorf("fenced block should appear once; got:\n%s", out)
	}
}

func TestRender_FulltextNilReaderFallsBack(t *testing.T) {
	// opts.Fulltext=true but reader=nil: falls back to outline mode (no fenced blocks).
	g := buildGraph(nil, []string{"a"}, nil)
	g.UntestedConcepts[0].Cites = []string{"abc123@src.txt:1-3"}
	s := state.LoadFromGraph(g, state.Config{})
	concepts, _ := report.Walk(g, s, "a", 0)
	out := report.Render(concepts, report.Options{Fulltext: true, SrcRoot: "/any"}, nil)

	if strings.Contains(out, "```") {
		t.Errorf("outline (nil reader) should not emit fenced blocks; got:\n%s", out)
	}
	if !strings.Contains(out, "[^1]") {
		t.Errorf("nil reader should fall back to outline footnotes; got:\n%s", out)
	}
}
