package state

import (
	"fmt"
	"sort"

	"github.com/reithan/teach-me/internal/graph"
)

// ConceptBatches returns the batch class IDs for concept in ascending N order.
// Returns nil when the concept has no batches.
func (s *State) ConceptBatches(concept string) []string {
	return s.conceptBatches[concept]
}

// DraftProbeBatch returns the highest-N probe batch in BatchDraft state for
// the given concept. Returns ("", false) when no draft probe batch exists.
func (s *State) DraftProbeBatch(concept string) (string, bool) {
	batches := s.conceptBatches[concept]
	// batches are sorted by N ascending; iterate to find the highest draft probe.
	class := ""
	for _, b := range batches {
		if graph.IsProbeClass(b) && s.BatchStateOf(b) == BatchDraft {
			class = b // keep updating to return the highest-N one
		}
	}
	if class == "" {
		return "", false
	}
	return class, true
}

// DraftTeachBatch returns the highest-N teach batch in BatchDraft state for
// the given concept. Returns ("", false) when no draft teach batch exists.
func (s *State) DraftTeachBatch(concept string) (string, bool) {
	batches := s.conceptBatches[concept]
	class := ""
	for _, b := range batches {
		if graph.IsTeachClass(b) && s.BatchStateOf(b) == BatchDraft {
			class = b
		}
	}
	if class == "" {
		return "", false
	}
	return class, true
}

// ProbeReplacesUnclear reports whether qid is a probe question whose graded
// answer has class "unclear". Returns false when qid is unknown, not a probe,
// or has no answer.
func (s *State) ProbeReplacesUnclear(qid string) bool {
	q, ok := s.qByID[qid]
	if !ok || !graph.IsProbeClass(q.Class) {
		return false
	}
	a := s.answerFor(qid)
	return a != nil && a.Class == "unclear"
}

// TeachTargetAnswer returns the answer node a<N(qid)> — the answer node that
// a teach question with --re qid would attach its incoming edge from. Returns
// (nil, false) when qid is not a valid question ID or has no answer.
func (s *State) TeachTargetAnswer(qid string) (*graph.AnswerNode, bool) {
	n := graph.QuestionN(qid)
	if n == 0 {
		return nil, false
	}
	aID := fmt.Sprintf("a%d", n)
	a, ok := s.aByID[aID]
	return a, ok
}

// AnswerFor returns the answer node for question qid (the aN where N matches
// qN), or nil when no answer exists.
func (s *State) AnswerFor(qid string) *graph.AnswerNode {
	return s.answerFor(qid)
}

// BatchConcept returns the concept ID that owns the given batch class.
// Returns ("", false) when the batch is unknown.
func (s *State) BatchConcept(class string) (string, bool) {
	c, ok := s.batchConcept[class]
	return c, ok
}

// BatchQuestions returns the questions in batch class in declaration order.
// Returns nil when the batch is unknown.
func (s *State) BatchQuestions(class string) []*graph.QuestionNode {
	return s.batchQs[class]
}

// BatchOf returns the batch class for question qid. Returns ("", false) when
// qid is unknown or its class is not a valid batch ID.
func (s *State) BatchOf(qid string) (string, bool) {
	q, ok := s.qByID[qid]
	if !ok || !graph.ValidBatchID(q.Class) {
		return "", false
	}
	return q.Class, true
}

// UnblockedBy returns the IDs of untested concepts that become newly
// answerable because conceptID just passed. A concept X is unblocked when
// conceptID is one of X's concept-parents and every other concept-parent of X
// is already in passed.
//
// This feeds the pass event's unblocked field (§10). The caller supplies the
// conceptID that has just passed; it need not yet appear in s.passedSet.
// The returned slice is sorted ascending.
func (s *State) UnblockedBy(conceptID string) []string {
	var result []string
	for _, c := range s.g.UntestedConcepts {
		if c.ID == conceptID {
			continue // skip the concept that is passing
		}
		// Determine whether conceptID is a direct concept-parent of c, and
		// whether all other concept-parents of c are already in passed.
		hasConceptAsParent := false
		allOthersPassed := true
		for _, e := range s.inEdges[c.ID] {
			if !s.allConcepts[e.From] {
				continue // skip non-concept edges (e.g., answer → question)
			}
			if e.From == conceptID {
				hasConceptAsParent = true
				continue
			}
			if !s.passedSet[e.From] {
				allOthersPassed = false
				break
			}
		}
		if hasConceptAsParent && allOthersPassed {
			result = append(result, c.ID)
		}
	}
	sort.Strings(result)
	return result
}
