package cli

import (
	"fmt"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/ops"
	"github.com/reithan/teach-me/internal/state"
)

// reciteRun is the Run handler for `tm recite <concept> <locator>:START-END`.
//
// Re-points one of a concept's citations to a new line range that hashes to
// the same content. Accepted only when the new range's hash matches an
// existing citation's hash (the text moved but did not change).
//
// For concepts with multiple citations, the matching citation is identified
// by its hash: the one whose stored hash equals the hash of the new range.
//
// Logged as a `recite` event with `id`, `before`, `after`.
//
// Exit codes:
//
//	0  ok
//	1  invariant refusal: hash mismatch (§7 line 297)
//	2  output fails lint (internal engine error)
//	3  usage error, unknown concept, or citation error
func reciteRun(ctx *Context) int {
	concept := ctx.Positionals[0]
	newRangeStr := ctx.Positionals[1]

	usageLine := FindCommand("recite").Usage()

	apply := func(g *graph.Graph, s *state.State) (*graph.Graph, []eventlog.Row, *ops.Refusal) {
		srcRoot := s.Cfg().SrcRoot

		// Find the concept (passed, untested, or reserve).
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
				Fix:  usageLine,
				Exit: 3,
			}
		}

		if len(targetNode.Cites) == 0 {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s has no citations", concept),
				Exit: 1,
			}
		}

		// Hash the new range to find which existing citation it matches.
		// HashCitation reads the text and computes a 12-hex-char hash.
		newHashed, hashErr := hashCiteText(newRangeStr, srcRoot)
		if hashErr != nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot hash %q: %v", newRangeStr, hashErr),
				Fix:  usageLine,
				Exit: 3,
			}
		}

		// Parse the new hashed citation to extract the hash.
		newCit, parseErr := cite.Parse(newHashed)
		if parseErr != nil {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("cannot parse %q: %v", newHashed, parseErr),
				Exit: 3,
			}
		}
		newHash := newCit.Hash

		// Find the existing citation whose stored hash equals the new hash.
		matchIdx := -1
		for i, citeStr := range targetNode.Cites {
			existing, pErr := cite.Parse(citeStr)
			if pErr != nil {
				continue
			}
			if existing.Hash == newHash {
				matchIdx = i
				break
			}
		}

		if matchIdx < 0 {
			return nil, nil, &ops.Refusal{
				Err:  fmt.Sprintf("%s recite hash mismatch", concept),
				Fix:  fmt.Sprintf("tm reopen %s \"<gap>\" --src %s", concept, newRangeStr),
				Exit: 1,
			}
		}

		// Replace the matching citation with the new hashed form.
		before := targetNode.Cites[matchIdx]

		newNode := *targetNode
		newCites := make([]string, len(targetNode.Cites))
		copy(newCites, targetNode.Cites)
		newCites[matchIdx] = newHashed
		newNode.Cites = newCites

		// Build updated graph (copy-on-write).
		newG := *g
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
			newG.PassedConcepts = newPassed
		case graph.BlockReserve:
			newReserve := make([]*graph.ConceptNode, len(g.ReserveConcepts))
			copy(newReserve, g.ReserveConcepts)
			for i, c := range newReserve {
				if c.ID == concept {
					newReserve[i] = &newNode
					break
				}
			}
			newG.ReserveConcepts = newReserve
		default:
			newUntested := make([]*graph.ConceptNode, len(g.UntestedConcepts))
			copy(newUntested, g.UntestedConcepts)
			for i, c := range newUntested {
				if c.ID == concept {
					newUntested[i] = &newNode
					break
				}
			}
			newG.UntestedConcepts = newUntested
		}

		row := eventlog.NewRow("recite", map[string]any{
			"id":     concept,
			"before": before,
			"after":  newHashed,
		})

		return &newG, []eventlog.Row{row}, nil
	}

	return runMutation(ctx, apply)
}
