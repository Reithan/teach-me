package ops

import (
	"github.com/reithan/teach-me/internal/eventlog"
	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// ClearGate writes a gate meta line for conceptID into a copy of g and returns
// the updated graph and the gate event row. Returns (nil, zero-value, false)
// when conceptID is not currently Gated in s.
//
// via must be "add", "reopen", "activate", or "override". reason is the --override reason
// text; pass "" for add/reopen/activate via.
//
// base = max(BatchN) over all of the concept's batch class IDs, 0 when the
// concept has no batches.
//
// trip: if len(cs.FailedProbeBatches) >= cfg.MaxFails → "probes"; if
// cs.Stalled → "stall"; when both hold, "probes" takes precedence (spec §10
// via enum has no trip/grade value per Q6).
//
// Any existing gate meta line for conceptID in g.UntestedMetas is replaced;
// new entries are appended. Copy-on-write: g is never mutated.
func ClearGate(g *graph.Graph, s *state.State, conceptID, via, reason string) (*graph.Graph, eventlog.Row, bool) {
	cs := s.ConceptStatus(conceptID)
	if !cs.Gated {
		return nil, eventlog.Row{}, false
	}

	cfg := s.Cfg()

	// Determine trip: prefer "probes" when both conditions hold simultaneously.
	trip := "stall"
	if len(cs.FailedProbeBatches) >= cfg.MaxFails {
		trip = "probes"
	}

	// base = max BatchN across all batches for the concept (spec §7 line 278:
	// "base=<current max batch>").
	base := 0
	for _, b := range s.ConceptBatches(conceptID) {
		if n := graph.BatchN(b); n > base {
			base = n
		}
	}

	// Build new graph copy-on-write: replace existing meta for this concept or
	// append a new one.
	meta := graph.GateMeta{Concept: conceptID, Base: base}
	newMetas := make([]graph.GateMeta, 0, len(g.UntestedMetas)+1)
	replaced := false
	for _, m := range g.UntestedMetas {
		if m.Concept == conceptID {
			newMetas = append(newMetas, meta)
			replaced = true
		} else {
			newMetas = append(newMetas, m)
		}
	}
	if !replaced {
		newMetas = append(newMetas, meta)
	}
	newG := *g
	newG.UntestedMetas = newMetas

	row := eventlog.NewRow("gate", map[string]any{
		"concept": conceptID,
		"trip":    trip,
		"base":    base,
		"via":     via,
		"reason":  reason,
	})

	return &newG, row, true
}
