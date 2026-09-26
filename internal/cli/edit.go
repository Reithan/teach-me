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
	citeStr, code := hashSrcFlag(ctx, file, usageLine)
	if code != 0 {
		return code
	}

	// ── Apply closure ────────────────────────────────────────────────────────
	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		ns := graphNodeSets(g)

		// Unknown ID → exit 3.
		if !ns.AllNodes[concept] {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("unknown ID %q", concept),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Not a concept (q or a node) → exit 1.
		if !ns.AllConcepts[concept] {
			return nil, nil, &ops.Refusal{
				Err:  concept + " is not a concept",
				Exit: 1,
			}
		}

		// Passed concept → exit 1.
		if ns.PassedSet[concept] {
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

// hashSrcFlag validates the --src citation, if given, against the sources of
// the graph at file and returns it with its hash. It returns "" when --src is
// absent. On failure it writes the err:/fix: lines and returns a nonzero exit
// code. Shared by edit and errata.
func hashSrcFlag(ctx *Context, file, usageLine string) (string, int) {
	vals := ctx.Flags["src"]
	if len(vals) == 0 {
		return "", 0
	}
	citeStr := vals[0]
	resolver, resolverErr := source.NewResolver(filepath.Dir(file))
	if resolverErr != nil {
		ctx.ErrMsg = fmt.Sprintf("source config: %v", resolverErr)
		writeErrFix(ctx.ErrOut, ctx.ErrMsg, "")
		return "", 3
	}
	hashedCite, _, hashErr := resolver.HashCitation(citeStr)
	if hashErr != nil {
		return "", citeHashError(ctx, citeStr, hashErr, usageLine)
	}
	return hashedCite, 0
}
