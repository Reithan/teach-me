package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// answerRun is the Run handler for:
//
//	tm answer <qid> "<raw>" [--asked "<wording>"] [--override "<reason>"]
//
// Records the human's raw answer as a pending answer node a<N> (where N
// matches the question's N) with class "pending". The answer is stored
// verbatim; the writer escapes it. --asked stores the teacher's wording in
// AnswerNode.Asked. --override is gate-clearing wired in m6e; accepted but
// ignored here.
//
// The answer is answerable iff its question is in the batch that `tm ask
// <concept>` would currently emit. This mirrors ask.go's batch-answerability
// ladder (shared logic noted inline).
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (see §7)
//	2  output fails lint (internal error, handled by engine)
//	3  usage error, unknown ID
func answerRun(ctx *Context) int {
	qid := ctx.Positionals[0]
	rawAnswer := ctx.Positionals[1]

	askedWording := ""
	if v := ctx.Flags["asked"]; len(v) > 0 {
		askedWording = v[0]
	}

	overrideReason := ""
	if v := ctx.Flags["override"]; len(v) > 0 {
		overrideReason = v[0]
	}

	usageLine := FindCommand("answer").Usage()

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		cfg := s.Cfg()

		// Build concept membership sets.
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

		// Exit 3: unknown qid.
		var qn *graph.QuestionNode
		for _, item := range g.TestingItems {
			if item.Q != nil && item.Q.ID == qid {
				qn = item.Q
				break
			}
		}
		if qn == nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown question %q", qid),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Exit 1: question already has an answer (§7 line 265).
		if s.AnswerFor(qid) != nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s already has an answer", qid),
				Fix:  fmt.Sprintf("run tm check %s to grade it", qid),
				Exit: 1,
			}
		}

		// Resolve concept and batch.
		conceptID, ok := s.ConceptOf(qid)
		if !ok {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot resolve concept for %s", qid),
				Exit: 1,
			}
		}
		batchClass, ok := s.BatchOf(qid)
		if !ok {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot resolve batch for %s", qid),
				Exit: 1,
			}
		}

		// Exit 1: any parent of the concept is outside passed (§7, shared with ask).
		for _, e := range g.Edges {
			if e.To == conceptID && allConceptSet[e.From] && !passedSet[e.From] {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("parent %s is not passed", e.From),
					Fix:  fmt.Sprintf("pass %s first", e.From),
					Exit: 1,
				}
			}
		}

		// Exit 1: concept is gated (§7); --override clears gate FIRST then
		// evaluates remaining refusals against post-clear state (Fix 3, dec#9).
		//
		// Per owner ruling: if a subsequent refusal fires, the gate-clear row must
		// NOT be persisted. The gate-clear is part of this same (not-yet-committed)
		// transaction; returning a non-nil refusal causes the engine to discard the
		// whole mutation and write nothing.
		cs := s.ConceptStatus(conceptID)
		var gateRow *eventlog.Row
		if cs.Gated {
			if overrideReason == "" {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("%s is gated", conceptID),
					Fix:  fmt.Sprintf("add a prerequisite concept or reopen a parent of %s", conceptID),
					Exit: 1,
				}
			}
			// Clear gate first, reload state, recompute concept status.
			if clearedG, gr, cleared := ops.ClearGate(g, s, conceptID, "override", overrideReason); cleared {
				g = clearedG
				s = state.LoadFromGraph(g, cfg)
				cs = s.ConceptStatus(conceptID)
				rowCopy := gr
				gateRow = &rowCopy
			}
			// If !cleared: gate resolved between check and lock (race-safe no-op).
		}

		allBatches := s.ConceptBatches(conceptID)

		// The remaining refusals depend on whether the question is in a probe
		// or teach batch. These predicates mirror ask.go's batch-selection logic
		// (see askRun / emitAskForConcept) so that answer and ask agree on what
		// is answerable. They are evaluated against the post-clear state.

		if graph.IsProbeClass(batchClass) {
			// Exit 1: batch is fallback probes and a teach batch is unresolved (§7).
			isFallback := false
			for _, fp := range cs.FallbackProbes {
				if fp == batchClass {
					isFallback = true
					break
				}
			}
			if isFallback {
				for _, b := range allBatches {
					if graph.IsTeachClass(b) && s.BatchStateOf(b) != state.BatchResolved {
						return nil, nil, &ops.Refusal{
							Err:  fmt.Sprintf("teach batch %s is not resolved for %s", b, conceptID),
							Fix:  fmt.Sprintf("finish answering %s before answering fallback probes", b),
							Exit: 1,
						}
					}
				}
			}

			// Exit 1: latest teach batch not all pass and teaching not spent (§7,
			// §8.6–8.9). Mirrors the condition in ask.go (Fix 2).
			if len(cs.FailedProbeBatches) > 0 && !cs.TeachingSpent && cs.LatestTeachNotAllPass {
				var openTarget string
				if len(cs.OpenTargets) > 0 {
					openTarget = cs.OpenTargets[0]
				}
				ref := &ops.Refusal{
					Err:  fmt.Sprintf("teaching round for %s is not complete", conceptID),
					Exit: 1,
				}
				if openTarget != "" {
					ref.Fix = fmt.Sprintf("add a teach question for %s with tm q %s ... --teach --re %s",
						openTarget, conceptID, openTarget)
				}
				return nil, nil, ref
			}

			// Exit 1: min count check, with replacement batch exemption (§8.5; lint §11.9; decision #7).
			// A replacement batch (containing ≥1 replacement probe) is min-exempt.
			qCount := len(s.BatchQuestions(batchClass))
			if qCount < cfg.ProbeMin && !answerBatchIsReplacement(g, batchClass) {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("batch %s has too few questions (need at least %d)", batchClass, cfg.ProbeMin),
					Fix:  fmt.Sprintf("add more questions with tm q %s", conceptID),
					Exit: 1,
				}
			}
		} else {
			// Teach batch.

			// Exit 1: an earlier unresolved teach batch exists (ask would select it).
			batchN := graph.BatchN(batchClass)
			for _, b := range allBatches {
				if graph.IsTeachClass(b) && graph.BatchN(b) < batchN && s.BatchStateOf(b) != state.BatchResolved {
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("teach batch %s is not resolved; answer it before %s", b, batchClass),
						Fix:  fmt.Sprintf("run tm ask %s and answer the questions in %s first", conceptID, b),
						Exit: 1,
					}
				}
			}

			// Exit 1: min count check for teach batch (§7).
			qCount := len(s.BatchQuestions(batchClass))
			if qCount < cfg.TeachMin {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("batch %s has too few questions (need at least %d)", batchClass, cfg.TeachMin),
					Fix:  fmt.Sprintf("add more questions with tm q %s --teach", conceptID),
					Exit: 1,
				}
			}
		}

		// Allocate answer ID: a<N> shares the question's N.
		qN := graph.QuestionN(qid)
		aid := fmt.Sprintf("a%d", qN)

		// Build new graph (copy-on-write) on top of g (= clearedG when gate was
		// cleared, original g otherwise).
		newG := *g
		newG.TestingItems = append(append([]graph.TestingItem{}, g.TestingItems...), graph.TestingItem{
			A: &graph.AnswerNode{
				ID:    aid,
				Class: "pending",
				Asked: askedWording,
				Label: rawAnswer,
			},
		})
		newG.Edges = append(append([]*graph.Edge{}, g.Edges...), &graph.Edge{
			From: qid,
			To:   aid,
		})

		answerRow := eventlog.NewRow("answer", map[string]any{
			"q":     qid,
			"raw":   rawAnswer,
			"asked": askedWording,
		})
		// Gate row (if any) must precede the answer row in the event log (spec §7,
		// dec#9; mirrors the ordering in ask.go's --override path).
		var rows []eventlog.Row
		if gateRow != nil {
			rows = append(rows, *gateRow)
		}
		rows = append(rows, answerRow)

		return &newG, rows, nil
	}

	return runMutation(ctx, apply)
}

// answerBatchIsReplacement reports whether the probe batch batchClass contains
// at least one replacement probe — a probe whose single incoming edge comes
// from an unclear answer node. Replacement batches are min-exempt per §8.5 / lint §11.9 (decision #7).
func answerBatchIsReplacement(g *graph.Graph, batchClass string) bool {
	// Build answer-by-ID index from graph.
	aByID := make(map[string]*graph.AnswerNode)
	for _, item := range g.TestingItems {
		if item.A != nil {
			aByID[item.A.ID] = item.A
		}
	}
	// Build set of question IDs in this batch.
	batchQIDs := make(map[string]bool)
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.Class == batchClass {
			batchQIDs[item.Q.ID] = true
		}
	}
	// A question is a replacement probe if its incoming edge is from an unclear answer.
	for _, e := range g.Edges {
		if !batchQIDs[e.To] {
			continue
		}
		if a, ok := aByID[e.From]; ok && a.Class == "unclear" {
			return true
		}
	}
	return false
}
