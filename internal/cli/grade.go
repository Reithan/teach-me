package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// gradeRun is the Run handler for:
//
//   - `tm grade <qid> pass|fail|unclear "<summary>" [--guided] [--oos]`
//   - `tm grade --drift <concept> keep|reopen "<summary>"`
//
// Without --drift: implements the §8 grade procedure (steps 1–10).
// With --drift: implements the §9.1 recheck verdict (keep or reopen).
//
// ForbidTeacher: true is enforced by the dispatcher (run.go checkRole).
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal
//	2  output fails lint (internal engine error)
//	3  usage error or unknown id
func gradeRun(ctx *Context) int {
	// Route to drift-grade handler when --drift is supplied.
	if len(ctx.Flags["drift"]) > 0 {
		return gradeDriftRun(ctx)
	}

	qid := ctx.Positionals[0]
	verdict := ctx.Positionals[1]
	summary := ctx.Positionals[2]

	guided := len(ctx.Flags["guided"]) > 0
	oos := len(ctx.Flags["oos"]) > 0

	usageLine := FindCommand("grade").Usage()

	// Runtime enum check: without --drift, only pass|fail|unclear are valid.
	if verdict != "pass" && verdict != "fail" && verdict != "unclear" {
		ctx.ErrMsg = fmt.Sprintf("verdict must be pass, fail, or unclear, got %q", verdict)
		ctx.FixMsg = usageLine
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Find the question node.
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

		// §7 line 266: refuse when the question has no pending answer.
		an := s.AnswerFor(qid)
		if an == nil || an.Class != "pending" {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s has no pending answer", qid),
				Fix:  fmt.Sprintf("run tm answer %s \"<answer>\" first", qid),
				Exit: 1,
			}
		}

		// §7 line 267: --oos is only valid for teach questions.
		if oos && graph.IsProbeClass(qn.Class) {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("--oos is not valid for probe question %s", qid),
				Fix:  "--oos is only for teach questions",
				Exit: 1,
			}
		}

		// Capture raw answer text before overwriting (step 1 log requirement).
		raw := an.Label

		// src_text: cited source text of the question per §10 grade event.
		srcText := ""
		srcRoot := s.Cfg().SrcRoot
		if cit, citErr := cite.Parse(qn.Cite); citErr == nil {
			if text, readErr := cite.ReadRange(cit, srcRoot); readErr == nil {
				srcText = text
			}
		}

		// Step 2 (§8.2, Q5): if verdict is unclear and the root probe reached by
		// walking the incoming-edge chain has an unclear answer, record "fail"
		// instead. The event preserves the original verdict and adds recorded.
		recordedClass := verdict
		if verdict == "unclear" && s.RootProbeUnclear(qid) {
			recordedClass = "fail"
		}

		// Resolve batch and concept.
		batchClass, ok := s.BatchOf(qid)
		if !ok {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot resolve batch for %s", qid),
				Exit: 1,
			}
		}
		conceptID, ok := s.ConceptOf(qid)
		if !ok {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot resolve concept for %s", qid),
				Exit: 1,
			}
		}

		// Step 1: build the updated answer node (copy-on-write).
		// Step 7: set OOS when --oos is present (teach questions only, per §8.7).
		newAN := *an
		newAN.Class = recordedClass
		newAN.Label = summary
		if oos {
			newAN.OOS = true
		}

		// Grade event (§10).
		gradeRow := eventlog.NewRow("grade", map[string]any{
			"q":        qid,
			"verdict":  verdict,
			"recorded": recordedClass,
			"summary":  summary,
			"raw":      raw,
			"src_text": srcText,
			"guided":   guided,
			"oos":      oos,
		})

		// Build modified graph with the updated answer.
		newG := gradeReplaceAnswer(g, &newAN)

		// Step 3: check whether the batch is complete after this grade.
		// Complete means every question in the batch (including the one being
		// graded now) has a non-pending, non-absent answer.
		batchComplete := true
		for _, q := range s.BatchQuestions(batchClass) {
			if q.ID == qid {
				continue // this question is being graded right now
			}
			a := s.AnswerFor(q.ID)
			if a == nil || a.Class == "pending" {
				batchComplete = false
				break
			}
		}
		if !batchComplete {
			// Step 3 stop: batch has unanswered or still-pending questions.
			return newG, []eventlog.Row{gradeRow}, nil
		}

		// Step 4 (§8.4): probe batch, all answers pass → run the pass procedure.
		// Steps 5, 6, 8, 9, 10 produce no structural writes; the derived state
		// (§5) handles what ask/answer report next.
		if graph.IsProbeClass(batchClass) {
			allPass := true
			for _, q := range s.BatchQuestions(batchClass) {
				var class string
				if q.ID == qid {
					class = recordedClass // use the class we're assigning
				} else {
					class = s.AnswerFor(q.ID).Class
				}
				if class != "pass" {
					allPass = false
					break
				}
			}

			if allPass {
				// UnblockedBy uses the pre-pass state (conceptID still untested).
				// Normalize nil to a non-nil empty slice so the pass event's
				// unblocked field serializes as [] (matching the add event's
				// children/parents convention) rather than null.
				unblocked := s.UnblockedBy(conceptID)
				if unblocked == nil {
					unblocked = []string{}
				}

				rst, newG2 := ops.RemoveTestingSubtree(newG, s, conceptID)
				newG3 := ops.MoveToPassed(newG2, conceptID)

				gcRow := gradeGCRow(rst, "pass")
				passRow := eventlog.NewRow("pass", map[string]any{
					"concept":   conceptID,
					"batches":   rst.Batches,
					"unblocked": unblocked,
				})

				return newG3, []eventlog.Row{gradeRow, gcRow, passRow}, nil
			}
		}

		// Steps 5/6 (probe, no all-pass) or steps 8/9/10 (teach): no structural
		// writes. Derived state will reflect the new answer class.
		return newG, []eventlog.Row{gradeRow}, nil
	}

	return runMutation(ctx, apply)
}

// gradeReplaceAnswer returns a copy-on-write graph with newAN replacing the
// answer that shares newAN.ID. The input graph is not mutated.
func gradeReplaceAnswer(g *graph.Graph, newAN *graph.AnswerNode) *graph.Graph {
	newG := *g
	items := make([]graph.TestingItem, len(g.TestingItems))
	for i, item := range g.TestingItems {
		if item.A != nil && item.A.ID == newAN.ID {
			items[i] = graph.TestingItem{A: newAN}
		} else {
			items[i] = item
		}
	}
	newG.TestingItems = items
	return &newG
}

// gradeGCRow constructs the gc event row (§10) from a RemovedSubtree.
// nodes: question nodes use Scope as label (batch class as class);
// answer nodes use Label (answer text) and their grading class.
func gradeGCRow(rst ops.RemovedSubtree, reason string) eventlog.Row {
	nodes := make([]map[string]any, len(rst.Nodes))
	for i, n := range rst.Nodes {
		nodes[i] = map[string]any{
			"id":    n.ID,
			"label": n.Label,
			"class": n.Class,
		}
	}
	edges := make([]map[string]any, len(rst.Edges))
	for i, e := range rst.Edges {
		edges[i] = map[string]any{
			"from": e.From,
			"to":   e.To,
		}
	}
	meta := make([]map[string]any, len(rst.Meta))
	for i, m := range rst.Meta {
		meta[i] = map[string]any{
			"concept": m.Concept,
			"base":    m.Base,
		}
	}
	return eventlog.NewRow("gc", map[string]any{
		"concept": rst.Concept,
		"reason":  reason,
		"nodes":   nodes,
		"edges":   edges,
		"meta":    meta,
	})
}
