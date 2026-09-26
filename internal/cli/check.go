package cli

import (
	"fmt"
	"strings"

	"github.com/reithan/teach-me/internal/graph"
)

// checkRun is the Run handler for:
//
//   - `tm check <qid>` — emits the grading payload (§9)
//   - `tm check --drift <concept>` — emits the drift recheck payload (§9.1)
//   - `tm check --errata <concept>` — emits the errata recheck payload (§9.1)
//
// Exit codes:
//
//	0  payload emitted
//	1  invariant refusal: no pending answer (§7 line 266); or citation drifted (§7 line 299)
//	3  usage / load error: unknown question ID or file problem
func checkRun(ctx *Context) int {
	// Route to drift-check handler when --drift is supplied.
	if len(ctx.Flags["drift"]) > 0 {
		return checkDriftRun(ctx)
	}
	// Route to errata-check handler when --errata is supplied.
	if len(ctx.Flags["errata"]) > 0 {
		return checkErrataRun(ctx)
	}

	qid := ctx.Positionals[0]

	// Resolve graph file (global --file > $TM_FILE > .tmconfig per §3).
	s, _, code := loadStateCtx(ctx)
	if code != 0 {
		return code
	}

	g := s.Graph()

	// Find the question node.
	var qn *graph.QuestionNode
	for _, item := range g.TestingItems {
		if item.Q != nil && item.Q.ID == qid {
			qn = item.Q
			break
		}
	}
	if qn == nil {
		ctx.ErrMsg = fmt.Sprintf("unknown question %q", qid)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Find the answer node (aN where N matches qN).
	n := graph.QuestionN(qid)
	answerID := fmt.Sprintf("a%d", n)
	var an *graph.AnswerNode
	for _, item := range g.TestingItems {
		if item.A != nil && item.A.ID == answerID {
			an = item.A
			break
		}
	}

	// §7 line 299: refuse when the question's citation has drifted.
	srcRoot := s.Cfg().SrcRoot
	if drifted, driftErr := checkCiteDrift(qn.Cite, srcRoot); driftErr == nil && drifted {
		ctx.ErrMsg = fmt.Sprintf("%s citation has drifted", qid)
		ctx.FixMsg = fmt.Sprintf("tm drop %s, then tm q --re %s <cite>", qid, qid)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	// §7 line 266: refuse when the question has no pending answer.
	if an == nil || an.Class != "pending" {
		ctx.ErrMsg = fmt.Sprintf("%s has no pending answer", qid)
		ctx.FixMsg = fmt.Sprintf("tm answer %s \"<raw>\"", qid)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	isTeach := graph.IsTeachClass(qn.Class)

	// CONCEPT: the concept the question hangs under (§9).
	var concept *checkConceptData
	conceptID, hasConcept := s.ConceptOf(qid)
	if hasConcept {
		if c := findConceptNode(g, conceptID); c != nil {
			concept = &checkConceptData{ID: c.ID, Scope: c.Scope}
		}
	}

	// SRC lines: pre-split, error folded in as a single line.
	var srcLines []string
	if lines, readErr := readCiteText(qn.Cite, srcRoot); readErr == nil {
		srcLines = strings.Split(lines, "\n")
	} else {
		// Keep going; grader needs to know citation is unreadable.
		srcLines = []string{fmt.Sprintf("[citation unreadable: %v]", readErr)}
	}

	// TARGET and GAP — teach questions only, per §9 lines 325-332.
	var target *checkTargetData
	var gap string
	if isTeach {
		if targetQID, ok := s.TeachingTarget(qid); ok {
			for _, item := range g.TestingItems {
				if item.Q != nil && item.Q.ID == targetQID {
					target = &checkTargetData{QID: targetQID, Scope: item.Q.Scope, Cite: item.Q.Cite}
					break
				}
			}
		}
		if hasConcept {
			for _, c := range g.UntestedConcepts {
				if c.ID == conceptID && c.GAP != "" {
					gap = c.GAP
					break
				}
			}
		}
	}

	data := checkData{
		Concept:  concept,
		QID:      qid,
		Question: qn.Scope,
		Asked:    an.Asked,
		Cite:     qn.Cite,
		SrcLines: srcLines,
		IsTeach:  isTeach,
		Target:   target,
		GAP:      gap,
		Answer:   an.Label,
	}
	_ = renderPrompt(ctx.Out, "check.txt", data)
	return 0
}
