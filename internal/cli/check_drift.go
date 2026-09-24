package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// gradeRec holds the last grade recorded for a single question in a concept.
type gradeRec struct {
	qid     string
	scope   string
	citeStr string // latest citation (after rehash/recite updates)
	raw     string
	srcText string // source text at time of grading
	verdict string
}

// conceptGrades reads the event log at logPath and returns the last grade
// for each question that belongs to concept, in the order the questions first
// appeared in the log.
//
// Returns an empty slice when the concept has no grade events; returns an
// error only when logPath cannot be read.
func conceptGrades(logPath, concept string) ([]gradeRec, error) {
	logData, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}

	// Single-pass accumulation:
	//   qCite:    qid → latest citation (updated by q, rehash, recite events)
	//   qScope:   qid → question scope
	//   qConcept: qid → concept ID
	//   byQ:      qid → last gradeRec seen (overwritten on each grade event)
	//   order:    qids in first-seen order for stable output
	qCite := make(map[string]string)
	qScope := make(map[string]string)
	qConcept := make(map[string]string)
	byQ := make(map[string]gradeRec)
	var order []string

	for _, line := range strings.Split(string(logData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if jerr := json.Unmarshal([]byte(line), &ev); jerr != nil {
			continue
		}
		evName, _ := ev["ev"].(string)

		switch evName {
		case "q":
			qid, _ := ev["q"].(string)
			src, _ := ev["src"].(string)
			scope, _ := ev["scope"].(string)
			cID, _ := ev["concept"].(string)
			if qid != "" {
				qCite[qid] = src
				qScope[qid] = graph.Unescape(scope)
				qConcept[qid] = cID
			}

		case "rehash", "recite":
			id, _ := ev["id"].(string)
			after, _ := ev["after"].(string)
			if id != "" && after != "" {
				if _, ok := qCite[id]; ok {
					qCite[id] = after
				}
			}

		case "grade":
			qid, _ := ev["q"].(string)
			if qid == "" || qConcept[qid] != concept {
				continue
			}
			raw, _ := ev["raw"].(string)
			srcText, _ := ev["src_text"].(string)
			verdict, _ := ev["verdict"].(string)
			if _, seen := byQ[qid]; !seen {
				order = append(order, qid)
			}
			byQ[qid] = gradeRec{
				qid:     qid,
				scope:   qScope[qid],
				citeStr: qCite[qid],
				raw:     graph.Unescape(raw),
				srcText: graph.Unescape(srcText),
				verdict: verdict,
			}
		}
	}

	result := make([]gradeRec, 0, len(order))
	for _, qid := range order {
		result = append(result, byQ[qid])
	}
	return result, nil
}

// checkDriftRun handles `tm check --drift <concept>`.
//
// Reads every `grade` event for the passed concept from the event log, resolves
// each question citation against the current source, and prints the §9.1 recheck
// payload. The event log is the second (and only other) log reader besides
// `show --history`.
//
// Exit codes:
//
//	0  payload emitted
//	1  event log is missing or unreadable (§7 line 298)
//	3  usage error or file problem
func checkDriftRun(ctx *Context) int {
	concept := ctx.Positionals[0]

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

	srcRoot := s.Cfg().SrcRoot

	// Verify the concept is known (passed, untested, or reserve).
	g := s.Graph()
	conceptKnown := false
	for _, c := range g.PassedConcepts {
		if c.ID == concept {
			conceptKnown = true
			break
		}
	}
	if !conceptKnown {
		for _, c := range g.UntestedConcepts {
			if c.ID == concept {
				conceptKnown = true
				break
			}
		}
	}
	if !conceptKnown {
		for _, c := range g.ReserveConcepts {
			if c.ID == concept {
				conceptKnown = true
				break
			}
		}
	}
	if !conceptKnown {
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Read the event log. A missing or unreadable log is a refusal (§7 line 298).
	logPath := eventlog.Path(file)
	grades, readErr := conceptGrades(logPath, concept)
	if readErr != nil {
		ctx.ErrMsg = fmt.Sprintf("no event log for %s", concept)
		ctx.FixMsg = fmt.Sprintf("tm reopen %s \"<gap>\"", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	var b strings.Builder

	for _, ge := range grades {
		// Q <qid>: <question scope>
		fmt.Fprintf(&b, "Q %s: %s\n", ge.qid, ge.scope)
		// CITE <citation>
		fmt.Fprintf(&b, "CITE %s\n", ge.citeStr)

		// Check drift on the current citation.
		drifted, _ := checkCiteDrift(ge.citeStr, srcRoot)
		if drifted {
			fmt.Fprintf(&b, "DRIFT %s\n", ge.citeStr)
		}

		// SRC_GRADED block (text at time of grading)
		fmt.Fprintln(&b, "SRC_GRADED")
		for _, l := range strings.Split(ge.srcText, "\n") {
			fmt.Fprintf(&b, "  %s\n", l)
		}

		// SRC_CURRENT block (text now)
		fmt.Fprintln(&b, "SRC_CURRENT")
		currentText, readCiteErr := readCiteText(ge.citeStr, srcRoot)
		if readCiteErr == nil {
			for _, l := range strings.Split(currentText, "\n") {
				fmt.Fprintf(&b, "  %s\n", l)
			}
		} else {
			fmt.Fprintf(&b, "  [citation unreadable: %v]\n", readCiteErr)
		}

		// A: <raw answer>
		fmt.Fprintf(&b, "A: %s\n", ge.raw)
		// VERDICT: <recorded verdict>
		fmt.Fprintf(&b, "VERDICT: %s\n", ge.verdict)
	}

	// Grader rubric and grade command line (§9.1).
	fmt.Fprintln(&b, "keep: every answer still holds against the current text (substance unchanged or delta does not affect the graded scope).")
	fmt.Fprintln(&b, "reopen: at least one answer no longer holds, or the grader cannot tell.")
	fmt.Fprintf(&b, "tm grade --drift %s keep|reopen \"<summary>\"\n", concept)

	_, _ = fmt.Fprint(ctx.Out, b.String())
	return 0
}

// gradeDriftRun handles `tm grade --drift <concept> keep|reopen "<summary>"`.
//
// ForbidTeacher enforcement is done by the table (grade.ForbidTeacher=true).
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal
//	2  output fails lint
//	3  usage or file error
func gradeDriftRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	verdict := ctx.Positionals[1]
	summary := ctx.Positionals[2]

	// Runtime validation: --drift only accepts keep|reopen.
	if verdict != "keep" && verdict != "reopen" {
		ctx.ErrMsg = fmt.Sprintf("--drift verdict must be keep or reopen, got %q", verdict)
		ctx.FixMsg = fmt.Sprintf("tm grade --drift %s keep|reopen \"<summary>\"", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 3
	}

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		srcRoot := s.Cfg().SrcRoot

		// Verify concept exists (passed, untested, or reserve).
		var targetNode *graph.ConceptNode
		for _, c := range g.PassedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}
		if targetNode == nil {
			for _, c := range g.UntestedConcepts {
				if c.ID == concept {
					targetNode = c
					break
				}
			}
		}
		if targetNode == nil {
			for _, c := range g.ReserveConcepts {
				if c.ID == concept {
					targetNode = c
					break
				}
			}
		}
		if targetNode == nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown concept %q", concept),
				Fix:  fmt.Sprintf("tm grade --drift %s keep|reopen \"<summary>\"", concept),
				Exit: 3,
			}
		}

		// Collect per-question src_text_before from the event log for the recheck event.
		logPath := eventlog.Path(ctx.GraphFile)
		grades, _ := conceptGrades(logPath, concept)

		recheckQs := make([]map[string]any, 0, len(grades))
		for _, ge := range grades {
			currentText, _ := readCiteText(ge.citeStr, srcRoot)
			recheckQs = append(recheckQs, map[string]any{
				"q":               ge.qid,
				"src_text_before": ge.srcText,
				"src_text_after":  currentText,
			})
		}

		var (
			newG *graph.Graph
			rows []eventlog.Row
		)

		if verdict == "keep" {
			// Re-hash all concept citations to the current text.
			// For drifted citations the stored hash no longer matches; strip it
			// and rehash against current content. Refuse only when the file
			// cannot be resolved at all.
			newNode := *targetNode
			newCites := make([]string, len(targetNode.Cites))
			for i, citeStr := range targetNode.Cites {
				// Strip any existing hash so HashCitation always reads current content.
				hashless := citeStr
				if cit, parseErr := cite.Parse(citeStr); parseErr == nil && cit.Hash != "" {
					hashless = fmt.Sprintf("%s:%d-%d", cit.File, cit.Start, cit.End)
				}
				hashed, hashErr := hashCiteText(hashless, srcRoot)
				if hashErr != nil {
					return nil, nil, &ops.Refusal{
						Err:  fmt.Sprintf("cannot resolve citation %q: %v", citeStr, hashErr),
						Fix:  "fix the source file or use reopen to re-point the citation",
						Exit: 1,
					}
				}
				newCites[i] = hashed
			}
			newNode.Cites = newCites

			// Build updated graph.
			ng := *g
			switch targetNode.Block {
			case graph.BlockPassed:
				newPassed := make([]*graph.ConceptNode, len(g.PassedConcepts))
				copy(newPassed, g.PassedConcepts)
				for i, c := range newPassed {
					if c.ID == concept {
						newPassed[i] = &newNode
						break
					}
				}
				ng.PassedConcepts = newPassed
			case graph.BlockReserve:
				newReserve := make([]*graph.ConceptNode, len(g.ReserveConcepts))
				copy(newReserve, g.ReserveConcepts)
				for i, c := range newReserve {
					if c.ID == concept {
						newReserve[i] = &newNode
						break
					}
				}
				ng.ReserveConcepts = newReserve
			default:
				newUntested := make([]*graph.ConceptNode, len(g.UntestedConcepts))
				copy(newUntested, g.UntestedConcepts)
				for i, c := range newUntested {
					if c.ID == concept {
						newUntested[i] = &newNode
						break
					}
				}
				ng.UntestedConcepts = newUntested
			}
			newG = &ng

		} else {
			// reopen: run the full reopenApply logic (gate clearing, reopen event).
			usageLine := fmt.Sprintf("tm grade --drift %s keep|reopen \"<summary>\"", concept)
			ng, reopenRows, ref := reopenApply(g, s, concept, summary, "", usageLine)
			if ref != nil {
				return nil, nil, ref
			}
			newG = ng
			rows = reopenRows
		}

		recheckRow := eventlog.NewRow("recheck", map[string]any{
			"concept":   concept,
			"verdict":   verdict,
			"summary":   summary,
			"questions": recheckQs,
		})
		rows = append(rows, recheckRow)

		return newG, rows, nil
	}

	return runMutation(ctx, apply)
}
