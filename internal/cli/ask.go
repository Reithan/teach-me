package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
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
	overrideReason := ""
	if v := ctx.Flags["override"]; len(v) > 0 {
		overrideReason = v[0]
	}

	s, file, code := loadStateCtx(ctx)
	if code != 0 {
		return code
	}

	cfg := s.Cfg()
	g := s.Graph()

	ns := graphNodeSets(g, false)

	// Unknown concept → exit 3.
	if !ns.AllConcepts[conceptID] {
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", conceptID)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// §7: refuse when any parent of the concept is outside passed.
	// Walk edges to find the first non-passed concept parent (in file order).
	for _, e := range g.Edges {
		if e.To == conceptID && ns.UntestedSet[e.From] {
			ctx.ErrMsg = fmt.Sprintf("parent %s is not passed", e.From)
			ctx.FixMsg = fmt.Sprintf("pass %s first", e.From)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
	}

	// §7: refuse when concept is gated; --override clears gate and proceeds
	// (ask becomes a mutation under --override only; dec#6 preserved otherwise).
	cs := s.ConceptStatus(conceptID)
	if cs.Gated {
		if overrideReason == "" {
			ctx.ErrMsg = fmt.Sprintf("%s is gated", conceptID)
			ctx.FixMsg = buildGateFixMsg(conceptID, s)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
		// Route through lock+mutation engine to write gate line + gate event.
		gateApply := func(innerG *graph.Graph, innerS *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
			innerCS := innerS.ConceptStatus(conceptID)
			if !innerCS.Gated {
				// No longer gated between check and lock (race-safe); no-op.
				return innerG, nil, nil
			}
			newG, gateRow, ok := ops.ClearGate(innerG, innerS, conceptID, "override", overrideReason)
			if !ok {
				return innerG, nil, nil
			}
			return newG, []eventlog.Row{gateRow}, nil
		}
		askLintCfg := buildLintConfig(file)
		_, gateRefusal, gateEngErr := ops.Mutate(file, cfg, askLintCfg, errlog.RealClock, gateApply)
		if gateEngErr != nil {
			ctx.ErrMsg = fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), gateEngErr)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		if gateRefusal != nil {
			ctx.ErrMsg = gateRefusal.Err
			ctx.FixMsg = gateRefusal.Fix
			writeErrFix(ctx.ErrOut, gateRefusal.Err, gateRefusal.Fix)
			return gateRefusal.Exit
		}
		// Reload state from the updated graph file and rebuild derived values.
		var loadErr error
		s, loadErr = state.Load(file, cfg)
		if loadErr != nil {
			ctx.ErrMsg = fmt.Sprintf("cannot load %s: %v", file, loadErr)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		g = s.Graph()
		ns = graphNodeSets(g, false)
		cs = s.ConceptStatus(conceptID)
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
		// §7: refuse when failed probe batches remain above base, teaching is
		// not spent, and the latest teach batch above base is not all-pass (§8.6–8.9).
		// Uses LatestTeachNotAllPass rather than OpenTargets to key on the latest
		// batch status, not on a stale per-target accounting (Fix 2).
		// Exempt: when a failed probe batch is still open, ask emits its remaining
		// question(s) so the batch can close normally (issue #78; openFailedProbeBatch).
		if len(cs.FailedProbeBatches) > 0 && !cs.TeachingSpent && cs.LatestTeachNotAllPass &&
			openFailedProbeBatch(cs, s) == "" {
			var openTarget string
			if len(cs.OpenTargets) > 0 {
				openTarget = cs.OpenTargets[0]
			}
			ctx.ErrMsg = fmt.Sprintf("teaching round for %s is not complete", conceptID)
			if openTarget != "" {
				ctx.FixMsg = fmt.Sprintf("add a teach question for %s with tm q %s ... --teach --re %s",
					openTarget, conceptID, openTarget)
			}
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
	// Replacement batches (§8.5) are min-exempt per lint §11.9 (decision #7).
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
			// DRIFT <cite> — printed when stored hash no longer matches file content.
			if drifted, driftErr := checkCiteDrift(q.Cite, srcRoot); driftErr == nil && drifted {
				_, _ = fmt.Fprintf(ctx.Out, "DRIFT %s\n", q.Cite)
			}
			if text, readErr := readCiteText(q.Cite, srcRoot); readErr == nil {
				for _, l := range strings.Split(text, "\n") {
					_, _ = fmt.Fprintf(ctx.Out, "  %s\n", l)
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
			if text, readErr := readCiteText(q.Cite, srcRoot); readErr == nil {
				jq.SrcText = text
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

// emitAskForConcept runs §8 ask selection and emits lines-format output for
// conceptID to out. Returns (0, "", "") when output is emitted (or nothing to
// ask), (1, errMsg, fixMsg) on invariant refusal, (3, errMsg, "") on unknown
// concept. Does NOT perform file resolution or write to errOut.
//
// This is the shared emitter used by statusConcept to chain tm ask output.
func emitAskForConcept(out io.Writer, s *state.State, conceptID string) (code int, errMsg, fixMsg string) {
	g := s.Graph()
	cfg := s.Cfg()

	ns := graphNodeSets(g, false)

	if !ns.AllConcepts[conceptID] {
		return 3, fmt.Sprintf("unknown concept %q", conceptID), ""
	}
	for _, e := range g.Edges {
		if e.To == conceptID && ns.UntestedSet[e.From] {
			return 1, fmt.Sprintf("parent %s is not passed", e.From),
				fmt.Sprintf("pass %s first", e.From)
		}
	}
	cs := s.ConceptStatus(conceptID)
	if cs.Gated {
		return 1, fmt.Sprintf("%s is gated", conceptID),
			buildGateFixMsg(conceptID, s)
	}
	batches := askConceptBatches(g, s, conceptID)
	selectedBatch := ""
	for _, b := range batches {
		if graph.IsTeachClass(b) && s.BatchStateOf(b) != state.BatchResolved {
			selectedBatch = b
			break
		}
	}
	if selectedBatch == "" {
		if len(cs.OpenTargets) > 0 && !cs.TeachingSpent {
			openTarget := cs.OpenTargets[0]
			return 1,
				fmt.Sprintf("teaching round for %s is not complete", conceptID),
				fmt.Sprintf("add a teach question for %s with tm q %s ... --teach --re %s",
					openTarget, conceptID, openTarget)
		}
		for _, b := range batches {
			if !graph.IsProbeClass(b) {
				continue
			}
			bst := s.BatchStateOf(b)
			if bst == state.BatchLocked || bst == state.BatchResolved {
				continue
			}
			selectedBatch = b
			break
		}
	}
	if selectedBatch == "" {
		return 0, "", ""
	}
	qCount := askBatchQuestionCount(g, selectedBatch)
	// Replacement batches (§8.5) are min-exempt per lint §11.9 (decision #7).
	if graph.IsProbeClass(selectedBatch) && !askBatchIsReplacement(g, selectedBatch) && qCount < cfg.ProbeMin {
		return 1,
			fmt.Sprintf("batch %s has too few questions (need at least %d)", selectedBatch, cfg.ProbeMin),
			fmt.Sprintf("add more questions with tm q %s", conceptID)
	}
	if graph.IsTeachClass(selectedBatch) && qCount < cfg.TeachMin {
		return 1,
			fmt.Sprintf("batch %s has too few questions (need at least %d)", selectedBatch, cfg.TeachMin),
			fmt.Sprintf("add more questions with tm q %s --teach", conceptID)
	}
	answerByQID := buildAnswerMap(g)
	var unanswered []*graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q == nil || item.Q.Class != selectedBatch {
			continue
		}
		if _, hasAnswer := answerByQID[item.Q.ID]; !hasAnswer {
			unanswered = append(unanswered, item.Q)
		}
	}
	if len(unanswered) == 0 {
		return 0, "", ""
	}
	_, _ = fmt.Fprintln(out, selectedBatch)
	for _, q := range unanswered {
		line := q.ID + " | " + q.Scope + " | " + q.Cite
		if graph.IsTeachClass(q.Class) {
			if targetQID, ok := s.TeachingTarget(q.ID); ok {
				line += " | re " + targetQID
			}
		}
		_, _ = fmt.Fprintln(out, line)
	}
	return 0, "", ""
}
