package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// statusRun is the Run handler for `tm status [--passed] [--concept <id>]`.
//
// Without --concept: prints counts line and one line per open (frontier) or
// blocked concept. With --concept: prints the detail view for that concept,
// chaining tm ask when applicable.
//
// Exit codes:
//
//	0  ok
//	3  file error or unknown concept id
func statusRun(ctx *Context) int {
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

	conceptID := ""
	if v := ctx.Flags["concept"]; len(v) > 0 {
		conceptID = v[0]
	}
	showPassed := len(ctx.Flags["passed"]) > 0

	if conceptID != "" {
		return statusConcept(ctx, s, conceptID)
	}
	return statusSummary(ctx, s, showPassed)
}

// batchStateStr converts a BatchStatus to its display string.
func batchStateStr(bs state.BatchStatus) string {
	switch bs {
	case state.BatchDraft:
		return "draft"
	case state.BatchLocked:
		return "locked"
	case state.BatchOpen:
		return "open"
	case state.BatchResolved:
		return "resolved"
	}
	return "draft"
}

// buildAnswerMap returns a map from "qN" → *AnswerNode for all answers in g.
// The key is the question ID (e.g. "q2") that the answer corresponds to.
func buildAnswerMap(g *graph.Graph) map[string]*graph.AnswerNode {
	m := make(map[string]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.A == nil {
			continue
		}
		n := graph.QuestionN(item.A.ID)
		if n == 0 {
			continue
		}
		m[fmt.Sprintf("q%d", n)] = item.A
	}
	return m
}

// statusLastActiveBatch returns the last non-resolved batch for a concept
// (the one shown in the summary line). Returns the last batch if all resolved.
// Returns "" if no batches exist.
func statusLastActiveBatch(s *state.State, batches []string) string {
	last := ""
	for _, b := range batches {
		if s.BatchStateOf(b) != state.BatchResolved {
			last = b
		}
	}
	if last == "" && len(batches) > 0 {
		last = batches[len(batches)-1]
	}
	return last
}

// statusSummary prints the overview status output per §6 sample lines 229-232.
//
// Line 1: "passed N  open M  blocked K"
// Then one line per frontier (open) concept, then one line per blocked concept.
// With --passed, passed concepts are also listed.
func statusSummary(ctx *Context, s *state.State, showPassed bool) int {
	g := s.Graph()

	// Build concept membership sets for the blocking-parent check.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	allConceptSet := make(map[string]bool)
	for k := range passedSet {
		allConceptSet[k] = true
	}
	for _, c := range g.UntestedConcepts {
		allConceptSet[c.ID] = true
	}

	frontier := s.Frontier()
	blocked := s.Blocked()

	frontierSet := make(map[string]bool, len(frontier))
	for _, f := range frontier {
		frontierSet[f] = true
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		blockedSet[b] = true
	}

	// Line 1: counts.
	_, _ = fmt.Fprintf(ctx.Out, "passed %d  open %d  blocked %d\n",
		len(g.PassedConcepts), len(frontier), len(blocked))

	// Optional: list passed concepts (--passed flag).
	if showPassed {
		for _, c := range g.PassedConcepts {
			_, _ = fmt.Fprintf(ctx.Out, "%s  passed\n", c.ID)
		}
	}

	// Frontier (open) concept lines, preserving UntestedConcepts declaration order.
	for _, c := range g.UntestedConcepts {
		if !frontierSet[c.ID] {
			continue
		}
		cs := s.ConceptStatus(c.ID)
		batches := askConceptBatches(g, s, c.ID)

		// Count probe batches above base for the "failed x/y" display.
		totalProbe := 0
		for _, b := range batches {
			if graph.IsProbeClass(b) && graph.BatchN(b) > cs.Base {
				totalProbe++
			}
		}
		failedProbe := len(cs.FailedProbeBatches)

		currentBatch := statusLastActiveBatch(s, batches)
		if currentBatch == "" {
			_, _ = fmt.Fprintf(ctx.Out, "%s  failed %d/%d\n", c.ID, failedProbe, totalProbe)
		} else {
			bst := batchStateStr(s.BatchStateOf(currentBatch))
			_, _ = fmt.Fprintf(ctx.Out, "%s  failed %d/%d  %s %s\n",
				c.ID, failedProbe, totalProbe, currentBatch, bst)
		}
	}

	// Blocked concept lines, preserving UntestedConcepts declaration order.
	for _, c := range g.UntestedConcepts {
		if !blockedSet[c.ID] {
			continue
		}
		// Find the first non-passed concept parent in Edges declaration order.
		blockingParent := ""
		for _, e := range g.Edges {
			if e.To == c.ID && allConceptSet[e.From] && !passedSet[e.From] {
				blockingParent = e.From
				break
			}
		}
		if blockingParent != "" {
			_, _ = fmt.Fprintf(ctx.Out, "%s  blocked by %s\n", c.ID, blockingParent)
		} else {
			_, _ = fmt.Fprintf(ctx.Out, "%s  blocked\n", c.ID)
		}
	}
	return 0
}

// statusConcept prints the detailed status for a single concept per §6 sample
// lines 234-242. Chains tm ask when the concept has an answerable batch with
// unanswered questions (§1 line 30).
func statusConcept(ctx *Context, s *state.State, conceptID string) int {
	g := s.Graph()

	// Build concept membership sets.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	allConceptSet := make(map[string]bool)
	for k := range passedSet {
		allConceptSet[k] = true
	}
	for _, c := range g.UntestedConcepts {
		allConceptSet[c.ID] = true
	}

	// Unknown concept → exit 3.
	if !allConceptSet[conceptID] {
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Passed concept: print one line and return (no chain).
	if passedSet[conceptID] {
		frontier := s.Frontier()
		frontierSet := make(map[string]bool, len(frontier))
		for _, f := range frontier {
			frontierSet[f] = true
		}
		// Collect direct children on the frontier (outgoing edges from this concept).
		var unblocked []string
		for _, e := range g.Edges {
			if e.From == conceptID && frontierSet[e.To] {
				unblocked = append(unblocked, e.To)
			}
		}
		line := conceptID + " passed  unblocked"
		if len(unblocked) > 0 {
			line += " " + strings.Join(unblocked, " ")
		}
		_, _ = fmt.Fprintln(ctx.Out, line)
		return 0
	}

	// Find the untested concept node.
	var concept *graph.ConceptNode
	for _, c := range g.UntestedConcepts {
		if c.ID == conceptID {
			concept = c
			break
		}
	}
	if concept == nil {
		// Shouldn't reach here since allConceptSet covers both blocks.
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	cs := s.ConceptStatus(conceptID)
	batches := askConceptBatches(g, s, conceptID)
	answerByQID := buildAnswerMap(g)

	// Count probe batches above base for the "failed x/y" display.
	totalProbe := 0
	for _, b := range batches {
		if graph.IsProbeClass(b) && graph.BatchN(b) > cs.Base {
			totalProbe++
		}
	}
	failedProbe := len(cs.FailedProbeBatches)

	// Header line: "<id>  failed x/y  GAP: <gap>" (GAP appended when present).
	header := fmt.Sprintf("%s  failed %d/%d", conceptID, failedProbe, totalProbe)
	if concept.GAP != "" {
		header += "  GAP: " + concept.GAP
	}
	_, _ = fmt.Fprintln(ctx.Out, header)

	// Collect teaching targets that are still in progress: probe question IDs
	// that are the targets of questions in any currently unresolved teach batch.
	// This covers both "open" targets (no passing teach question yet) and
	// partially-complete targets (some teach questions pass, others still open),
	// which matches the §6 sample where q2 appears as a target even though q5
	// already passes — because q6 (in the same teach batch) is still unanswered.
	currentTargetSet := make(map[string]bool)
	for _, batchClass := range batches {
		if !graph.IsTeachClass(batchClass) {
			continue
		}
		if s.BatchStateOf(batchClass) == state.BatchResolved {
			continue
		}
		for _, item := range g.TestingItems {
			if item.Q == nil || item.Q.Class != batchClass {
				continue
			}
			if targetQID, ok := s.TeachingTarget(item.Q.ID); ok {
				currentTargetSet[targetQID] = true
			}
		}
	}

	// Per-batch lines.
	for _, batchClass := range batches {
		bst := s.BatchStateOf(batchClass)
		bstStr := batchStateStr(bst)

		// Collect questions in this batch in declaration order.
		var batchQs []*graph.QuestionNode
		for _, item := range g.TestingItems {
			if item.Q != nil && item.Q.Class == batchClass {
				batchQs = append(batchQs, item.Q)
			}
		}

		// Build per-question token strings.
		// Answered: "qN <verdict>" (+ " oos" when OOS).
		// Unanswered in locked batch: bare "qN".
		// Unanswered in non-locked batch: "qN -".
		tokens := make([]string, 0, len(batchQs))
		for _, q := range batchQs {
			a := answerByQID[q.ID]
			if a == nil {
				if bst == state.BatchLocked {
					tokens = append(tokens, q.ID)
				} else {
					tokens = append(tokens, q.ID+" -")
				}
			} else {
				tok := q.ID + " " + a.Class
				if a.OOS {
					tok += " oos"
				}
				tokens = append(tokens, tok)
			}
		}

		// Batch line: "  <class> <state>  <tokens>".
		// Separator between tokens: single space for locked (bare IDs),
		// two spaces for open/resolved/draft (tokens include verdicts or dashes).
		var sb strings.Builder
		sb.WriteString("  ")
		sb.WriteString(batchClass)
		sb.WriteString(" ")
		sb.WriteString(bstStr)
		if len(tokens) > 0 {
			sb.WriteString("  ")
			if bst == state.BatchLocked {
				sb.WriteString(strings.Join(tokens, " "))
			} else {
				sb.WriteString(strings.Join(tokens, "  "))
			}
		}
		_, _ = fmt.Fprintln(ctx.Out, sb.String())

		// Target lines (4-space indent) for this batch's questions when they are
		// teaching targets in an unresolved teach batch.
		// "    target <qid> | <scope> | <cite> | <answer label>"
		for _, q := range batchQs {
			if !currentTargetSet[q.ID] {
				continue
			}
			a := answerByQID[q.ID]
			aLabel := ""
			if a != nil {
				aLabel = a.Label
			}
			_, _ = fmt.Fprintf(ctx.Out, "    target %s | %s | %s | %s\n",
				q.ID, q.Scope, q.Cite, aLabel)
		}
	}

	// Chain tm ask when applicable (§1 line 30): run the ask emitter into a
	// buffer; only print the chain header + output when ask would actually emit.
	var askBuf bytes.Buffer
	code, _, _ := emitAskForConcept(&askBuf, s, conceptID)
	if code == 0 && askBuf.Len() > 0 {
		_, _ = fmt.Fprintf(ctx.Out, "> tm ask %s\n", conceptID)
		_, _ = ctx.Out.Write(askBuf.Bytes())
	}

	return 0
}
