package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// showRun is the Run handler for `tm show <id> [--history]`.
//
// Prints the node record for the given concept, question, or answer ID.
// With --history, appends the node's event-log events.
//
// Output format (greppable key:value headers):
//
//	Concept:
//	  id: <id>
//	  kind: concept
//	  state: passed|open|blocked
//	  scope: <scope>
//	  gap: <gap>               (only when present)
//	  src: <cite1>, <cite2>
//	  parents: <id>, ...
//	  children: <id>, ...
//	  (blank line)
//	  <batch> <state>
//	    <qid> <verdict>  <label>   (answered)
//	    <qid> -  <scope>           (unanswered)
//
//	Question:
//	  id: <qid>
//	  kind: q
//	  batch: <class>
//	  concept: <id>
//	  scope: <scope>
//	  src: <cite>
//	  answer: <aid>              (when answered)
//	    state: <class>
//	    label: <label>
//	  answer: none               (when unanswered)
//
//	Answer:
//	  id: <aid>
//	  kind: answer
//	  question: <qid>
//	  state: <class>
//	  label: <label>
//
//	History (--history): appended after a "---" separator; each matched event
//	on its own line as JSON with string content fields unescaped.
//
// Exit codes:
//
//	0  ok
//	3  unknown id or file error
func showRun(ctx *Context) int {
	id := ctx.Positionals[0]
	withHistory := len(ctx.Flags["history"]) > 0

	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	cfg := state.ConfigFromEnv()
	s, loadErr := state.Load(file, cfg)
	if loadErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	g := s.Graph()
	answerByQID := buildAnswerMap(g)

	// Build concept sets.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	untestedSet := make(map[string]bool, len(g.UntestedConcepts))
	for _, c := range g.UntestedConcepts {
		untestedSet[c.ID] = true
	}

	// Build question and answer maps.
	qMap := make(map[string]*graph.QuestionNode)
	for _, item := range g.TestingItems {
		if item.Q != nil {
			qMap[item.Q.ID] = item.Q
		}
	}
	aMap := make(map[string]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.A != nil {
			aMap[item.A.ID] = item.A
		}
	}

	switch {
	case passedSet[id] || untestedSet[id]:
		return showConcept(ctx, s, g, id, passedSet, untestedSet, answerByQID, withHistory, file)
	case qMap[id] != nil:
		return showQuestion(ctx, s, g, id, qMap, answerByQID, withHistory, file)
	case aMap[id] != nil:
		return showAnswer(ctx, g, id, aMap, answerByQID, withHistory, file)
	default:
		ctx.ErrMsg = fmt.Sprintf("unknown id %q", id)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
}

// showConcept prints the node record for a concept.
func showConcept(ctx *Context, s *state.State, g *graph.Graph, id string,
	passedSet, untestedSet map[string]bool,
	answerByQID map[string]*graph.AnswerNode,
	withHistory bool, graphFile string,
) int {
	allConceptSet := make(map[string]bool)
	for k := range passedSet {
		allConceptSet[k] = true
	}
	for k := range untestedSet {
		allConceptSet[k] = true
	}

	// Determine concept state.
	nodeState := "passed"
	var concept *graph.ConceptNode
	if passedSet[id] {
		for _, c := range g.PassedConcepts {
			if c.ID == id {
				concept = c
				break
			}
		}
	} else {
		frontier := s.Frontier()
		frontierSet := make(map[string]bool, len(frontier))
		for _, f := range frontier {
			frontierSet[f] = true
		}
		nodeState = "blocked"
		if frontierSet[id] {
			nodeState = "open"
		}
		for _, c := range g.UntestedConcepts {
			if c.ID == id {
				concept = c
				break
			}
		}
	}
	if concept == nil {
		ctx.ErrMsg = fmt.Sprintf("unknown id %q", id)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Header fields.
	_, _ = fmt.Fprintf(ctx.Out, "id: %s\n", id)
	_, _ = fmt.Fprintf(ctx.Out, "kind: concept\n")
	_, _ = fmt.Fprintf(ctx.Out, "state: %s\n", nodeState)
	_, _ = fmt.Fprintf(ctx.Out, "scope: %s\n", concept.Scope)
	if concept.GAP != "" {
		_, _ = fmt.Fprintf(ctx.Out, "gap: %s\n", concept.GAP)
	}
	if len(concept.Cites) > 0 {
		_, _ = fmt.Fprintf(ctx.Out, "src: %s\n", strings.Join(concept.Cites, ", "))
	}

	// Parents and children (concept-to-concept edges).
	var parents, children []string
	for _, e := range g.Edges {
		if e.To == id && allConceptSet[e.From] {
			parents = append(parents, e.From)
		}
		if e.From == id && allConceptSet[e.To] {
			children = append(children, e.To)
		}
	}
	if len(parents) > 0 {
		_, _ = fmt.Fprintf(ctx.Out, "parents: %s\n", strings.Join(parents, ", "))
	}
	if len(children) > 0 {
		_, _ = fmt.Fprintf(ctx.Out, "children: %s\n", strings.Join(children, ", "))
	}

	// Batches section (blank line separator then one block per batch).
	batches := askConceptBatches(g, s, id)
	if len(batches) > 0 {
		_, _ = fmt.Fprintln(ctx.Out)
		for _, batchClass := range batches {
			bst := s.BatchStateOf(batchClass)
			bstStr := batchStateStr(bst)
			_, _ = fmt.Fprintf(ctx.Out, "%s %s\n", batchClass, bstStr)

			for _, item := range g.TestingItems {
				if item.Q == nil || item.Q.Class != batchClass {
					continue
				}
				q := item.Q
				a := answerByQID[q.ID]
				if a == nil {
					_, _ = fmt.Fprintf(ctx.Out, "  %s -  %s\n", q.ID, q.Scope)
				} else {
					_, _ = fmt.Fprintf(ctx.Out, "  %s %s  %s\n", q.ID, a.Class, a.Label)
				}
			}
		}
	}

	if withHistory {
		showHistory(ctx, id, graphFile)
	}
	return 0
}

// showQuestion prints the node record for a question.
func showQuestion(ctx *Context, s *state.State, _ *graph.Graph, id string,
	qMap map[string]*graph.QuestionNode,
	answerByQID map[string]*graph.AnswerNode,
	withHistory bool, graphFile string,
) int {
	q := qMap[id]
	if q == nil {
		ctx.ErrMsg = fmt.Sprintf("unknown id %q", id)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	conceptID, _ := s.ConceptOf(id)

	_, _ = fmt.Fprintf(ctx.Out, "id: %s\n", id)
	_, _ = fmt.Fprintf(ctx.Out, "kind: q\n")
	_, _ = fmt.Fprintf(ctx.Out, "batch: %s\n", q.Class)
	if conceptID != "" {
		_, _ = fmt.Fprintf(ctx.Out, "concept: %s\n", conceptID)
	}
	_, _ = fmt.Fprintf(ctx.Out, "scope: %s\n", q.Scope)
	_, _ = fmt.Fprintf(ctx.Out, "src: %s\n", q.Cite)

	// Answer section.
	a := answerByQID[id]
	if a == nil {
		_, _ = fmt.Fprintln(ctx.Out, "answer: none")
	} else {
		aID := fmt.Sprintf("a%d", graph.QuestionN(id))
		_, _ = fmt.Fprintf(ctx.Out, "answer: %s\n", aID)
		_, _ = fmt.Fprintf(ctx.Out, "  state: %s\n", a.Class)
		_, _ = fmt.Fprintf(ctx.Out, "  label: %s\n", a.Label)
		if a.OOS {
			_, _ = fmt.Fprintln(ctx.Out, "  oos: true")
		}
	}

	if withHistory {
		showHistory(ctx, id, graphFile)
	}
	return 0
}

// showAnswer prints the node record for an answer.
func showAnswer(ctx *Context, _ *graph.Graph, id string,
	aMap map[string]*graph.AnswerNode,
	_ map[string]*graph.AnswerNode,
	withHistory bool, graphFile string,
) int {
	a := aMap[id]
	if a == nil {
		ctx.ErrMsg = fmt.Sprintf("unknown id %q", id)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Derive the question ID from the answer ID (aN → qN).
	n := graph.QuestionN(id)
	qID := fmt.Sprintf("q%d", n)

	_, _ = fmt.Fprintf(ctx.Out, "id: %s\n", id)
	_, _ = fmt.Fprintf(ctx.Out, "kind: answer\n")
	_, _ = fmt.Fprintf(ctx.Out, "question: %s\n", qID)
	_, _ = fmt.Fprintf(ctx.Out, "state: %s\n", a.Class)
	_, _ = fmt.Fprintf(ctx.Out, "label: %s\n", a.Label)
	if a.OOS {
		_, _ = fmt.Fprintln(ctx.Out, "oos: true")
	}

	if withHistory {
		showHistory(ctx, id, graphFile)
	}
	return 0
}

// showHistory appends the history section to ctx.Out. The event log is the
// graph file with ".jsonl" appended (per §3 line 60). Events are filtered to
// those referencing id via the "id", "q", "concept", "from", or "to" fields
// (per §10). String content fields are unescaped with graph.Unescape.
// A missing log file is not an error.
func showHistory(ctx *Context, id, graphFile string) {
	logFile := graphFile + ".jsonl"
	data, err := os.ReadFile(logFile)
	if err != nil {
		return
	}

	_, _ = fmt.Fprintln(ctx.Out, "---")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse as a generic map.
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		// Filter: include events referencing id via any of these fields.
		matched := false
		for _, key := range []string{"id", "q", "concept", "from", "to"} {
			if v, ok := ev[key]; ok {
				if s, ok := v.(string); ok && s == id {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}

		// Unescape all string values (§4.4: show unescapes content fields).
		unescapeEventStrings(ev)

		// Re-marshal and print.
		out, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintln(ctx.Out, string(out))
	}
}

// unescapeEventStrings unescapes all string values in the event map in-place.
func unescapeEventStrings(ev map[string]any) {
	for k, v := range ev {
		if s, ok := v.(string); ok {
			ev[k] = graph.Unescape(s)
		}
	}
}
