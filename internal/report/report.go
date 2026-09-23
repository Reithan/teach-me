// Package report implements the tm report walk and Markdown formatter.
//
// Walk traverses parent edges from a starting concept to collect its
// foundations; Render formats them as Markdown for the teacher.
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

// Walk returns the concepts reachable from startID via parent (prerequisite)
// edges, in topological order with roots first.
//
// hops < 0 means unbounded. hops == 0 returns only the start concept.
// hops > 0 limits the walk to that many hops from the start.
//
// Returns ErrUnknownConcept when startID is not a concept in g.
func Walk(g *graph.Graph, s *state.State, startID string, hops int) ([]ConceptInfo, error) {
	// Build a flat map of all concepts for fast lookup.
	allConcepts := make(map[string]*graph.ConceptNode, len(g.PassedConcepts)+len(g.UntestedConcepts))
	for _, c := range g.PassedConcepts {
		allConcepts[c.ID] = c
	}
	for _, c := range g.UntestedConcepts {
		allConcepts[c.ID] = c
	}

	if _, ok := allConcepts[startID]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownConcept, startID)
	}

	// parentMap[id] lists the parent concept IDs (concepts that id depends on).
	parentMap := make(map[string][]string, len(allConcepts))
	for _, e := range g.Edges {
		if _, ok := allConcepts[e.From]; !ok {
			continue
		}
		if _, ok := allConcepts[e.To]; !ok {
			continue
		}
		parentMap[e.To] = append(parentMap[e.To], e.From)
	}

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

	// Declaration order for the visited set (passed first, then untested).
	declOrder := make([]string, 0, len(visited))
	for _, c := range g.PassedConcepts {
		if visited[c.ID] {
			declOrder = append(declOrder, c.ID)
		}
	}
	for _, c := range g.UntestedConcepts {
		if visited[c.ID] {
			declOrder = append(declOrder, c.ID)
		}
	}

	// Build child edges for Kahn's algorithm (within visited concept set only).
	childEdges := make(map[string][]string, len(visited))
	seenEdge := make(map[[2]string]bool)
	inDeg := make(map[string]int, len(visited))
	for _, id := range declOrder {
		inDeg[id] = 0
	}
	for _, e := range g.Edges {
		if !visited[e.From] || !visited[e.To] {
			continue
		}
		if _, ok := allConcepts[e.From]; !ok {
			continue
		}
		if _, ok := allConcepts[e.To]; !ok {
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

	// Kahn's topological sort with declaration-order tie-breaking.
	ready := make(map[string]bool, len(visited))
	for _, id := range declOrder {
		if inDeg[id] == 0 {
			ready[id] = true
		}
	}

	topoOrder := make([]string, 0, len(visited))
	for len(topoOrder) < len(visited) {
		// Take the first ready node in declaration order.
		chosen := ""
		for _, id := range declOrder {
			if ready[id] {
				chosen = id
				break
			}
		}
		if chosen == "" {
			break // cycle guard; valid graphs never reach here
		}
		delete(ready, chosen)
		topoOrder = append(topoOrder, chosen)
		for _, child := range childEdges[chosen] {
			inDeg[child]--
			if inDeg[child] == 0 {
				ready[child] = true
			}
		}
	}

	// Derive per-concept state and fail summaries.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	frontier := s.Frontier()
	frontierSet := make(map[string]bool, len(frontier))
	for _, f := range frontier {
		frontierSet[f] = true
	}

	// Build question index by ID and answer index by question number.
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

	// conceptProbeQs[id] lists probe question IDs belonging to the concept.
	conceptProbeQs := make(map[string][]string)
	for _, e := range g.Edges {
		if _, isConcept := allConcepts[e.From]; !isConcept {
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
		cn := allConcepts[id]

		// Derive concept state.
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

		// Collect fail summaries from probe questions in declaration order.
		// Declaration order matches the order answers were recorded.
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

	return concepts, nil
}

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
