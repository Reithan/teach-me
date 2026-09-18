package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// askRun is the Run handler for `tm ask <concept> [--format lines|json] [--src-text]`.
//
// Read-only: emits the batch the teacher should ask next without mutating
// anything. Per §8: emits the concept's unresolved teach batch if one exists,
// otherwise its unanswered probe batch.
//
// "Nothing to ask" behavior: when no suitable batch is found (no batches at
// all, or all batches resolved) the command exits 0 with no output. The caller
// can infer current state from `tm status`.
//
// Exit codes:
//
//	0  batch emitted, or nothing to ask
//	1  invariant refusal (blocked parent, gated, teaching incomplete)
//	3  unknown concept or file error
//
// --format json shape:
//
//	{"batch":"<class>","questions":[{"id":"<qid>","scope":"...","cite":"...","re":"<qid>","src_text":"..."},...]}
//
// "re" and "src_text" are omitted when not applicable / not requested.
// "re" is derived from state.TeachingTarget for teach questions; for probe
// replacements the current graph model stores no --re relationship, so the
// field is omitted for probes (see M6 note: if §8.5 probe-replacing-unclear
// needs re tracking, add a Re field to graph.QuestionNode).
func askRun(ctx *Context) int {
	conceptID := ctx.Positionals[0]

	format := "lines"
	if v := ctx.Flags["format"]; len(v) > 0 {
		format = v[0]
	}
	wantSrcText := len(ctx.Flags["src-text"]) > 0

	// Resolve graph file.
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

	// Build concept membership sets for fast lookup.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	allConceptSet := make(map[string]bool, len(g.PassedConcepts)+len(g.UntestedConcepts))
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

	// §7: refuse when any parent of the concept is outside passed.
	// Walk edges to find the first non-passed concept parent (in file order).
	for _, e := range g.Edges {
		if e.To == conceptID && allConceptSet[e.From] && !passedSet[e.From] {
			ctx.ErrMsg = fmt.Sprintf("parent %s is not passed", e.From)
			ctx.FixMsg = fmt.Sprintf("pass %s first", e.From)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
	}

	// §7: refuse when concept is gated.
	cs := s.ConceptStatus(conceptID)
	if cs.Gated {
		ctx.ErrMsg = fmt.Sprintf("%s is gated", conceptID)
		ctx.FixMsg = fmt.Sprintf("add a prerequisite concept or reopen a parent of %s", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	// Collect all batches for this concept (sorted by N ascending) by scanning
	// TestingItems. We avoid touching unexported state fields.
	batches := askConceptBatches(g, s, conceptID)

	// §8 batch selection:
	// 1. Unresolved teach batch (lowest N first).
	// 2. Otherwise, unanswered probe batch (checking refusals first).
	selectedBatch := ""
	for _, b := range batches {
		if graph.IsTeachClass(b) && s.BatchStateOf(b) != state.BatchResolved {
			selectedBatch = b
			break
		}
	}

	if selectedBatch == "" {
		// No unresolved teach batch — try probe selection.
		// §7: refuse when open targets remain and teaching is not spent.
		// This fires when all teach batches are resolved but the latest had
		// in-scope fails and there are still failed probes with no passing teach.
		if len(cs.OpenTargets) > 0 && !cs.TeachingSpent {
			openTarget := cs.OpenTargets[0]
			ctx.ErrMsg = fmt.Sprintf("teaching round for %s is not complete", conceptID)
			ctx.FixMsg = fmt.Sprintf("add a teach question for %s with tm q %s ... --teach --re %s",
				openTarget, conceptID, openTarget)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}

		// Find the lowest-N probe batch that is Draft (not locked) or Open.
		for _, b := range batches {
			if !graph.IsProbeClass(b) {
				continue
			}
			status := s.BatchStateOf(b)
			if status == state.BatchLocked || status == state.BatchResolved {
				continue
			}
			selectedBatch = b
			break
		}
	}

	// Nothing to ask — no suitable batch found; exit 0 with no output.
	if selectedBatch == "" {
		if format == "json" {
			out, _ := json.Marshal(askJSONResponse{Batch: "", Questions: []askJSONQuestion{}})
			_, _ = fmt.Fprintf(ctx.Out, "%s\n", out)
		}
		return 0
	}

	// §7: refuse when the selected batch is under its minimum.
	// §8.5: replacement batches (all questions incoming from answers, not concepts)
	// are exempt from ProbeMin.
	qCount := askBatchQuestionCount(g, selectedBatch)
	if graph.IsProbeClass(selectedBatch) && !askBatchIsReplacement(g, selectedBatch) && qCount < cfg.ProbeMin {
		ctx.ErrMsg = fmt.Sprintf("batch %s has too few questions (need at least %d)", selectedBatch, cfg.ProbeMin)
		ctx.FixMsg = fmt.Sprintf("add more questions with tm q %s", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}
	if graph.IsTeachClass(selectedBatch) && qCount < cfg.TeachMin {
		ctx.ErrMsg = fmt.Sprintf("batch %s has too few questions (need at least %d)", selectedBatch, cfg.TeachMin)
		ctx.FixMsg = fmt.Sprintf("add more questions with tm q %s --teach", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	// Collect unanswered questions (no answer node) in the selected batch.
	// Build an answer-by-qid map for fast lookup.
	answerByQID := make(map[string]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.A == nil {
			continue
		}
		n := graph.QuestionN(item.A.ID)
		if n == 0 {
			continue
		}
		answerByQID[fmt.Sprintf("q%d", n)] = item.A
	}

	var unanswered []*graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q == nil || item.Q.Class != selectedBatch {
			continue
		}
		if _, hasAnswer := answerByQID[item.Q.ID]; !hasAnswer {
			unanswered = append(unanswered, item.Q)
		}
	}

	srcRoot := s.Cfg().SrcRoot

	if format == "json" {
		return askEmitJSON(ctx, selectedBatch, unanswered, s, srcRoot, wantSrcText)
	}
	return askEmitLines(ctx, selectedBatch, unanswered, s, srcRoot, wantSrcText)
}

// askEmitLines emits the lines-format output for tm ask.
func askEmitLines(ctx *Context, batch string, questions []*graph.QuestionNode, s *state.State, srcRoot string, wantSrcText bool) int {
	_, _ = fmt.Fprintln(ctx.Out, batch)
	for _, q := range questions {
		line := q.ID + " | " + q.Scope + " | " + q.Cite
		if graph.IsTeachClass(q.Class) {
			if targetQID, ok := s.TeachingTarget(q.ID); ok {
				line += " | re " + targetQID
			}
		}
		_, _ = fmt.Fprintln(ctx.Out, line)
		if wantSrcText {
			cit, citErr := cite.Parse(q.Cite)
			if citErr == nil {
				text, readErr := cite.ReadRange(cit, srcRoot)
				if readErr == nil {
					for _, l := range strings.Split(text, "\n") {
						_, _ = fmt.Fprintf(ctx.Out, "  %s\n", l)
					}
				}
			}
		}
	}
	return 0
}

// askJSONQuestion is the per-question object in the JSON output.
type askJSONQuestion struct {
	ID      string `json:"id"`
	Scope   string `json:"scope"`
	Cite    string `json:"cite"`
	Re      string `json:"re,omitempty"`
	SrcText string `json:"src_text,omitempty"`
}

// askJSONResponse is the top-level JSON output object.
type askJSONResponse struct {
	Batch     string            `json:"batch"`
	Questions []askJSONQuestion `json:"questions"`
}

// askEmitJSON emits the JSON-format output for tm ask.
func askEmitJSON(ctx *Context, batch string, questions []*graph.QuestionNode, s *state.State, srcRoot string, wantSrcText bool) int {
	qs := make([]askJSONQuestion, 0, len(questions))
	for _, q := range questions {
		jq := askJSONQuestion{
			ID:    q.ID,
			Scope: q.Scope,
			Cite:  q.Cite,
		}
		if graph.IsTeachClass(q.Class) {
			if targetQID, ok := s.TeachingTarget(q.ID); ok {
				jq.Re = targetQID
			}
		}
		if wantSrcText {
			cit, citErr := cite.Parse(q.Cite)
			if citErr == nil {
				text, readErr := cite.ReadRange(cit, srcRoot)
				if readErr == nil {
					jq.SrcText = text
				}
			}
		}
		qs = append(qs, jq)
	}
	resp := askJSONResponse{Batch: batch, Questions: qs}
	out, _ := json.Marshal(resp)
	_, _ = fmt.Fprintf(ctx.Out, "%s\n", out)
	return 0
}

// askConceptBatches returns the batch class IDs for conceptID in ascending N
// order by scanning g.TestingItems and querying s.ConceptOf for each question.
func askConceptBatches(g *graph.Graph, s *state.State, conceptID string) []string {
	seen := make(map[string]bool)
	var batches []string
	for _, item := range g.TestingItems {
		if item.Q == nil {
			continue
		}
		q := item.Q
		if !graph.ValidBatchID(q.Class) || seen[q.Class] {
			continue
		}
		if c, ok := s.ConceptOf(q.ID); ok && c == conceptID {
			seen[q.Class] = true
			batches = append(batches, q.Class)
		}
	}
	sort.Slice(batches, func(i, j int) bool {
		return graph.BatchN(batches[i]) < graph.BatchN(batches[j])
	})
	return batches
}

// askBatchQuestionCount counts the number of questions with the given batch class.
func askBatchQuestionCount(g *graph.Graph, batchClass string) int {
	n := 0
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.Class == batchClass {
			n++
		}
	}
	return n
}

// askBatchIsReplacement reports whether batchClass is a §8.5 replacement probe
// batch. A replacement batch has every question's single incoming edge coming
// from an answer node (unclear answer being replaced), not from a concept.
// Replacement batches are exempt from ProbeMin/ProbeMax.
func askBatchIsReplacement(g *graph.Graph, batchClass string) bool {
	// Build an answer-ID set for fast membership tests.
	answerIDs := make(map[string]bool)
	for _, item := range g.TestingItems {
		if item.A != nil {
			answerIDs[item.A.ID] = true
		}
	}
	// Build a map from node ID to the ID of its single incoming node.
	inFrom := make(map[string]string)
	for _, e := range g.Edges {
		inFrom[e.To] = e.From
	}
	found := false
	for _, item := range g.TestingItems {
		if item.Q == nil || item.Q.Class != batchClass {
			continue
		}
		found = true
		src, ok := inFrom[item.Q.ID]
		if !ok || !answerIDs[src] {
			return false // at least one question whose source is not an answer
		}
	}
	return found // true only if we saw at least one question and all came from answers
}
