package cli

import (
	"fmt"
	"path/filepath"

	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/source"
	"github.com/reithan/teach-me/internal/state"
)

// editRun is the Run handler for
// `tm edit <concept> "<scope>" [--src <cite>] [--errata "<reason>"]`.
//
// Rewrites the scope of a concept. Without --errata the concept must have no
// questions and not be passed. With --errata (decision 73) it also accepts a
// concept that has questions or is passed: the reason is logged, questions and
// answers are untouched, and a pass then goes to a grader recheck
// (`tm check --errata` / `tm grade --errata`). An optional --src flag replaces
// the citation. Outputs "ok".
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal (passed or has questions without --errata; wrong type)
//	2  output fails lint (internal error)
//	3  usage error, unknown ID, bad --src citation, empty --errata reason
func editRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	scope := ctx.Positionals[1]

	usageLine := FindCommand("edit").Usage()

	// --errata "<reason>": with it, edit accepts a passed concept or one with
	// questions. The reason must be non-empty.
	errata := ""
	hasErrata := false
	if vals := ctx.Flags["errata"]; len(vals) > 0 {
		hasErrata = true
		errata = vals[0]
		if errata == "" {
			ctx.ErrMsg = "--errata needs a reason"
			ctx.FixMsg = fmt.Sprintf("tm edit %s \"<scope>\" --errata \"<why the old scope was wrong>\"", concept)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, ctx.FixMsg)
			return 3
		}
	}

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
		resolver, resolverErr := source.NewResolver(filepath.Dir(file))
		if resolverErr != nil {
			ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
			writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
			return 3
		}
		hashedCite, _, hashErr := resolver.HashCitation(citeStr)
		if hashErr != nil {
			return citeHashError(ctx, citeStr, hashErr, usageLine)
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

		// Not a concept (q or a node) → exit 1. This holds even with --errata.
		if !allConcepts[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not a concept",
				Exit: 1,
			}
		}

		inPassed := passedSet[concept]

		// Passed concept → exit 1 without --errata; allowed with --errata.
		if inPassed && !hasErrata {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is passed",
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\"", concept),
				Exit: 1,
			}
		}

		// Has questions → exit 1 without --errata; allowed with --errata.
		if !hasErrata {
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
		}

		// Find the target node in the list that currently holds it.
		list := g.UntestedConcepts
		if inPassed {
			list = g.PassedConcepts
		}
		var targetNode *graph.ConceptNode
		for _, c := range list {
			if c.ID == concept {
				targetNode = c
				break
			}
		}

		oldScope := targetNode.Scope
		oldCite := ""
		if len(targetNode.Cites) > 0 {
			oldCite = targetNode.Cites[0]
		}

		// Build new node (copy-on-write).
		newNode := *targetNode
		newNode.Scope = scope
		if citeStr != "" {
			newNode.Cites = []string{citeStr}
		}

		// Build new graph with the edited node replaced in its list.
		newG := *g
		newConcepts := make([]*graph.ConceptNode, len(list))
		for i, c := range list {
			if c.ID == concept {
				newConcepts[i] = &newNode
			} else {
				newConcepts[i] = c
			}
		}
		if inPassed {
			newG.PassedConcepts = newConcepts
		} else {
			newG.UntestedConcepts = newConcepts
		}

		fields := map[string]any{
			"id":     concept,
			"before": oldScope,
			"after":  scope,
		}
		if hasErrata {
			fields["errata"] = errata
			// Log the citation move only when --errata --src re-points it.
			if citeStr != "" {
				fields["src_before"] = oldCite
				fields["src_after"] = citeStr
			}
		}
		row := eventlog.NewRow("edit", fields)

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutationWithFile(ctx, file, apply)
}
