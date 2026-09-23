package cli

import (
	"fmt"
	"path/filepath"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// editRun is the Run handler for `tm edit <concept> "<scope>" [--src <cite>]`.
//
// Rewrites the scope of a concept that has no questions yet. An optional
// --src flag replaces the citation. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (passed, wrong type, has questions)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID, bad --src citation
func editRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	scope := ctx.Positionals[1]

	usageLine := FindCommand("edit").Usage()

	// ── Pre-Mutate validation (exit 3) ───────────────────────────────────────

	// Resolve the file now so we can validate the citation against SrcRoot.
	file, err := state.ResolveFile(ctx.FileFlag)
	if err != nil {
		ctx.ErrMsg = err.Error()
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return 3
	}
	ctx.GraphFile = file

	// Validate --src citation if given.
	citeStr := ""
	if vals := ctx.Flags["src"]; len(vals) > 0 {
		citeStr = vals[0]
		srcRoot := cite.SrcRoot(filepath.Dir(file))
		hashedCite, hashErr := cite.HashCitation(citeStr, srcRoot)
		if hashErr != nil {
			ctx.ErrMsg = fmt.Sprintf("citation %q: %v", citeStr, hashErr)
			ctx.FixMsg = usageLine
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
		citeStr = hashedCite
	}

	// ── Apply closure ────────────────────────────────────────────────────────
	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		// Build node membership sets.
		allConcepts := make(map[string]bool)
		passedSet := make(map[string]bool)
		for _, c := range g.PassedConcepts {
			allConcepts[c.ID] = true
			passedSet[c.ID] = true
		}
		for _, c := range g.UntestedConcepts {
			allConcepts[c.ID] = true
		}

		allNodes := make(map[string]bool)
		for id := range allConcepts {
			allNodes[id] = true
		}
		for _, item := range g.TestingItems {
			if item.Q != nil {
				allNodes[item.Q.ID] = true
			}
			if item.A != nil {
				allNodes[item.A.ID] = true
			}
		}

		// Unknown ID → exit 3.
		if !allNodes[concept] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Not a concept (q or a node) → exit 1.
		if !allConcepts[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not a concept",
				Exit: 1,
			}
		}

		// Passed concept → exit 1.
		if passedSet[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is passed",
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\"", concept),
				Exit: 1,
			}
		}

		// Has questions → exit 1.
		for _, item := range g.TestingItems {
			if item.Q == nil {
				continue
			}
			cid, ok := s.ConceptOf(item.Q.ID)
			if ok && cid == concept {
				return nil, nil, &ops.Refusal{
					Err:  concept + " has questions",
					Exit: 1,
				}
			}
		}

		// Find the target node in untested.
		var targetNode *graph.ConceptNode
		for _, c := range g.UntestedConcepts {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		oldScope := targetNode.Scope

		// Build new node (copy-on-write).
		newNode := *targetNode
		newNode.Scope = scope
		if citeStr != "" {
			newNode.Cites = []string{citeStr}
		}

		// Build new graph with the edited node.
		newG := *g
		newConcepts := make([]*graph.ConceptNode, len(g.UntestedConcepts))
		for i, c := range g.UntestedConcepts {
			if c.ID == concept {
				newConcepts[i] = &newNode
			} else {
				newConcepts[i] = c
			}
		}
		newG.UntestedConcepts = newConcepts

		row := eventlog.NewRow("edit", map[string]any{
			"id":     concept,
			"before": oldScope,
			"after":  scope,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutationWithFile(ctx, file, apply)
}
