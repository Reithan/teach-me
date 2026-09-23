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

	// Verify the concept is known (passed or untested).
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
		ctx.ErrMsg = fmt.Sprintf("unknown concept %q", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}

	// Read the event log. A missing or unreadable log is a refusal (§7 line 298).
	logPath := eventlog.Path(file)
	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		ctx.ErrMsg = fmt.Sprintf("no event log for %s", concept)
		ctx.FixMsg = fmt.Sprintf("tm reopen %s \"<gap>\"", concept)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
		return 1
	}

	// Build question metadata maps from the event log.
	// qScope: qid → scope (from q events)
	// qCite: qid → latest citation string (from q, then recite/rehash)
	// qConcept: qid → concept ID (from q events)
	qScope := make(map[string]string)
	qCite := make(map[string]string)
	qConcept := make(map[string]string)

	// gradeEvents: grade events for questions belonging to this concept.
	type gradeRec struct {
		qid     string
		raw     string
		srcText string
		verdict string
	}
	var gradeEvents []gradeRec

	for _, line := range strings.Split(string(logData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		evName, _ := ev["ev"].(string)

		switch evName {
		case "q":
			// Record question metadata.
			qid, _ := ev["q"].(string)
			scope, _ := ev["scope"].(string)
			src, _ := ev["src"].(string)
			conceptID, _ := ev["concept"].(string)
			if qid != "" {
				qScope[qid] = graph.Unescape(scope)
				qCite[qid] = src
				qConcept[qid] = conceptID
			}

		case "rehash", "recite":
			// Update citation when a question's cite has changed.
			id, _ := ev["id"].(string)
			after, _ := ev["after"].(string)
			if id != "" && after != "" {
				// Only update if this id is a question we track.
				if _, ok := qCite[id]; ok {
					qCite[id] = after
				}
			}

		case "grade":
			// Collect grade events; filter by concept after we have all q events.
			qid, _ := ev["q"].(string)
			if qid == "" {
				continue
			}
			raw, _ := ev["raw"].(string)
			srcText, _ := ev["src_text"].(string)
			verdict, _ := ev["verdict"].(string)
			gradeEvents = append(gradeEvents, gradeRec{
				qid:     qid,
				raw:     graph.Unescape(raw),
				srcText: graph.Unescape(srcText),
				verdict: verdict,
			})
		}
	}

	// Filter grade events to questions belonging to this concept.
	var filtered []gradeRec
	seenQ := make(map[string]bool)
	for _, ge := range gradeEvents {
		if qConcept[ge.qid] == concept {
			// Take only the latest grade per question.
			if !seenQ[ge.qid] {
				seenQ[ge.qid] = true
				filtered = append(filtered, ge)
			}
		}
	}
	// Reverse to get latest grade per question: the log is append-only so
	// the last grade for each qid is the canonical one.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	seenQ = make(map[string]bool)
	var deduped []gradeRec
	for _, ge := range filtered {
		if !seenQ[ge.qid] {
			seenQ[ge.qid] = true
			deduped = append(deduped, ge)
		}
	}
	// Restore original order (by first appearance in log).
	for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
		deduped[i], deduped[j] = deduped[j], deduped[i]
	}
	filtered = deduped

	var b strings.Builder

	for _, ge := range filtered {
		citeStr := qCite[ge.qid]
		scope := qScope[ge.qid]

		// Q <qid>: <question scope>
		fmt.Fprintf(&b, "Q %s: %s\n", ge.qid, scope)
		// CITE <citation>
		fmt.Fprintf(&b, "CITE %s\n", citeStr)

		// Check drift on the current citation.
		drifted, _ := cite.CheckDrift(citeStr, srcRoot)
		if drifted {
			fmt.Fprintf(&b, "DRIFT %s\n", citeStr)
		}

		// SRC_GRADED block (text at time of grading)
		fmt.Fprintln(&b, "SRC_GRADED")
		for _, l := range strings.Split(ge.srcText, "\n") {
			fmt.Fprintf(&b, "  %s\n", l)
		}

		// SRC_CURRENT block (text now)
		fmt.Fprintln(&b, "SRC_CURRENT")
		currentText, readErr := readCiteText(citeStr, srcRoot)
		if readErr == nil {
			for _, l := range strings.Split(currentText, "\n") {
				fmt.Fprintf(&b, "  %s\n", l)
			}
		} else {
			fmt.Fprintf(&b, "  [citation unreadable: %v]\n", readErr)
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

		// Verify concept exists.
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
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown concept %q", concept),
				Fix:  fmt.Sprintf("tm grade --drift %s keep|reopen \"<summary>\"", concept),
				Exit: 3,
			}
		}

		// Collect per-question src_text_before and src_text_after from the
		// event log for the recheck event.
		logPath := eventlog.Path(ctx.GraphFile)
		var recheckQs []map[string]any
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			qCite := make(map[string]string)
			qConcept := make(map[string]string)
			type gradeRec struct {
				qid     string
				srcText string
			}
			var grades []gradeRec
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
					cID, _ := ev["concept"].(string)
					if qid != "" {
						qCite[qid] = src
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
					srcText, _ := ev["src_text"].(string)
					if qid != "" && qConcept[qid] == concept {
						grades = append(grades, gradeRec{qid: qid, srcText: srcText})
					}
				}
			}
			// Deduplicate: keep latest grade per qid.
			seenQ := make(map[string]bool)
			for i := len(grades) - 1; i >= 0; i-- {
				ge := grades[i]
				if !seenQ[ge.qid] {
					seenQ[ge.qid] = true
					citeStr := qCite[ge.qid]
					currentText, _ := readCiteText(citeStr, srcRoot)
					recheckQs = append(recheckQs, map[string]any{
						"q":               ge.qid,
						"src_text_before": graph.Unescape(ge.srcText),
						"src_text_after":  currentText,
					})
				}
			}
		}

		var newG *graph.Graph
		rows := make([]eventlog.Row, 0, 1)

		if verdict == "keep" {
			// Re-hash all concept citations to the current text.
			newNode := *targetNode
			newCites := make([]string, len(targetNode.Cites))
			for i, citeStr := range targetNode.Cites {
				hashed, hashErr := cite.HashCitation(citeStr, srcRoot)
				if hashErr == nil {
					newCites[i] = hashed
				} else {
					newCites[i] = citeStr
				}
			}
			newNode.Cites = newCites

			// Build updated graph.
			ng := *g
			if targetNode.Block == graph.BlockPassed {
				newPassed := make([]*graph.ConceptNode, len(g.PassedConcepts))
				copy(newPassed, g.PassedConcepts)
				for i, c := range newPassed {
					if c.ID == concept {
						newPassed[i] = &newNode
						break
					}
				}
				ng.PassedConcepts = newPassed
			} else {
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
			// reopen: move the passed concept back to untested with summary as GAP.
			// Reuse the reopenApply logic inline to avoid code duplication.
			if targetNode.Block != graph.BlockPassed {
				return nil, nil, &ops.Refusal{
					Err:  fmt.Sprintf("%s is not passed; cannot reopen via grade --drift", concept),
					Exit: 1,
				}
			}

			newNode := *targetNode
			newNode.Block = graph.BlockUntested
			newNode.GAP = summary

			ng := *g
			newPassed := make([]*graph.ConceptNode, 0, len(g.PassedConcepts)-1)
			for _, c := range g.PassedConcepts {
				if c.ID != concept {
					newPassed = append(newPassed, c)
				}
			}
			ng.PassedConcepts = newPassed

			newUntested := make([]*graph.ConceptNode, 0, len(g.UntestedConcepts)+1)
			newUntested = append(newUntested, &newNode)
			newUntested = append(newUntested, g.UntestedConcepts...)
			ng.UntestedConcepts = newUntested
			newG = &ng
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
