package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// errataRun is the Run handler for
// `tm errata <concept> "<scope>" "<reason>" [--src <cite>]` (decision 73).
//
// Rewrites the scope of a concept in any active state, including one that has
// questions or is passed, which `tm edit` refuses. Questions and answers are
// untouched. The reason is logged in an `errata` event; a passed concept's pass
// then goes to a grader recheck (`tm check --errata` / `tm grade --errata`).
// An optional --src flag replaces the citation. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  the ID is a question or answer
//	2  output fails lint (internal error)
//	3  usage error, unknown ID, bad --src citation, empty reason
func errataRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	scope := ctx.Positionals[1]
	reason := ctx.Positionals[2]

	usageLine := FindCommand("errata").Usage()

	if reason == "" {
		ctx.ErrMsg = "errata needs a reason"
		ctx.FixMsg = fmt.Sprintf("tm errata %s \"<scope>\" \"<why the old scope was wrong>\"", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	citeStr, code := hashSrcFlag(ctx, file, usageLine)
	if code != 0 {
		return code
	}

	apply := func(g *graph.Graph, _ *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Locate the concept in the active list that holds it. Reserve
		// concepts are not active, so they fall through as unknown, as in edit.
		var list []*graph.ConceptNode
		inPassed := false
		for _, cand := range []struct {
			nodes  []*graph.ConceptNode
			passed bool
		}{{g.UntestedConcepts, false}, {g.PassedConcepts, true}} {
			for _, c := range cand.nodes {
				if c.ID == concept {
					list, inPassed = cand.nodes, cand.passed
				}
			}
		}

		if list == nil {
			for _, item := range g.TestingItems {
				if (item.Q != nil && item.Q.ID == concept) || (item.A != nil && item.A.ID == concept) {
					return nil, nil, &ops.Refusal{
						Err:  concept + " is not a concept",
						Exit: 1,
					}
				}
			}
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		newG := *g
		newConcepts := make([]*graph.ConceptNode, len(list))
		fields := map[string]any{"id": concept, "reason": reason, "after": scope}
		for i, c := range list {
			if c.ID != concept {
				newConcepts[i] = c
				continue
			}
			newNode := *c
			newNode.Scope = scope
			fields["before"] = c.Scope
			if citeStr != "" {
				newNode.Cites = []string{citeStr}
				oldCite := ""
				if len(c.Cites) > 0 {
					oldCite = c.Cites[0]
				}
				fields["src_before"] = oldCite
				fields["src_after"] = citeStr
			}
			newConcepts[i] = &newNode
		}
		if inPassed {
			newG.PassedConcepts = newConcepts
		} else {
			newG.UntestedConcepts = newConcepts
		}

		return &newG, []eventlog.Row{eventlog.NewRow("errata", fields)}, nil
	}

	return runMutationWithFile(ctx, file, apply)
}

// errataEdit holds the scope correction read from the latest errata event for
// a concept.
type errataEdit struct {
	before string
	after  string
	reason string
}

// latestErrata scans the event log for concept and returns the correction from
// its most recent errata event. ok is true only when such an event exists and
// came after the concept's most recent pass event, i.e. the pass has not yet
// been rechecked against a newer correction. ok is false when the log is
// unreadable or no qualifying errata event is present. Plain edit events never
// qualify.
func latestErrata(logPath, concept string) (errataEdit, bool) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return errataEdit{}, false
	}

	var (
		found     errataEdit
		haveEdit  bool
		sincePass bool // an errata event was seen after the last pass
	)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev["ev"] {
		case "pass":
			if c, _ := ev["concept"].(string); c == concept {
				sincePass = false
			}
		case "errata":
			if id, _ := ev["id"].(string); id != concept {
				continue
			}
			reason, _ := ev["reason"].(string)
			before, _ := ev["before"].(string)
			after, _ := ev["after"].(string)
			found = errataEdit{before: before, after: after, reason: reason}
			haveEdit = true
			sincePass = true
		}
	}
	return found, haveEdit && sincePass
}

// checkErrataRun handles `tm check --errata <concept>` (§9.1 errata payload).
//
// For a passed concept whose scope was corrected with `tm errata`, it
// prints the correction header (SCOPE_BEFORE / SCOPE_AFTER / REASON) then one
// block per logged grade event so the grader can judge whether the pass
// survives the correction.
//
// Exit codes:
//
//	0  payload emitted
//	1  the concept is not passed, or has no errata since its last pass
//	3  usage error, unknown concept, or file problem
func checkErrataRun(ctx *Context) int {
	concept := ctx.Positionals[0]

	s, file, loadCode := loadStateCtx(ctx)
	if loadCode != 0 {
		return loadCode
	}

	g := s.Graph()

	// The concept must be known and currently passed.
	if code := errataConceptGuard(ctx, g, concept); code != 0 {
		return code
	}

	// A pass is rechecked only against a newer errata event.
	logPath := eventlog.Path(file)
	ee, ok := latestErrata(logPath, concept)
	if !ok {
		ctx.ErrMsg = fmt.Sprintf("no errata for %s since it passed", concept)
		ctx.FixMsg = fmt.Sprintf("tm errata %s \"<scope>\" \"<reason>\"", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	grades, _ := conceptGrades(logPath, concept)

	var b strings.Builder
	fmt.Fprintf(&b, "SCOPE_BEFORE %s\n", ee.before)
	fmt.Fprintf(&b, "SCOPE_AFTER %s\n", ee.after)
	fmt.Fprintf(&b, "REASON %s\n", ee.reason)

	for _, ge := range grades {
		fmt.Fprintf(&b, "Q %s: %s\n", ge.qid, ge.scope)
		fmt.Fprintf(&b, "CITE %s\n", ge.citeStr)
		fmt.Fprintln(&b, "SRC")
		for _, l := range strings.Split(ge.srcText, "\n") {
			fmt.Fprintf(&b, "  %s\n", l)
		}
		fmt.Fprintf(&b, "A: %s\n", ge.raw)
		fmt.Fprintf(&b, "VERDICT: %s\n", ge.verdict)
	}

	fmt.Fprintln(&b, "keep: no logged question or verdict turns on the detail that differs between SCOPE_BEFORE and SCOPE_AFTER.")
	fmt.Fprintln(&b, "reopen: a logged question or verdict turns on that detail, or the grader cannot tell.")
	fmt.Fprintf(&b, "tm grade --errata %s keep|reopen \"<summary>\"\n", concept)

	_, _ = fmt.Fprint(ctx.Out, b.String())
	return 0
}

// gradeErrataRun handles `tm grade --errata <concept> keep|reopen "<summary>"`.
//
// `keep` leaves the pass in place; `reopen` runs the reopen path with the
// summary as the GAP. Both log a `recheck` event with `kind: errata`.
// ForbidTeacher is enforced by the table.
//
// Exit codes:
//
//	0  ok
//	1  the concept is not passed, or has no errata since its last pass
//	2  output fails lint
//	3  usage error
func gradeErrataRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	verdict := ctx.Positionals[1]
	summary := ctx.Positionals[2]

	usageLine := fmt.Sprintf("tm grade --errata %s keep|reopen \"<summary>\"", concept)

	if verdict != "keep" && verdict != "reopen" {
		ctx.ErrMsg = fmt.Sprintf("--errata verdict must be keep or reopen, got %q", verdict)
		ctx.FixMsg = usageLine
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// The concept must be currently passed.
		passed := false
		for _, c := range g.PassedConcepts {
			if c.ID == concept {
				passed = true
				break
			}
		}
		if !passed {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not passed",
				Fix:  usageLine,
				Exit: 1,
			}
		}

		// A pass is rechecked only against a newer errata event.
		if _, ok := latestErrata(eventlog.Path(ctx.GraphFile), concept); !ok {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("no errata for %s since it passed", concept),
				Fix:  fmt.Sprintf("tm errata %s \"<scope>\" \"<reason>\"", concept),
				Exit: 1,
			}
		}

		recheckRow := eventlog.NewRow("recheck", map[string]any{
			"concept": concept,
			"verdict": verdict,
			"summary": summary,
			"kind":    "errata",
		})

		if verdict == "keep" {
			// Nothing changes but the log.
			return g, []eventlog.Row{recheckRow}, nil
		}

		// reopen: run the shared reopen logic (gate clearing, reopen event),
		// then append the recheck event.
		newG, rows, ref := reopenApply(g, s, concept, summary, "", usageLine)
		if ref != nil {
			return nil, nil, ref
		}
		rows = append(rows, recheckRow)
		return newG, rows, nil
	}

	return runMutation(ctx, apply)
}

// errataConceptGuard verifies that concept is known to the graph and is
// currently passed. It writes the err:/fix: lines and returns the exit code on
// failure, or 0 when the concept is passed.
func errataConceptGuard(ctx *Context, g *graph.Graph, concept string) int {
	known := false
	for _, c := range g.PassedConcepts {
		if c.ID == concept {
			return 0
		}
	}
	for _, lists := range [][]*graph.ConceptNode{g.UntestedConcepts, g.ReserveConcepts} {
		for _, c := range lists {
			if c.ID == concept {
				known = true
			}
		}
	}
	if !known {
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.ErrMsg = concept + " is not passed"
	ctx.FixMsg = fmt.Sprintf("tm errata %s \"<scope>\" \"<reason>\"", concept)
	writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
	return 1
}
