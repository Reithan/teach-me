package cli

import (
	"strings"

	"github.com/reithan/teach-me/internal/state"
)

// buildGateFixMsg returns the fix: message for a gated concept, following
// spec §7. When the concept has reserve parents, the message leads with
// "tm activate <p1>|<p2>". The activate clause is omitted when there are no
// reserve parents.
//
// Format with reserve parents:
//
//	tm activate <p1>|<p2>, tm add --child <concept>, tm reopen <parent>, or --override "<reason>"
//
// Format without reserve parents:
//
//	tm add --child <concept>, tm reopen <parent>, or --override "<reason>"
func buildGateFixMsg(conceptID string, s *state.State) string {
	parts := make([]string, 0, 4)

	rp := s.ReserveParents(conceptID)
	if len(rp) > 0 {
		parts = append(parts, "tm activate "+strings.Join(rp, "|"))
	}

	parts = append(parts,
		"tm add --child "+conceptID,
		"tm reopen <parent>",
	)

	// Last item gets "or" prefix.
	return strings.Join(parts, ", ") + `, or --override "<reason>"`
}
