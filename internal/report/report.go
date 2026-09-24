// Package report implements the tm report walk and Markdown formatter.
//
// Walk traverses parent edges from a starting concept to collect its
// foundations; WalkAll traverses the whole graph from roots; Render formats
// the result as Markdown for the teacher.
package report

import (
	"errors"
	"fmt"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// ErrUnknownConcept is returned by Walk when startID is not known.
var ErrUnknownConcept = errors.New("unknown concept")

// ConceptInfo holds derived data for one concept in the report walk.
type ConceptInfo struct {
	Node          *graph.ConceptNode
	ConceptState  string   // "passed" | "open" | "blocked" | "gated"
	FailSummaries []string // grader labels from fail-class probe answers, in declaration order
}

// Options controls how Render formats the Markdown.
type Options struct {
	Fulltext   bool
	PassedOnly bool
	SrcRoot    string
}

// TextReader reads source text and drift status for citeStr under srcRoot.
// drifted is true when the stored hash does not match the current content.
// The CLI passes a wrapper that calls readCiteText then cite.CheckDrift, so
// the report package never calls cite.CheckDrift or cite.ReadRange directly.
type TextReader func(citeStr, srcRoot string) (text string, drifted bool, err error)

// ── internal helpers ─────────────────────────────────────────────────────────

// buildConceptMap returns a map from concept ID → *graph.ConceptNode for all
// four blocks (passed, untested, reserve, and their edges). Reserve concepts
// are included so that edge resolution works correctly; Walk/WalkAll exclude
// them from output until --reserve is implemented (M14c).
func buildConceptMap(g *graph.Graph) map[string]*graph.ConceptNode {
	all := make(map[string]*graph.ConceptNode, len(g.PassedConcepts)+len(g.UntestedConcepts)+len(g.ReserveConcepts))
	for _, c := range g.PassedConcepts {
		all[c.ID] = c
	}
	for _, c := range g.UntestedConcepts {
		all[c.ID] = c
	}
	for _, c := range g.ReserveConcepts {
		all[c.ID] = c
	}
	return all
}

// reserveSet returns a set of concept IDs that are in the reserve block.
func reserveSet(g *graph.Graph) map[string]bool {
	m := make(map[string]bool, len(g.ReserveConcepts))
	for _, c := range g.ReserveConcepts {
		m[c.ID] = true
	}
	return m
}

// conceptParentMap returns parentMap[childID] = []parentConceptIDs.
// Only concept-to-concept edges between non-reserve concepts are included;
// reserve parents are excluded from the walk so they never appear in reports
// (spec §6: "reserve concepts are omitted unless --reserve").
func conceptParentMap(g *graph.Graph, all map[string]*graph.ConceptNode) map[string][]string {
	reserve := reserveSet(g)
	pm := make(map[string][]string, len(all))
	for _, e := range g.Edges {
		if _, ok := all[e.From]; !ok {
			continue
		}
		if _, ok := all[e.To]; !ok {
			continue
		}
		// Skip edges involving reserve concepts (omitted from reports).
		if reserve[e.From] || reserve[e.To] {
			continue
		}
		pm[e.To] = append(pm[e.To], e.From)
	}
	return pm
}

// buildDeclOrder returns concept IDs in declaration order (passed first, then
// untested) limited to those whose ID is in included.
func buildDeclOrder(g *graph.Graph, included map[string]bool) []string {
	out := make([]string, 0, len(included))
	for _, c := range g.PassedConcepts {
		if included[c.ID] {
			out = append(out, c.ID)
		}
	}
	for _, c := range g.UntestedConcepts {
		if included[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out
}

// kahnSort returns a topological order over the included concept IDs using
// declaration order (decl) as the tie-breaker. Roots appear first.
func kahnSort(included map[string]bool, decl []string, g *graph.Graph, all map[string]*graph.ConceptNode) []string {
	childEdges := make(map[string][]string, len(included))
	seenEdge := make(map[[2]string]bool)
	inDeg := make(map[string]int, len(included))
	for _, id := range decl {
		inDeg[id] = 0
	}
	for _, e := range g.Edges {
		if !included[e.From] || !included[e.To] {
			continue
		}
		if _, ok := all[e.From]; !ok {
			continue
		}
		if _, ok := all[e.To]; !ok {
			continue
		}
		key := [2]string{e.From, e.To}
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		childEdges[e.From] = append(childEdges[e.From], e.To)
		inDeg[e.To]++
	}

	ready := make(map[string]bool, len(included))
	for _, id := range decl {
		if inDeg[id] == 0 {
			ready[id] = true
		}
	}

	order := make([]string, 0, len(included))
	for len(order) < len(included) {
		chosen := ""
		for _, id := range decl {
			if ready[id] {
				chosen = id
				break
			}
		}
		if chosen == "" {
			break // cycle guard; valid graphs never reach here
		}
		delete(ready, chosen)
		order = append(order, chosen)
		for _, child := range childEdges[chosen] {
			inDeg[child]--
			if inDeg[child] == 0 {
				ready[child] = true
			}
		}
	}
	return order
}

// deriveConceptInfos builds a ConceptInfo for each ID in topoOrder.
func deriveConceptInfos(topoOrder []string, all map[string]*graph.ConceptNode, g *graph.Graph, s *state.State) []ConceptInfo {
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	frontier := s.Frontier()
	frontierSet := make(map[string]bool, len(frontier))
	for _, f := range frontier {
		frontierSet[f] = true
	}

	questionByID := make(map[string]*graph.QuestionNode)
	for _, item := range g.TestingItems {
		if item.Q != nil {
			questionByID[item.Q.ID] = item.Q
		}
	}
	answerByQN := make(map[int]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.A != nil {
			n := graph.QuestionN(item.A.ID)
			if n > 0 {
				answerByQN[n] = item.A
			}
		}
	}

	conceptProbeQs := make(map[string][]string)
	for _, e := range g.Edges {
		if _, isConcept := all[e.From]; !isConcept {
			continue
		}
		q, isQ := questionByID[e.To]
		if !isQ {
			continue
		}
		if graph.IsProbeClass(q.Class) {
			conceptProbeQs[e.From] = append(conceptProbeQs[e.From], q.ID)
		}
	}

	concepts := make([]ConceptInfo, 0, len(topoOrder))
	for _, id := range topoOrder {
		cn := all[id]

		var cState string
		if passedSet[id] {
			cState = "passed"
		} else {
			cs := s.ConceptStatus(id)
			switch {
			case frontierSet[id] && cs.Gated:
				cState = "gated"
			case frontierSet[id]:
				cState = "open"
			default:
				cState = "blocked"
			}
		}

		var failSums []string
		for _, qid := range conceptProbeQs[id] {
			n := graph.QuestionN(qid)
			if n == 0 {
				continue
			}
			if a := answerByQN[n]; a != nil && a.Class == "fail" {
				failSums = append(failSums, a.Label)
			}
		}

		concepts = append(concepts, ConceptInfo{
			Node:          cn,
			ConceptState:  cState,
			FailSummaries: failSums,
		})
	}
	return concepts
}

// ── public walk API ───────────────────────────────────────────────────────────

// Walk returns the concepts reachable from startID via parent (prerequisite)
// edges, in topological order with roots first.
//
// hops < 0 means unbounded. hops == 0 returns only the start concept.
// hops > 0 limits the walk to that many hops from the start.
//
// Returns ErrUnknownConcept when startID is not a concept in g.
func Walk(g *graph.Graph, s *state.State, startID string, hops int) ([]ConceptInfo, error) {
	all := buildConceptMap(g)
	if _, ok := all[startID]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownConcept, startID)
	}

	parentMap := conceptParentMap(g, all)

	// BFS from startID following parent edges, bounded by hops when hops >= 0.
	type bfsItem struct {
		id    string
		depth int
	}
	visited := map[string]bool{startID: true}
	queue := []bfsItem{{startID, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if hops >= 0 && cur.depth >= hops {
			continue
		}
		for _, parentID := range parentMap[cur.id] {
			if !visited[parentID] {
				visited[parentID] = true
				queue = append(queue, bfsItem{parentID, cur.depth + 1})
			}
		}
	}

	decl := buildDeclOrder(g, visited)
	order := kahnSort(visited, decl, g, all)
	return deriveConceptInfos(order, all, g, s), nil
}

// WalkAll returns every concept reachable from graph roots (concepts with no
// parent concept) via child edges, bounded by depth. depth < 0 uses the
// default limit of 5. depth == 0 returns roots only.
//
// Output is in topological order with roots first, declaration-order
// tie-breaking, matching the ordering Walk produces.
func WalkAll(g *graph.Graph, s *state.State, depth int) []ConceptInfo {
	const defaultDepth = 5
	if depth < 0 {
		depth = defaultDepth
	}

	all := buildConceptMap(g)
	parentMap := conceptParentMap(g, all)
	reserve := reserveSet(g)

	// Build child map (concept-level): childMap[parentID] = []childIDs.
	// Exclude reserve concepts so they are never traversed as BFS roots or
	// children (they are omitted from report output by default until M14c).
	childMap := make(map[string][]string, len(all))
	seenEdge := make(map[[2]string]bool)
	for _, e := range g.Edges {
		if _, ok := all[e.From]; !ok {
			continue
		}
		if _, ok := all[e.To]; !ok {
			continue
		}
		// Skip edges involving reserve concepts.
		if reserve[e.From] || reserve[e.To] {
			continue
		}
		key := [2]string{e.From, e.To}
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		childMap[e.From] = append(childMap[e.From], e.To)
	}

	// BFS from roots (non-reserve concepts with no parents), following child edges.
	type bfsItem struct {
		id    string
		depth int
	}
	visited := make(map[string]bool, len(all))
	var queue []bfsItem
	for id := range all {
		// Reserve concepts are excluded from report output.
		if reserve[id] {
			continue
		}
		if len(parentMap[id]) == 0 {
			visited[id] = true
			queue = append(queue, bfsItem{id, 0})
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= depth {
			continue
		}
		for _, childID := range childMap[cur.id] {
			if !visited[childID] {
				visited[childID] = true
				queue = append(queue, bfsItem{childID, cur.depth + 1})
			}
		}
	}

	decl := buildDeclOrder(g, visited)
	order := kahnSort(visited, decl, g, all)
	return deriveConceptInfos(order, all, g, s)
}

// ── Markdown rendering ────────────────────────────────────────────────────────

// Render produces a Markdown report from a pre-walked concept list.
// reader is used in fulltext mode to read cited source text; pass nil to skip
// text inlining (outline-only).
func Render(concepts []ConceptInfo, opts Options, reader TextReader) string {
	// Filter if --passed-only.
	filtered := make([]ConceptInfo, 0, len(concepts))
	for _, c := range concepts {
		if opts.PassedOnly && c.ConceptState != "passed" {
			continue
		}
		filtered = append(filtered, c)
	}

	if len(filtered) == 0 {
		return ""
	}

	if opts.Fulltext && reader != nil {
		return renderFulltext(filtered, opts, reader)
	}
	return renderOutline(filtered)
}

// renderOutline formats concepts without inlining source text. Citations appear
// as footnote references in each concept heading block and as definitions at
// the end. URI locators are rendered as Markdown links in the definitions.
func renderOutline(concepts []ConceptInfo) string {
	var sb strings.Builder

	// Footnote registry: cite string → footnote number (1-based).
	footnotes := make(map[string]int)
	var footnoteOrder []string

	footnoteNum := func(citeStr string) int {
		if n, ok := footnotes[citeStr]; ok {
			return n
		}
		n := len(footnoteOrder) + 1
		footnotes[citeStr] = n
		footnoteOrder = append(footnoteOrder, citeStr)
		return n
	}

	for _, ci := range concepts {
		cn := ci.Node

		fmt.Fprintf(&sb, "## %s: %s\n\n", cn.ID, cn.Scope)
		fmt.Fprintf(&sb, "state: %s\n", ci.ConceptState)

		if cn.GAP != "" {
			fmt.Fprintf(&sb, "GAP: %s\n", cn.GAP)
		}

		if len(ci.FailSummaries) > 0 {
			sb.WriteString("\nFailed probes:\n")
			for _, s := range ci.FailSummaries {
				fmt.Fprintf(&sb, "- %s\n", s)
			}
		}

		if len(cn.Cites) > 0 {
			sb.WriteString("\nSources:")
			for _, c := range cn.Cites {
				fmt.Fprintf(&sb, " [^%d]", footnoteNum(c))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}

	// Footnote definitions.
	for _, citeStr := range footnoteOrder {
		n := footnotes[citeStr]
		c, err := cite.Parse(citeStr)
		if err == nil && cite.IsURI(c.File) {
			fmt.Fprintf(&sb, "[^%d]: [%s](%s)\n", n, citeStr, c.File)
		} else {
			fmt.Fprintf(&sb, "[^%d]: %s\n", n, citeStr)
		}
	}

	return sb.String()
}

// renderFulltext formats concepts with cited source text inlined in fenced
// blocks. Repeated citations link back to the first concept heading's anchor
// instead of re-emitting the text. A drifted citation emits a DRIFT line
// between the citation line and the fenced block. An unreadable source
// emits a visible error line with no fenced block.
func renderFulltext(concepts []ConceptInfo, opts Options, reader TextReader) string {
	var sb strings.Builder

	// firstAnchor[citeStr] = concept ID where this citation's text was first emitted.
	firstAnchor := make(map[string]string)

	for _, ci := range concepts {
		cn := ci.Node

		// HTML anchor for back-links from repeated citations.
		fmt.Fprintf(&sb, "<a id=\"%s\"></a>\n\n", cn.ID)

		fmt.Fprintf(&sb, "## %s: %s\n\n", cn.ID, cn.Scope)
		fmt.Fprintf(&sb, "state: %s\n", ci.ConceptState)

		if cn.GAP != "" {
			fmt.Fprintf(&sb, "GAP: %s\n", cn.GAP)
		}

		if len(ci.FailSummaries) > 0 {
			sb.WriteString("\nFailed probes:\n")
			for _, s := range ci.FailSummaries {
				fmt.Fprintf(&sb, "- %s\n", s)
			}
		}

		for _, citeStr := range cn.Cites {
			sb.WriteString("\n")

			if anchor, already := firstAnchor[citeStr]; already {
				// Repeated citation: link back instead of re-emitting text.
				fmt.Fprintf(&sb, "Source: %s — see [#%s](#%s)\n", citeStr, anchor, anchor)
				continue
			}
			firstAnchor[citeStr] = cn.ID

			fmt.Fprintf(&sb, "Source: %s\n", citeStr)

			text, drifted, err := reader(citeStr, opts.SrcRoot)
			if err != nil {
				// Unreadable source: visible error so a resuming teacher sees it.
				fmt.Fprintf(&sb, "Source unreadable: %v\n", err)
				continue
			}

			if drifted {
				fmt.Fprintf(&sb, "DRIFT %s\n", citeStr)
			}

			// Fenced block with cited text.
			sb.WriteString("```\n")
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			sb.WriteString(text)
			sb.WriteString("```\n")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
