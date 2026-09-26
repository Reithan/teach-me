package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/reithan/teach-me/internal/errlog"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// pruneRun is the Run handler for `tm prune <goal> [--keep N]`.
//
// Parks every untested concept with no questions that is outside the goal's
// ancestor closure. With --keep N, also parks closure members beyond the N
// nearest untested concepts by hop distance (goal counted first, ties by ID).
// Concepts with questions are never moved. Outputs "reserved <n>".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (goal not in untested, --keep < 1)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID
func pruneRun(ctx *Context) int {
	goal := ctx.Positionals[0]
	usageLine := FindCommand("prune").Usage()

	// Parse optional --keep N.
	keepN := -1
	if v := ctx.Flags["keep"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 {
			ctx.ErrMsg = "--keep must be a positive integer"
			ctx.FixMsg = usageLine
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 1
		}
		keepN = n
	}

	var movedCount int

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g)

		// Unknown goal → exit 3.
		if !ns.AllConcepts[goal] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", goal),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Goal not in untested → exit 1.
		// Spec §7: fix is "tm activate <goal>" only when goal is in reserve.
		if !ns.UntestedSet[goal] {
			var fix string
			if ns.ReserveSet[goal] {
				fix = fmt.Sprintf("tm activate %s", goal)
			}
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s is not in untested", goal),
				Fix:  fix,
				Exit: 1,
			}
		}

		// Build question-has-questions set.
		hasQuestions := make(map[string]bool)
		for _, item := range g.TestingItems {
			if item.Q == nil {
				continue
			}
			cid, ok := s.ConceptOf(item.Q.ID)
			if ok {
				hasQuestions[cid] = true
			}
		}

		// Get ancestor closure from goal.
		closure := s.AncestorClosure(goal)

		// Determine which concepts to move.
		// moveSet tracks IDs to park (in file order from g.UntestedConcepts).
		moveSet := make(map[string]bool)

		// Base: park untested, question-less, not goal, outside closure.
		for _, c := range g.UntestedConcepts {
			if c.ID == goal {
				continue
			}
			if hasQuestions[c.ID] {
				continue
			}
			if _, inClosure := closure[c.ID]; !inClosure {
				moveSet[c.ID] = true
			}
		}

		// With --keep N: also park in-closure candidates beyond the N nearest.
		if keepN >= 1 {
			// Collect all untested concepts (including those with questions) that
			// are in the closure, sorted by (hop distance, ID). Goal is at dist=0.
			type entry struct {
				id   string
				dist int
			}
			var closureUntested []entry
			for _, c := range g.UntestedConcepts {
				if d, ok := closure[c.ID]; ok {
					closureUntested = append(closureUntested, entry{c.ID, d})
				}
			}
			sort.Slice(closureUntested, func(i, j int) bool {
				if closureUntested[i].dist != closureUntested[j].dist {
					return closureUntested[i].dist < closureUntested[j].dist
				}
				return closureUntested[i].id < closureUntested[j].id
			})

			// Keep the first keepN, park the rest if question-less.
			for i, e := range closureUntested {
				if i < keepN {
					continue // kept
				}
				if e.id == goal {
					continue // goal is never moved
				}
				if hasQuestions[e.id] {
					continue // concepts with questions are never moved
				}
				moveSet[e.id] = true
			}
		}

		if len(moveSet) == 0 {
			movedCount = 0
			row := eventlog.NewRow("prune", map[string]any{
				"goal":  goal,
				"keep":  keepEventVal(keepN),
				"moved": []string{},
			})
			return g, []eventlog.Row{row}, nil
		}

		// Build moved list in file order (g.UntestedConcepts declaration order).
		moved := make([]string, 0, len(moveSet))
		for _, c := range g.UntestedConcepts {
			if moveSet[c.ID] {
				moved = append(moved, c.ID)
			}
		}
		movedCount = len(moved)

		// Build new graph: remove moved from untested, append to reserve.
		newG := *g

		newUntested := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)-len(moved))
		newReserve := make([]*graph.ConceptNode, len(g.ReserveConcepts), len(g.ReserveConcepts)+len(moved))
		copy(newReserve, g.ReserveConcepts)

		for _, c := range g.UntestedConcepts {
			if moveSet[c.ID] {
				newNode := *c
				newNode.Block = graph.BlockReserve
				newReserve = append(newReserve, &newNode)
			} else {
				newUntested = append(newUntested, c)
			}
		}

		newG.UntestedConcepts = newUntested
		newG.ReserveConcepts = newReserve

		row := eventlog.NewRow("prune", map[string]any{
			"goal":  goal,
			"keep":  keepEventVal(keepN),
			"moved": moved,
		})
		return &newG, []eventlog.Row{row}, nil
	}

	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

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

	_, _ = fmt.Fprintf(ctx.Out, "reserved %d\n", movedCount)
	return 0
}

// keepEventVal returns the event value for the "keep" field: the integer N when
// --keep was given, nil (null in JSON) when omitted.
func keepEventVal(keepN int) any {
	if keepN < 0 {
		return nil
	}
	return keepN
}
