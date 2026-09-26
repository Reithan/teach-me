package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// qRun is the Run handler for:
//
//	tm q <concept> <cite> "<narrow scope>" [--re <qid>] [--teach --re <qid>]
//
// Creates a new question node q<N> with exactly one incoming edge.
//
//   - Probe (no --teach, no --re): class probe_<B>, edge concept → q<N>.
//     B is the concept's draft probe batch; opens probe_<NextBatchN> if none.
//   - Probe with --re <uq>: class probe_<B>, edge a<N(uq)> → q<N>.
//     Same batch allocation. <uq> must be a probe whose answer graded unclear.
//   - Teach (--teach --re <t>): class teach_<B>, edge a<N(t)> → q<N>.
//     B is the concept's draft teach batch; opens teach_<NextBatchN> if none.
//     <t> must have a fail or unclear answer that is not flagged OOS.
//
// --override PATH is gate-clearing wired in m6e; ignored here.
//
// Prints the new question ID on success (e.g. "q7").
//
// Exit codes:
//
//	0  ok; prints qN
//	1  invariant refusal
//	2  output fails lint (internal error)
//	3  usage error, unknown ID, bad citation
func qRun(ctx *Context) int {
	conceptID := ctx.Positionals[0]
	citeStr := ctx.Positionals[1]
	scope := ctx.Positionals[2]

	reQID := ""
	if v := ctx.Flags["re"]; len(v) > 0 {
		reQID = v[0]
	}
	isTeach := len(ctx.Flags["teach"]) > 0

	overrideReason := ""
	if v := ctx.Flags["override"]; len(v) > 0 {
		overrideReason = v[0]
	}

	usageLine := FindCommand("q").Usage()

	// ── Pre-Mutate validation (exit 3) ────────────────────────────────────────

	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	// Hash the citation: resolve, compute SHA-256 prefix, return hashed form.
	resolver, resolverErr := source.NewResolver(filepath.Dir(file))
	if resolverErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	hashedCite, citeMeta, hashErr := resolver.HashCitation(citeStr)
	if hashErr != nil {
		return citeHashError(ctx, citeStr, aidRefusalWithID(hashErr, conceptID), usageLine)
	}
	citeStr = hashedCite

	// §7 question citation cap: a single question may cite at most 120 lines or
	// 6,000 characters. Enforced here — after the citation text resolves, before
	// any lock is taken or event is logged — for every q form (probe, --re,
	// --teach). add, edit --src, reopen --src, and recite are uncapped, and a
	// question already in the graph above the cap keeps working.
	if txt := citeMeta.SrcText; txt != "" {
		nLines := strings.Count(txt, "\n") + 1
		nChars := len(txt)
		if nLines > questionCiteLineMax || nChars > questionCiteCharMax {
			locator := citeStr
			if c, perr := cite.Parse(citeStr); perr == nil {
				locator = c.File
			}
			ctx.ErrMsg = fmt.Sprintf(
				"citation spans %d lines/%d chars; a question cites at most %d lines or %d chars",
				nLines, nChars, questionCiteLineMax, questionCiteCharMax)
			ctx.FixMsg = fmt.Sprintf("narrow with tm src %s --find <regex>", locator)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
	}

	// ── Apply closure ─────────────────────────────────────────────────────────

	var newQID string

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		cfg := s.Cfg()
		logPath := eventlog.Path(file)

		ns := graphNodeSets(g, false)

		// Exit 3: unknown concept.
		if !ns.AllConcepts[conceptID] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown concept %q", conceptID),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Exit 3: unknown --re qid.
		if reQID != "" {
			found := false
			for _, item := range g.TestingItems {
				if item.Q != nil && item.Q.ID == reQID {
					found = true
					break
				}
			}
			if !found {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("unknown question %q", reQID),
					Fix:  usageLine,
					Exit: 3,
				}
			}
		}

		// Exit 1: concept is passed.
		if ns.PassedSet[conceptID] {
			return nil, nil, &ops.Refusal{
				Err:  conceptID + " is passed",
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\"", conceptID),
				Exit: 1,
			}
		}

		// Exit 1: concept is gated (§7); --override clears gate and proceeds.
		cs := s.ConceptStatus(conceptID)
		if cs.Gated && overrideReason == "" {
			return nil, nil, &ops.Refusal{
				Err:  conceptID + " is gated",
				Fix:  buildGateFixMsg(conceptID, s),
				Exit: 1,
			}
		}

		allBatches := s.ConceptBatches(conceptID)

		// Find the concept node for the GAP field (needed by teach checks).
		var conceptNode *graph.ConceptNode
		for _, c := range g.UntestedConcepts {
			if c.ID == conceptID {
				conceptNode = c
				break
			}
		}

		if !isTeach {
			// ── Probe question ────────────────────────────────────────────────

			// Exit 1: any probe batch is locked or open.
			for _, b := range allBatches {
				if !graph.IsProbeClass(b) {
					continue
				}
				switch s.BatchStateOf(b) {
				case state.BatchLocked:
					lockingTeach := qFindLockingTeach(allBatches, b)
					if lockingTeach != "" {
						return nil, nil, &ops.Refusal{
							Err:  fmt.Sprintf("%s is locked by %s", b, lockingTeach),
							Fix:  fmt.Sprintf("finish %s, then answer %s", lockingTeach, b),
							Exit: 1,
						}
					}
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("%s is locked", b),
						Exit: 1,
					}
				case state.BatchOpen:
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("%s is open (answers in progress)", b),
						Fix:  fmt.Sprintf("finish answering %s before adding new questions", b),
						Exit: 1,
					}
				}
			}

			// Exit 1: --re target belongs to a different concept.
			if reQID != "" {
				if c, ok := s.ConceptOf(reQID); !ok || c != conceptID {
					msg := fmt.Sprintf("%s does not belong to %s", reQID, conceptID)
					if ok {
						msg = fmt.Sprintf("%s belongs to %s, not %s", reQID, c, conceptID)
					}
					return nil, nil, &ops.Refusal{
						Err:  msg,
						Exit: 1,
					}
				}
			}

			// Exit 1: --re on a probe whose target is not an unclear probe.
			if reQID != "" && !s.ProbeReplacesUnclear(reQID) {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("%s is not an unclear probe", reQID),
					Fix:  "--re requires a probe question whose answer graded unclear",
					Exit: 1,
				}
			}

			// Exit 1: draft probe batch is at max.
			draftBatch, hasDraft := s.DraftProbeBatch(conceptID)
			if hasDraft && len(s.BatchQuestions(draftBatch)) >= cfg.ProbeMax {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("batch %s is at maximum size (%d)", draftBatch, cfg.ProbeMax),
					Exit: 1,
				}
			}

			// Allocate new question N and batch class.
			qN := graph.NextQuestionN(g, logPath)
			qid := fmt.Sprintf("q%d", qN)
			batchClass := draftBatch
			if !hasDraft {
				bN := graph.NextBatchN(g, logPath)
				batchClass = fmt.Sprintf("probe_%d", bN)
			}

			// Determine edge source: concept for regular probe, a<N(re)> for replacement.
			edgeFrom := conceptID
			if reQID != "" {
				reN := graph.QuestionN(reQID)
				edgeFrom = fmt.Sprintf("a%d", reN)
			}

			// Build new graph (copy-on-write).
			newG := *g
			newG.TestingItems = append(append([]graph.TestingItem{}, g.TestingItems...), graph.TestingItem{
				Q: &graph.QuestionNode{
					ID:    qid,
					Scope: scope,
					Cite:  citeStr,
					Class: batchClass,
				},
			})
			newG.Edges = append(append([]*graph.Edge{}, g.Edges...), &graph.Edge{
				From: edgeFrom,
				To:   qid,
			})

			newQID = qid
			re := reQID // "" when no --re
			probeFields := map[string]any{
				"q":       qid,
				"concept": conceptID,
				"batch":   batchClass,
				"kind":    "probe",
				"scope":   scope,
				"src":     citeStr,
				"re":      re,
			}
			source.ApplyMeta(probeFields, citeMeta)
			row := eventlog.NewRow("q", probeFields)
			// Gate clearing via --override (spec §7 line 278, Q3/Q7).
			rows := []eventlog.Row{row}
			finalG := &newG
			if overrideReason != "" && cs.Gated {
				if clearedG, gateRow, ok := ops.ClearGate(finalG, s, conceptID, "override", overrideReason); ok {
					finalG = clearedG
					rows = append([]eventlog.Row{gateRow}, rows...)
				}
			}
			return finalG, rows, nil
		}

		// ── Teach question ────────────────────────────────────────────────────

		// Exit 1: --teach without --re.
		if reQID == "" {
			return nil, nil, &ops.Refusal{
				Err:  "--teach requires --re",
				Fix:  fmt.Sprintf("tm q %s <cite> \"<scope>\" --teach --re <qid>", conceptID),
				Exit: 1,
			}
		}

		// Exit 1: teaching is spent.
		if cs.TeachingSpent {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("teaching is spent for %s", conceptID),
				Exit: 1,
			}
		}

		// Exit 1: concept has no GAP.
		if conceptNode == nil || conceptNode.GAP == "" {
			return nil, nil, &ops.Refusal{
				Err:  conceptID + " has no GAP",
				Fix:  fmt.Sprintf("tm gap %s \"<gap>\"", conceptID),
				Exit: 1,
			}
		}

		// Exit 1: no failed probe batch above base.
		if len(cs.FailedProbeBatches) == 0 {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("no failed probe batch above base for %s", conceptID),
				Exit: 1,
			}
		}

		// Exit 1: no fallback probe batch at minimum size with no answers yet.
		hasFallback := false
		for _, fp := range cs.FallbackProbes {
			if len(s.BatchQuestions(fp)) >= cfg.ProbeMin {
				hasFallback = true
				break
			}
		}
		if !hasFallback {
			return nil, nil, &ops.Refusal{
				Err: fmt.Sprintf(
					"no fallback probe batch at minimum size (%d) for %s",
					cfg.ProbeMin, conceptID,
				),
				Fix: fmt.Sprintf(
					"add at least %d probe questions to a fallback batch with tm q %s",
					cfg.ProbeMin, conceptID,
				),
				Exit: 1,
			}
		}

		// Exit 1: any teach batch is open.
		for _, b := range allBatches {
			if !graph.IsTeachClass(b) {
				continue
			}
			if s.BatchStateOf(b) == state.BatchOpen {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("%s is open (answers in progress)", b),
					Fix:  fmt.Sprintf("finish answering %s before adding new teach questions", b),
					Exit: 1,
				}
			}
		}

		// Exit 1: --re target belongs to a different concept.
		if c, ok := s.ConceptOf(reQID); !ok || c != conceptID {
			msg := fmt.Sprintf("%s does not belong to %s", reQID, conceptID)
			if ok {
				msg = fmt.Sprintf("%s belongs to %s, not %s", reQID, c, conceptID)
			}
			return nil, nil, &ops.Refusal{
				Err:  msg,
				Exit: 1,
			}
		}

		// Exit 1: --re target answer is not fail/unclear or is flagged OOS.
		targetAnswer, hasTargetAnswer := s.TeachTargetAnswer(reQID)
		if !hasTargetAnswer {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s has no answer (--teach --re requires a graded fail or unclear answer)", reQID),
				Exit: 1,
			}
		}
		if targetAnswer.Class != "fail" && targetAnswer.Class != "unclear" {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s answer is %q, not fail or unclear", reQID, targetAnswer.Class),
				Exit: 1,
			}
		}
		if targetAnswer.OOS {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s answer is flagged OOS", reQID),
				Exit: 1,
			}
		}

		// Exit 1: draft teach batch is at max.
		draftBatch, hasDraft := s.DraftTeachBatch(conceptID)
		if hasDraft && len(s.BatchQuestions(draftBatch)) >= cfg.TeachMax {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("batch %s is at maximum size (%d)", draftBatch, cfg.TeachMax),
				Exit: 1,
			}
		}

		// Allocate new question N and batch class.
		qN := graph.NextQuestionN(g, logPath)
		qid := fmt.Sprintf("q%d", qN)
		batchClass := draftBatch
		if !hasDraft {
			bN := graph.NextBatchN(g, logPath)
			batchClass = fmt.Sprintf("teach_%d", bN)
		}

		// Edge from a<N(re)>.
		reN := graph.QuestionN(reQID)
		edgeFrom := fmt.Sprintf("a%d", reN)

		// Build new graph (copy-on-write).
		newG := *g
		newG.TestingItems = append(append([]graph.TestingItem{}, g.TestingItems...), graph.TestingItem{
			Q: &graph.QuestionNode{
				ID:    qid,
				Scope: scope,
				Cite:  citeStr,
				Class: batchClass,
			},
		})
		newG.Edges = append(append([]*graph.Edge{}, g.Edges...), &graph.Edge{
			From: edgeFrom,
			To:   qid,
		})

		newQID = qid
		teachFields := map[string]any{
			"q":       qid,
			"concept": conceptID,
			"batch":   batchClass,
			"kind":    "teach",
			"scope":   scope,
			"src":     citeStr,
			"re":      reQID,
		}
		source.ApplyMeta(teachFields, citeMeta)
		row := eventlog.NewRow("q", teachFields)
		// Gate clearing via --override (spec §7 line 278, Q3/Q7).
		rows := []eventlog.Row{row}
		finalG := &newG
		if overrideReason != "" && cs.Gated {
			if clearedG, gateRow, ok := ops.ClearGate(finalG, s, conceptID, "override", overrideReason); ok {
				finalG = clearedG
				rows = append([]eventlog.Row{gateRow}, rows...)
			}
		}
		return finalG, rows, nil
	}

	stateCfg := state.ConfigFromEnv()
	lintCfg := buildLintConfig(file)

	_, refusal, engErr := ops.Mutate(file, stateCfg, lintCfg, errlog.RealClock, apply)
	if engErr != nil {
		ctx.ErrMsg = fmt.Sprintf("cannot mutate %s: %v", filepath.Base(file), engErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	if refusal != nil {
		ctx.ErrMsg = refusal.Err
		ctx.FixMsg = refusal.Fix
		writeErrFix(ctx.ErrOut, refusal.Err, refusal.Fix)
		return refusal.Exit
	}

	_, _ = fmt.Fprintln(ctx.Out, newQID)
	return 0
}

// qFindLockingTeach returns the lowest-N teach batch in allBatches whose N
// exceeds the N of probeClass. Returns "" when no such batch exists.
func qFindLockingTeach(allBatches []string, probeClass string) string {
	probeN := graph.BatchN(probeClass)
	result := ""
	for _, b := range allBatches {
		if !graph.IsTeachClass(b) {
			continue
		}
		bN := graph.BatchN(b)
		if bN > probeN {
			if result == "" || bN < graph.BatchN(result) {
				result = b
			}
		}
	}
	return result
}
