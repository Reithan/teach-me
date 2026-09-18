package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/reithan/teach-me/internal/state"
)

// findRun is the Run handler for `tm find "<text>" [--kind concept|q|a]`.
//
// Performs case-insensitive substring search over concept scopes, question
// scopes, and graded answer Labels. --kind restricts to one node type.
//
// Output line: "<id>  <state>  <truncated scope>" (scope truncated at 60 runes
// with an ellipsis appended). Deterministic graph order.
//
// Exit codes:
//
//	0  ok (empty output when no hits)
//	3  file error
func findRun(ctx *Context) int {
	query := ctx.Positionals[0]
	kind := ""
	if v := ctx.Flags["kind"]; len(v) > 0 {
		kind = v[0]
	}

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
	lower := strings.ToLower(query)

	// Build lookup sets for concept state classification.
	passedSet := make(map[string]bool, len(g.PassedConcepts))
	for _, c := range g.PassedConcepts {
		passedSet[c.ID] = true
	}
	frontier := s.Frontier()
	frontierSet := make(map[string]bool, len(frontier))
	for _, f := range frontier {
		frontierSet[f] = true
	}

	// Build answer map for question-state lookup.
	answerByQID := buildAnswerMap(g)

	const maxRunes = 60

	// truncate shortens s to maxRunes runes, appending an ellipsis if truncated.
	truncate := func(text string) string {
		if utf8.RuneCountInString(text) <= maxRunes {
			return text
		}
		r := []rune(text)
		return string(r[:maxRunes]) + "…"
	}

	// Search concepts (passed first, then untested, preserving declaration order).
	if kind == "" || kind == "concept" {
		for _, c := range g.PassedConcepts {
			if strings.Contains(strings.ToLower(c.Scope), lower) {
				_, _ = fmt.Fprintf(ctx.Out, "%s  passed  %s\n", c.ID, truncate(c.Scope))
			}
		}
		for _, c := range g.UntestedConcepts {
			if strings.Contains(strings.ToLower(c.Scope), lower) {
				st := "blocked"
				if frontierSet[c.ID] {
					st = "open"
				}
				_, _ = fmt.Fprintf(ctx.Out, "%s  %s  %s\n", c.ID, st, truncate(c.Scope))
			}
		}
	}

	// Search questions and answers (TestingItems declaration order).
	for _, item := range g.TestingItems {
		if item.Q != nil && (kind == "" || kind == "q") {
			q := item.Q
			if strings.Contains(strings.ToLower(q.Scope), lower) {
				// Question state: answer class if graded, batch state if not.
				qState := batchStateStr(s.BatchStateOf(q.Class))
				if a, ok := answerByQID[q.ID]; ok {
					qState = a.Class
				}
				_, _ = fmt.Fprintf(ctx.Out, "%s  %s  %s\n", q.ID, qState, truncate(q.Scope))
			}
		}
		if item.A != nil && (kind == "" || kind == "a") {
			a := item.A
			if strings.Contains(strings.ToLower(a.Label), lower) {
				_, _ = fmt.Fprintf(ctx.Out, "%s  %s  %s\n", a.ID, a.Class, truncate(a.Label))
			}
		}
	}
	return 0
}
